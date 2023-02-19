package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/common"
	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"github.com/dirien/minectl-sdk/update"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const instanceNameTag = "Name"

type Aws struct {
	client *ec2.Client
	tmpl   *minctlTemplate.Template
	region string
}

// NewAWS creates an Aws and initialises an EC2 client
func NewAWS(region string) (*Aws, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	ec2Svc := ec2.NewFromConfig(cfg)

	tmpl, err := minctlTemplate.NewTemplateCloudConfig()
	if err != nil {
		return nil, err
	}

	return &Aws{
		client: ec2Svc,
		region: region,
		tmpl:   tmpl,
	}, err
}

func (a *Aws) ListServer() ([]automation.ResourceResults, error) {
	ctx := context.TODO()
	var result []automation.ResourceResults
	var nextToken *string

	for {
		input := &ec2.DescribeInstancesInput{
			Filters: []types.Filter{
				{
					Name:   aws.String(fmt.Sprintf("tag:%s", common.InstanceTag)),
					Values: []string{"true"},
				},
			},
			NextToken: nextToken,
		}

		instances, err := a.client.DescribeInstances(ctx, input)
		if err != nil {
			return nil, err
		}

		for _, r := range instances.Reservations {
			for _, i := range r.Instances {
				if i.State.Name != types.InstanceStateNameTerminated {
					arr := automation.ResourceResults{
						ID:     *i.InstanceId,
						Region: a.region,
					}

					if i.PublicIpAddress != nil {
						arr.PublicIP = *i.PublicIpAddress
					}

					var tags []string
					var instanceName string
					for _, v := range i.Tags {
						tags = append(tags, fmt.Sprintf("%s=%s", *v.Key, *v.Value))
						if *v.Key == instanceNameTag {
							instanceName = *v.Value
						}
					}

					arr.Tags = strings.Join(tags, ",")
					arr.Name = instanceName

					result = append(result, arr)
				}
			}
		}

		nextToken = instances.NextToken
		if nextToken == nil {
			break
		}
	}

	return result, nil
}

func addBlockDevice(volumeSize int) []types.BlockDeviceMapping {
	if volumeSize > 0 {
		return []types.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &types.EbsBlockDevice{
					VolumeSize: aws.Int32(int32(volumeSize)),
				},
			},
		}
	}
	return nil
}

func (a *Aws) addNetworkInterfaces(ctx context.Context, vpc *ec2.CreateVpcOutput, args automation.ServerArgs, subnetID *string) ([]types.InstanceNetworkInterfaceSpecification, error) {
	var secGroups []string
	var groupID *string
	var err error
	if args.MinecraftResource.GetEdition() == "bedrock" || args.MinecraftResource.GetEdition() == "nukkit" || args.MinecraftResource.GetEdition() == "powernukkit" {
		groupID, err = a.createEC2SecurityGroup(ctx, vpc.Vpc.VpcId, "udp", args.MinecraftResource.GetPort())
		if err != nil {
			return nil, err
		}
	} else {
		groupID, err = a.createEC2SecurityGroup(ctx, vpc.Vpc.VpcId, "tcp", args.MinecraftResource.GetPort())
		if err != nil {
			return nil, err
		}
		if args.MinecraftResource.HasRCON() {
			rconID, err := a.createEC2SecurityGroup(ctx, vpc.Vpc.VpcId, "tcp", args.MinecraftResource.GetRCONPort())
			if err != nil {
				return nil, err
			}
			secGroups = append(secGroups, *rconID)
		}
	}
	secGroups = append(secGroups, *groupID)
	if args.MinecraftResource.HasMonitoring() {
		promGroupID, err := a.createEC2SecurityGroup(ctx, vpc.Vpc.VpcId, "tcp", 9090)
		if err != nil {
			return nil, err
		}
		secGroups = append(secGroups, *promGroupID)
	}
	sshGroupID, err := a.createEC2SecurityGroup(ctx, vpc.Vpc.VpcId, "tcp", args.MinecraftResource.GetSSHPort())
	if err != nil {
		return nil, err
	}
	secGroups = append(secGroups, *sshGroupID)

	return []types.InstanceNetworkInterfaceSpecification{
		{
			Description:              aws.String("the primary device eth0"),
			DeviceIndex:              aws.Int32(0),
			AssociatePublicIpAddress: aws.Bool(true),
			SubnetId:                 subnetID,
			Groups:                   secGroups,
		},
	}, nil
}

