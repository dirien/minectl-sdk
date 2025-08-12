package azure

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/common"
	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"github.com/dirien/minectl-sdk/update"
	"go.uber.org/zap"
)

type Azure struct {
	subscriptionID string
	credential     *azidentity.DefaultAzureCredential
	tmpl           *minctlTemplate.Template
}

func NewAzure(authFile string) (*Azure, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	tmpl, err := minctlTemplate.NewTemplateCloudConfig()
	if err != nil {
		return nil, err
	}
	zap.S().Infow("Azure set cloud-config template", "name", tmpl.Template.Name())
	return &Azure{
		subscriptionID: os.Getenv("AZURE_SUBSCRIPTION_ID"),
		credential:     cred,
		tmpl:           tmpl,
	}, nil
}

func getTags(edition string) map[string]*string {
	return map[string]*string{
		common.InstanceTag: to.Ptr("true"),
		edition:            to.Ptr("true"),
	}
}

func getTagKeys(tags map[string]*string) []string {
	var keys []string
	for key := range tags {
		keys = append(keys, key)
	}
	return keys
}

func (a *Azure) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	ctx := context.Background()
	resourceGroupsClient, err := armresources.NewResourceGroupsClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	group, err := resourceGroupsClient.CreateOrUpdate(
		ctx,
		fmt.Sprintf("%s-rg", args.MinecraftResource.GetName()),
		armresources.ResourceGroup{
			Location: to.Ptr(args.MinecraftResource.GetRegion()),
		}, nil)
	if err != nil {
		return nil, err
	}
	zap.S().Infow("Azure resource group created", "name", group.Name)

	virtualNetworkClient, err := armnetwork.NewVirtualNetworksClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	vnetPoller, err := virtualNetworkClient.BeginCreateOrUpdate(
		ctx,
		*group.Name,
		fmt.Sprintf("%s-vnet", args.MinecraftResource.GetName()),
		armnetwork.VirtualNetwork{
			Name:     to.Ptr(fmt.Sprintf("%s-vnet", args.MinecraftResource.GetName())),
			Location: to.Ptr(args.MinecraftResource.GetRegion()),
			Tags:     getTags(args.MinecraftResource.GetEdition()),
			Properties: &armnetwork.VirtualNetworkPropertiesFormat{
				AddressSpace: &armnetwork.AddressSpace{
					AddressPrefixes: []*string{to.Ptr("10.0.0.0/8")},
				},
				Subnets: []*armnetwork.Subnet{
					{
						Name: to.Ptr(fmt.Sprintf("%s-snet", args.MinecraftResource.GetName())),
						Properties: &armnetwork.SubnetPropertiesFormat{
							AddressPrefix: to.Ptr("10.0.0.0/16"),
						},
					},
				},
			},
		},
		nil,
	)
	if err != nil {
		return nil, err
	}
	vnet, err := vnetPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	zap.S().Infow("Azure virtual network created", "name", vnet.Name)

	publicIPAddressesClient, err := armnetwork.NewPublicIPAddressesClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	publicIPAdressPoller, err := publicIPAddressesClient.BeginCreateOrUpdate(
		ctx,
		*group.Name,
		fmt.Sprintf("%s-ip", args.MinecraftResource.GetName()),
		armnetwork.PublicIPAddress{
			Name:     to.Ptr(fmt.Sprintf("%s-ip", args.MinecraftResource.GetName())),
			Location: to.Ptr(args.MinecraftResource.GetRegion()),
			Properties: &armnetwork.PublicIPAddressPropertiesFormat{
				PublicIPAddressVersion:   to.Ptr(armnetwork.IPVersionIPv4),
				PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodStatic),
			},
			Tags: getTags(args.MinecraftResource.GetEdition()),
		}, nil,
	)
	if err != nil {
		return nil, err
	}
	ip, err := publicIPAdressPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	zap.S().Infow("Azure public ip created", "name", ip.Name)

	interfacesClient, err := armnetwork.NewInterfacesClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}

	interfacesPoller, err := interfacesClient.BeginCreateOrUpdate(
		context.Background(),
		*group.Name,
		fmt.Sprintf("%s-nic", args.MinecraftResource.GetName()),
		armnetwork.Interface{
			Name:     to.Ptr(fmt.Sprintf("%s-nic", args.MinecraftResource.GetName())),
			Location: group.Location,
			Properties: &armnetwork.InterfacePropertiesFormat{
				IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
					{
						Name: to.Ptr("ipConfig1"),
						Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
							Subnet:                    vnet.Properties.Subnets[0],
							PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
							PublicIPAddress:           &ip.PublicIPAddress,
						},
					},
				},
			},
			Tags: getTags(args.MinecraftResource.GetEdition()),
		}, nil)
	if err != nil {
		return nil, err
	}
	nic, err := interfacesPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	zap.S().Infow("Azure network interface controller created", "name", nic.Name)
	var mount string
	var diskID *string
	if args.MinecraftResource.GetVolumeSize() > 0 {
		disksClient, err := armcompute.NewDisksClient(a.subscriptionID, a.credential, nil)
		if err != nil {
			return nil, err
		}
		diskPoller, err := disksClient.BeginCreateOrUpdate(
			context.Background(),
			*group.Name,
			fmt.Sprintf("%s-vol", args.MinecraftResource.GetName()),
			armcompute.Disk{
				Location: group.Location,
				Properties: &armcompute.DiskProperties{
					CreationData: &armcompute.CreationData{
						CreateOption: to.Ptr(armcompute.DiskCreateOptionEmpty),
					},
					DiskSizeGB: to.Ptr(int32(args.MinecraftResource.GetVolumeSize())),
				},
			}, nil)
		if err != nil {
			return nil, err
		}
		disk, err := diskPoller.PollUntilDone(ctx, nil)
		if err != nil {
			return nil, err
		}
		diskID = disk.ID
		mount = "sda"
		zap.S().Infow("Azure managed disk created", "name", disk.Name)
	}
	virtualMachinesClient, err := armcompute.NewVirtualMachinesClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	publicKey, err := cloud.GetSSHPublicKey(args)
	if err != nil {
		return nil, err
	}

	userData, err := a.tmpl.GetTemplate(args.MinecraftResource, &minctlTemplate.CreateUpdateTemplateArgs{Mount: mount, Name: minctlTemplate.GetTemplateCloudConfigName(args.MinecraftResource.IsProxyServer())})
	if err != nil {
		return nil, err
	}

	priority := armcompute.VirtualMachinePriorityTypesRegular
	var evictionPolicy armcompute.VirtualMachineEvictionPolicyTypes
	if args.MinecraftResource.IsSpot() {
		priority = armcompute.VirtualMachinePriorityTypesSpot
		evictionPolicy = armcompute.VirtualMachineEvictionPolicyTypesDeallocate
	}
	image := &armcompute.ImageReference{
		Publisher: to.Ptr("Canonical"),
		Offer:     to.Ptr("0001-com-ubuntu-minimal-jammy-daily"),
		SKU:       to.Ptr("minimal-22_04-daily-lts-gen2"),
		Version:   to.Ptr("latest"),
	}
	if args.MinecraftResource.IsArm() {
		image.Offer = to.Ptr("0001-com-ubuntu-server-jammy")
		image.SKU = to.Ptr("22_04-lts-arm64")
	}
	vmOptions := armcompute.VirtualMachine{
		Location: group.Location,
		Properties: &armcompute.VirtualMachineProperties{
			Priority:       to.Ptr(priority),
			EvictionPolicy: to.Ptr(evictionPolicy),
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes(args.MinecraftResource.GetSize())),
			},
			StorageProfile: &armcompute.StorageProfile{
				ImageReference: image,
			},
			OSProfile: &armcompute.OSProfile{
				ComputerName:  to.Ptr(args.MinecraftResource.GetName()),
				AdminUsername: to.Ptr("ubuntu"),
				CustomData:    to.Ptr(base64.StdEncoding.EncodeToString([]byte(userData))),
				LinuxConfiguration: &armcompute.LinuxConfiguration{
					SSH: &armcompute.SSHConfiguration{
						PublicKeys: []*armcompute.SSHPublicKey{
							{
								Path:    to.Ptr("/home/ubuntu/.ssh/authorized_keys"),
								KeyData: publicKey,
							},
						},
					},
				},
			},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
					{
						ID: nic.ID,
						Properties: &armcompute.NetworkInterfaceReferenceProperties{
							Primary: to.Ptr(true),
						},
					},
				},
			},
		},
		Tags: getTags(args.MinecraftResource.GetEdition()),
	}
	if args.MinecraftResource.GetVolumeSize() > 0 {
		vmOptions.Properties.StorageProfile.DataDisks = []*armcompute.DataDisk{{
			CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesAttach),
			Lun:          to.Ptr(int32(0)),
			ManagedDisk: &armcompute.ManagedDiskParameters{
				ID: diskID,
			},
		}}
	}

	vmPoller, err := virtualMachinesClient.BeginCreateOrUpdate(
		context.Background(),
		*group.Name,
		args.MinecraftResource.GetName(),
		vmOptions,
		nil,
	)
	if err != nil {
		return nil, err
	}
	instance, err := vmPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	zap.S().Infow("Azure virtual machine created", "name", instance.Name)
	vmStartPoller, err := virtualMachinesClient.BeginStart(
		ctx,
		*group.Name,
		*instance.Name,
		nil,
	)
	if err != nil {
		return nil, err
	}
	_, err = vmStartPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	zap.S().Infow("Azure virtual machine started", "name", instance.Name, "ip", *ip.Properties.IPAddress, "id", instance.Name)

	return &automation.ResourceResults{
		ID:       *instance.Name,
		Name:     *instance.Name,
		Region:   *group.Location,
		PublicIP: *ip.Properties.IPAddress,
		Tags:     strings.Join(getTagKeys(instance.Tags), ","),
	}, err
}

func (a *Azure) DeleteServer(id string, args automation.ServerArgs) error {
	ctx := context.Background()
	resourceGroupName := fmt.Sprintf("%s-rg", args.MinecraftResource.GetName())
	resourceGroupsClient, err := armresources.NewResourceGroupsClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return err
	}
	pollerResp, err := resourceGroupsClient.BeginDelete(
		ctx,
		resourceGroupName, &armresources.ResourceGroupsClientBeginDeleteOptions{
			ForceDeletionTypes: to.Ptr("Microsoft.Compute/virtualMachines"),
		})
	if err != nil {
		return err
	}
	_, err = pollerResp.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}
	zap.S().Infow("Azure resource group deleted", "name", resourceGroupName)
	return nil
}

func (a *Azure) ListServer() ([]automation.ResourceResults, error) {
	ctx := context.Background()
	virtualMachinesClient, err := armcompute.NewVirtualMachinesClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	pager := virtualMachinesClient.NewListAllPager(&armcompute.VirtualMachinesClientListAllOptions{})
	var result []automation.ResourceResults
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, instance := range nextResult.Value {
			for key := range instance.Tags {
				if key == common.InstanceTag {

					publicIPAddressesClient, err := armnetwork.NewPublicIPAddressesClient(a.subscriptionID, a.credential, nil)
					if err != nil {
						return nil, err
					}
					ip, err := publicIPAddressesClient.Get(
						context.Background(),
						fmt.Sprintf("%s-rg", *instance.Name),
						fmt.Sprintf("%s-ip", *instance.Name),
						nil)
					if err != nil {
						return nil, err
					}
					result = append(result, automation.ResourceResults{
						ID:       *instance.Name,
						Name:     *instance.Name,
						Region:   *instance.Location,
						PublicIP: *ip.Properties.IPAddress,
						Tags:     strings.Join(getTagKeys(instance.Tags), ","),
					})
				}
			}
		}
	}
	if len(result) > 0 {
		zap.S().Infow("Azure list all minectl vms", "list", result)
	} else {
		zap.S().Infow("No minectl vms found")
	}
	return result, nil
}