func addTags(args automation.ServerArgs) []types.Tag {
	return []types.Tag{
		{
			Key:   aws.String("edition"),
			Value: aws.String(args.MinecraftResource.GetEdition()),
		},
		{
			Key:   aws.String(instanceNameTag),
			Value: aws.String(args.MinecraftResource.GetName()),
		},
		{
			Key:   aws.String(common.InstanceTag),
			Value: aws.String("true"),
		},
	}
}

func addTagSpecifications(args automation.ServerArgs, resourceType types.ResourceType) []types.TagSpecification {
	return []types.TagSpecification{
		{
			ResourceType: resourceType,
			Tags:         addTags(args),
		},
	}
}

// CreateServer TODO: https://github.com/dirien/minectl/issues/298
func (a *Aws) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) { //nolint: gocyclo
	ctx := context.TODO()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	publicKey, err := cloud.GetSSHPublicKey(args)
	if err != nil {
		return nil, err
	}

	var imageAMI *string
	if args.MinecraftResource.IsArm() {
		imageAMI, err = a.lookupAMI(ctx, "ubuntu-minimal/images/hvm-ssd/ubuntu-jammy-22.04*", "arm64")
		if err != nil {
			return nil, err
		}
	} else {
		imageAMI, err = a.lookupAMI(ctx, "ubuntu-minimal/images/hvm-ssd/ubuntu-jammy-22.04*", "x86_64")
		if err != nil {
			return nil, err
		}
	}

	key, err := a.client.ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
		KeyName:           aws.String(fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName())),
		PublicKeyMaterial: []byte(*publicKey),
	})
	if err != nil {
		return nil, err
	}

	vpc, err := a.client.CreateVpc(ctx, &ec2.CreateVpcInput{
		CidrBlock:         aws.String("172.16.0.0/16"),
		TagSpecifications: addTagSpecifications(args, types.ResourceTypeVpc),
	})
	if err != nil {
		return nil, err
	}

	subnet, err := a.client.CreateSubnet(ctx, &ec2.CreateSubnetInput{
		CidrBlock:         aws.String("172.16.10.0/24"),
		VpcId:             vpc.Vpc.VpcId,
		TagSpecifications: addTagSpecifications(args, types.ResourceTypeSubnet),
	})
	if err != nil {
		return nil, err
	}

	internetGateway, err := a.client.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{
		TagSpecifications: addTagSpecifications(args, types.ResourceTypeInternetGateway),
	})
	if err != nil {
		return nil, err
	}

	_, err = a.client.AttachInternetGateway(ctx, &ec2.AttachInternetGatewayInput{
		VpcId:             vpc.Vpc.VpcId,
		InternetGatewayId: internetGateway.InternetGateway.InternetGatewayId,
	})
	if err != nil {
		return nil, err
	}

	routeTable, err := a.client.CreateRouteTable(ctx, &ec2.CreateRouteTableInput{
		VpcId:             vpc.Vpc.VpcId,
		TagSpecifications: addTagSpecifications(args, types.ResourceTypeRouteTable),
	})
	if err != nil {
		return nil, err
	}
	_, err = a.client.CreateRoute(ctx, &ec2.CreateRouteInput{
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            internetGateway.InternetGateway.InternetGatewayId,
		RouteTableId:         routeTable.RouteTable.RouteTableId,
	})
	if err != nil {
		return nil, err
	}
	_, err = a.client.AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
		SubnetId:     subnet.Subnet.SubnetId,
		RouteTableId: routeTable.RouteTable.RouteTableId,
	})
	if err != nil {
		return nil, err
	}

	userData, err := a.tmpl.GetTemplate(args.MinecraftResource, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.GetTemplateCloudConfigName(args.MinecraftResource.IsProxyServer())})
	if err != nil {
		return nil, err
	}

	if args.MinecraftResource.IsSpot() {
		zap.S().Infow("Creating spot instance", "name", args.MinecraftResource.GetName())
		spotInstance := ec2.RequestSpotInstancesInput{
			InstanceCount: aws.Int32(1),
			LaunchSpecification: &types.RequestSpotLaunchSpecification{
				ImageId:             imageAMI,
				KeyName:             key.KeyName,
				InstanceType:        types.InstanceType(args.MinecraftResource.GetSize()),
				BlockDeviceMappings: addBlockDevice(args.MinecraftResource.GetVolumeSize()),
				UserData:            aws.String(base64.StdEncoding.EncodeToString([]byte(userData))),
			},
			TagSpecifications: addTagSpecifications(args, types.ResourceTypeSpotInstancesRequest),
		}
		spotInstance.LaunchSpecification.NetworkInterfaces, err = a.addNetworkInterfaces(ctx, vpc, args, subnet.Subnet.SubnetId)
		if err != nil {
			return nil, err
		}

		result, err := a.client.RequestSpotInstances(ctx, &spotInstance)
		if err != nil {
			return nil, err
		}

		for {
			select {
			case <-ctx.Done():
				return nil, errors.New("timed out while creating the aws instance")
			case <-time.After(10 * time.Second):
				spotInstanceRequests, err := a.client.DescribeSpotInstanceRequests(ctx, &ec2.DescribeSpotInstanceRequestsInput{
					SpotInstanceRequestIds: []string{*result.SpotInstanceRequests[0].SpotInstanceRequestId},
				})
				if err != nil {
					return nil, err
				}
				instanceStatus, err := a.client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
					InstanceIds: []string{*spotInstanceRequests.SpotInstanceRequests[0].InstanceId},
				})
				if err != nil {
					return nil, err
				}
				_, err = a.client.CreateTags(ctx, &ec2.CreateTagsInput{
					Resources: []string{*spotInstanceRequests.SpotInstanceRequests[0].InstanceId},
					Tags:      addTags(args),
				})
				if err != nil {
					return nil, err
				}
				if instanceStatus.InstanceStatuses[0].InstanceState.Name == "running" {
					i, err := a.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
						InstanceIds: []string{*spotInstanceRequests.SpotInstanceRequests[0].InstanceId},
					})
					if err != nil {
						return nil, err
					}
					var tags []string
					var instanceName string
					for _, v := range i.Reservations[0].Instances[0].Tags {
						tags = append(tags, fmt.Sprintf("%s=%s", *v.Key, *v.Value))

						if *v.Key == instanceNameTag {
							instanceName = *v.Value
						}
					}

					return &automation.ResourceResults{
						ID:       fmt.Sprintf("%s#%s", *i.Reservations[0].Instances[0].InstanceId, *result.SpotInstanceRequests[0].SpotInstanceRequestId),
						Name:     instanceName,
						Region:   a.region,
						PublicIP: *i.Reservations[0].Instances[0].PublicIpAddress,
						Tags:     strings.Join(tags, ","),
					}, nil
				}
			}
		}
	} else {
		zap.S().Infow("Creating instance", "name", args.MinecraftResource.GetName())
		instanceInput := &ec2.RunInstancesInput{
			ImageId:             imageAMI,
			KeyName:             key.KeyName,
			InstanceType:        types.InstanceType(args.MinecraftResource.GetSize()),
			MinCount:            aws.Int32(1),
			MaxCount:            aws.Int32(1),
			UserData:            aws.String(base64.StdEncoding.EncodeToString([]byte(userData))),
			TagSpecifications:   addTagSpecifications(args, types.ResourceTypeInstance),
			BlockDeviceMappings: addBlockDevice(args.MinecraftResource.GetVolumeSize()),
		}

		instanceInput.NetworkInterfaces, err = a.addNetworkInterfaces(ctx, vpc, args, subnet.Subnet.SubnetId)
		if err != nil {
			return nil, err
		}

		result, err := a.client.RunInstances(ctx, instanceInput)
		if err != nil {
			return nil, err
		}

		for {
			select {
			case <-ctx.Done():
				return nil, errors.New("timed out while creating the aws instance")
			case <-time.After(10 * time.Second):
				describeInstanceStatus, err := a.client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
					InstanceIds: []string{*result.Instances[0].InstanceId},
				})
				if err != nil {
					return nil, err
				}
				if len(describeInstanceStatus.InstanceStatuses) > 0 {
					if describeInstanceStatus.InstanceStatuses[0].InstanceState.Name == "running" {
						i, err := a.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
							InstanceIds: []string{*result.Instances[0].InstanceId},
						})
						if err != nil {
							return nil, err
						}
						var tags []string
						var instanceName string
						for _, v := range i.Reservations[0].Instances[0].Tags {
							tags = append(tags, fmt.Sprintf("%s=%s", *v.Key, *v.Value))

							if *v.Key == instanceNameTag {
								instanceName = *v.Value
							}
						}

						return &automation.ResourceResults{
							ID:       *i.Reservations[0].Instances[0].InstanceId,
							Name:     instanceName,
							Region:   a.region,
							PublicIP: *i.Reservations[0].Instances[0].PublicIpAddress,
							Tags:     strings.Join(tags, ","),
						}, nil
					}
				}
			}
		}
	}
}

func (a *Aws) UpdateServer(id string, args automation.ServerArgs) error {
	ctx := context.TODO()
	ids, _, _ := strings.Cut(id, "#")
	i, err := a.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{ids},
	})
	if err != nil {
		return err
	}

	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, *i.Reservations[0].Instances[0].PublicIpAddress, "ubuntu")
	err = remoteCommand.UpdateServer(args.MinecraftResource)
	if err != nil {
		return err
	}

	return nil
}

func (a *Aws) DeleteServer(id string, args automation.ServerArgs) error {
	ctx := context.TODO()

	ids, spotID, _ := strings.Cut(id, "#")
	if args.MinecraftResource.IsSpot() {
		_, err := a.client.CancelSpotInstanceRequests(ctx, &ec2.CancelSpotInstanceRequestsInput{
			SpotInstanceRequestIds: []string{spotID},
		})
		if err != nil {
			return err
		}
	}
	i, err := a.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{ids},
	})
	if err != nil {
		return err
	}
	// we have only on instance
	instance := i.Reservations[0].Instances[0]

	_, err = a.client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{ids},
	})
	if err != nil {
		return err
	}

	stillDeleting := true

	for stillDeleting {
		status, err := a.client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
			InstanceIds: []string{ids},
		})
		if err != nil {
			return err
		}

		if *status.TerminatingInstances[0].CurrentState.Code == 48 {
			stillDeleting = false
			time.Sleep(15 * time.Second)
		} else {
			time.Sleep(2 * time.Second)
		}

	}

	groups := instance.SecurityGroups

	for _, group := range groups {
		_, err = a.client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: group.GroupId,
		})
		if err != nil {
			return err
		}
	}

	vpcID := instance.VpcId
	subnetID := instance.SubnetId
	_, err = a.client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
		SubnetId: subnetID,
	})
	if err != nil {
		return err
	}

	internetGateways, err := a.client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("attachment.vpc-id"),
				Values: []string{*vpcID},
			},
		},
	})
	if err != nil {
		return err
	}

	for _, internetGateway := range internetGateways.InternetGateways {
		_, err := a.client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
			InternetGatewayId: internetGateway.InternetGatewayId,
			VpcId:             vpcID,
		})
		if err != nil {
			return err
		}
		_, err = a.client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
			InternetGatewayId: internetGateway.InternetGatewayId,
		})
		if err != nil {
			return err
		}
	}

	routeTables, err := a.client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{*vpcID},
			},
		},
	})
	if err != nil {
		return err
	}

	for _, routeTable := range routeTables.RouteTables {
		if len(routeTable.Associations) == 0 {
			_, err := a.client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{
				RouteTableId: routeTable.RouteTableId,
			})
			if err != nil {
				return err
			}
		}
	}

	_, err = a.client.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: vpcID,
	})
	if err != nil {
		return err
	}
	keys, err := a.client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName())},
	})
	if err != nil {
		return err
	}

	_, err = a.client.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
		KeyName: aws.String(*keys.KeyPairs[0].KeyName),
	})
	if err != nil {
		return err
	}
	return nil
}