func (a *Azure) UpdateServer(id string, args automation.ServerArgs) error {
	server, err := a.GetServer(id, args)
	if err != nil {
		return err
	}
	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, server.PublicIP, "ubuntu")
	err = remoteCommand.UpdateServer(args.MinecraftResource)
	if err != nil {
		return err
	}
	zap.S().Infow("minectl server updated", "name", server.Name)
	return nil
}

func (a *Azure) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	server, err := a.GetServer(id, args)
	if err != nil {
		return err
	}
	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, server.PublicIP, "ubuntu")

	// as we are not allowed to login via root user, we need to add sudo to the command
	source := filepath.Join("/tmp", filepath.Base(plugin))
	err = remoteCommand.TransferFile(plugin, source, args.MinecraftResource.GetSSHPort())
	if err != nil {
		return err
	}
	_, err = remoteCommand.ExecuteCommand(fmt.Sprintf("sudo mv %s %s\nsudo systemctl restart minecraft.service", source, destination), args.MinecraftResource.GetSSHPort())
	if err != nil {
		return err
	}
	zap.S().Infow("Minecraft plugin uploaded", "plugin", plugin, "server", server.Name)
	return nil
}

func (a *Azure) GetServer(id string, args automation.ServerArgs) (*automation.ResourceResults, error) {
	virtualMachinesClient, err := armcompute.NewVirtualMachinesClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	instance, err := virtualMachinesClient.Get(
		context.Background(),
		fmt.Sprintf("%s-rg", args.MinecraftResource.GetName()),
		id,
		&armcompute.VirtualMachinesClientGetOptions{Expand: nil},
	)
	if err != nil {
		return nil, err
	}
	publicIPAddressesClient, err := armnetwork.NewPublicIPAddressesClient(a.subscriptionID, a.credential, nil)
	if err != nil {
		return nil, err
	}
	ip, err := publicIPAddressesClient.Get(
		context.Background(),
		fmt.Sprintf("%s-rg", *instance.Name),
		fmt.Sprintf("%s-ip", *instance.Name),
		nil)
	if err != nil {
		return nil, err
	}
	return &automation.ResourceResults{
		ID:       *instance.Name,
		Name:     *instance.Name,
		Region:   *instance.Location,
		PublicIP: *ip.Properties.IPAddress,
		Tags:     strings.Join(getTagKeys(instance.Tags), ","),
	}, err
}