func (a *Aws) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	ctx := context.TODO()
	ids, _, _ := strings.Cut(id, "#")
	i, err := a.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{ids},
	})
	if err != nil {
		return err
	}

	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, *i.Reservations[0].Instances[0].PublicIpAddress, "ubuntu")

	err = remoteCommand.TransferFile(plugin, filepath.Join(destination, filepath.Base(plugin)), args.MinecraftResource.GetSSHPort())
	if err != nil {
		return err
	}

	_, err = remoteCommand.ExecuteCommand("systemctl restart minecraft.service", args.MinecraftResource.GetSSHPort())
	if err != nil {
		return err
	}

	return nil
}

func (a *Aws) GetServer(id string, _ automation.ServerArgs) (*automation.ResourceResults, error) {
	ctx := context.TODO()
	ids, _, _ := strings.Cut(id, "#")
	i, err := a.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{ids},
	})
	if err != nil {
		return nil, err
	}

	var tags []string
	var instanceName string
	for _, v := range i.Reservations[0].Instances[0].Tags {
		tags = append(tags, fmt.Sprintf("%s=%s", *v.Key, *v.Value))

		if *v.Key == instanceNameTag {
			instanceName = *v.Value
		}
	}

	return &automation.ResourceResults{
		ID:       *i.Reservations[0].Instances[0].InstanceId,
		Name:     instanceName,
		Region:   a.region,
		PublicIP: *i.Reservations[0].Instances[0].PublicIpAddress,
		Tags:     strings.Join(tags, ","),
	}, err
}

func (a *Aws) createEC2SecurityGroup(ctx context.Context, vpcID *string, protocol string, controlPort int) (*string, error) {
	groupName := "minecraft-" + uuid.New().String()
	input := &ec2.CreateSecurityGroupInput{
		Description: aws.String("minecraft security group"),
		GroupName:   aws.String(groupName),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeSecurityGroup,
				Tags: []types.Tag{
					{
						Key:   aws.String("Name"),
						Value: aws.String(groupName),
					},
				},
			},
		},
	}

	if vpcID != nil {
		input.VpcId = vpcID
	}

	group, err := a.client.CreateSecurityGroup(ctx, input)
	if err != nil {
		return nil, err
	}

	err = a.createEC2SecurityGroupRule(ctx, *group.GroupId, protocol, controlPort, controlPort)
	if err != nil {
		return group.GroupId, err
	}

	return group.GroupId, nil
}

func (a *Aws) createEC2SecurityGroupRule(ctx context.Context, groupID, protocol string, fromPort, toPort int) error {
	_, err := a.client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		CidrIp:     aws.String("0.0.0.0/0"),
		FromPort:   aws.Int32(int32(fromPort)),
		IpProtocol: aws.String(protocol),
		ToPort:     aws.Int32(int32(toPort)),
		GroupId:    aws.String(groupID),
	})
	if err != nil {
		return err
	}

	return nil
}

// lookupAMI gets the AMI ID that the exit node will use
func (a *Aws) lookupAMI(ctx context.Context, name, architecture string) (*string, error) {
	images, err := a.client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Filters: []types.Filter{
			{
				Name: aws.String("name"),
				Values: []string{
					name,
				},
			},
			{
				Name: aws.String("architecture"),
				Values: []string{
					architecture,
				},
			},
			{
				Name: aws.String("owner-id"),
				Values: []string{
					"099720109477",
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(images.Images) == 0 {
		return nil, fmt.Errorf("image not found")
	}
	sort.SliceStable(images.Images, func(i, j int) bool {
		before := *images.Images[i].CreationDate
		after := *images.Images[j].CreationDate
		beforeTime, _ := time.Parse(time.RFC3339, before)
		afterTime, _ := time.Parse(time.RFC3339, after)
		return beforeTime.After(afterTime)
	})
	return images.Images[0].ImageId, nil
}
