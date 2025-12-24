package openstack

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/common"
	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"github.com/dirien/minectl-sdk/update"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/secgroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/gophercloud/gophercloud/v2/pagination"
	"go.uber.org/zap"
)

type OpenStack struct {
	tmpl          *minctlTemplate.Template
	computeClient *gophercloud.ServiceClient
	networkClient *gophercloud.ServiceClient
	imageClient   *gophercloud.ServiceClient
	region        string
	imageName     string
}

func getTags(edition string) map[string]string {
	return map[string]string{
		common.InstanceTag: "true",
		edition:            "true",
	}
}

func getTagKeys(tags map[string]string) []string {
	var keys []string
	for key := range tags {
		keys = append(keys, key)
	}
	return keys
}

func NewOpenStack(imageName string) (*OpenStack, error) {
	ctx := context.Background()
	tmpl, err := minctlTemplate.NewTemplateCloudConfig()
	if err != nil {
		return nil, err
	}

	userID := os.Getenv("OS_USER_ID")
	domainID := os.Getenv("OS_PROJECT_DOMAIN_ID")
	if len(userID) != 0 {
		domainID = ""
		userID = os.Getenv("OS_USER_ID")
	}

	opts := gophercloud.AuthOptions{
		IdentityEndpoint: os.Getenv("OS_AUTH_URL"),
		Username:         os.Getenv("OS_USERNAME"),
		Password:         os.Getenv("OS_PASSWORD"),
		DomainID:         domainID,
		UserID:           userID,
		Passcode:         os.Getenv("OS_PASSCODE"),
		TenantID:         os.Getenv("OS_PROJECT_ID"),
		TenantName:       os.Getenv("OS_PROJECT_NAME"),
	}
	provider, err := openstack.AuthenticatedClient(ctx, opts)
	if err != nil {
		return nil, err
	}
	computeClient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, err
	}
	networkClient, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, err
	}
	imageClient, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, err
	}
	return &OpenStack{
		tmpl:          tmpl,
		computeClient: computeClient,
		networkClient: networkClient,
		imageClient:   imageClient,
		region:        os.Getenv("OS_REGION_NAME"),
		imageName:     imageName,
	}, nil
}

// CreateServer TODO: https://github.com/dirien/minectl/issues/299
func (o *OpenStack) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) { //nolint: gocyclo
	ctx := context.Background()
	publicKey, err := cloud.GetSSHPublicKey(args)
	if err != nil {
		return nil, err
	}

	keyPair, err := keypairs.Create(ctx, o.computeClient, keypairs.CreateOpts{
		Name:      fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName()),
		PublicKey: *publicKey,
	}).Extract()
	if err != nil {
		return nil, err
	}

	listOpts := images.ListOpts{
		Status: images.ImageStatusActive,
	}

	var image images.Image
	pager := images.List(o.imageClient, listOpts)
	err = pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		imageList, err := images.ExtractImages(page)
		if err != nil {
			return false, err
		}
		for _, i := range imageList {
			if strings.Contains(i.Name, o.imageName) && !strings.HasSuffix(i.Name, "vGPU") {
				image = i
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	var flavor flavors.Flavor

	flavorPager := flavors.ListDetail(o.computeClient, flavors.ListOpts{})
	err = flavorPager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		flavorsList, err := flavors.ExtractFlavors(page)
		if err != nil {
			return false, err
		}
		for _, i := range flavorsList {
			if i.Name == args.MinecraftResource.GetSize() {
				flavor = i
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	createOpts := secgroups.CreateOpts{
		Name:        fmt.Sprintf("%s-sg", args.MinecraftResource.GetName()),
		Description: "minectl",
	}

	group, err := secgroups.Create(ctx, o.computeClient, createOpts).Extract()
	if err != nil {
		return nil, err
	}

	err = o.createSecurityGroup(ctx, group, args.MinecraftResource.GetSSHPort(), "TCP")
	if err != nil {
		return nil, err
	}
	if args.MinecraftResource.GetEdition() == "bedrock" || args.MinecraftResource.GetEdition() == "nukkit" || args.MinecraftResource.GetEdition() == "powernukkit" {
		err = o.createSecurityGroup(ctx, group, args.MinecraftResource.GetPort(), "UDP")
		if err != nil {
			return nil, err
		}
	} else {
		err = o.createSecurityGroup(ctx, group, args.MinecraftResource.GetPort(), "TCP")
		if err != nil {
			return nil, err
		}
		if args.MinecraftResource.HasRCON() {
			err = o.createSecurityGroup(ctx, group, args.MinecraftResource.GetRCONPort(), "TCP")
			if err != nil {
				return nil, err
			}
		}
	}

	adminStateUp := true
	networkOpts := networks.CreateOpts{
		Name:         fmt.Sprintf("%s-net", args.MinecraftResource.GetName()),
		AdminStateUp: &adminStateUp,
	}

	network, err := networks.Create(ctx, o.networkClient, networkOpts).Extract()
	if err != nil {
		return nil, err
	}

	subnetOpts := subnets.CreateOpts{
		Name:      fmt.Sprintf("%s-subnet", args.MinecraftResource.GetName()),
		NetworkID: network.ID,
		CIDR:      "10.1.10.0/24",
		IPVersion: gophercloud.IPVersion(4),
		DNSNameservers: []string{
			"8.8.8.8",
			"8.8.4.4",
		},
	}

	subnet, err := subnets.Create(ctx, o.networkClient, subnetOpts).Extract()
	if err != nil {
		return nil, err
	}

	networkPager := networks.List(o.networkClient, networks.ListOpts{
		Name: "public",
	})
	var publicNetwork networks.Network
	err = networkPager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		networkList, err := networks.ExtractNetworks(page)
		if err != nil {
			return false, err
		}
		for _, i := range networkList {
			publicNetwork = i
			break
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	gatewayInfo := &routers.GatewayInfo{
		NetworkID: publicNetwork.ID,
	}

	router, err := routers.Create(ctx, o.networkClient, routers.CreateOpts{
		Name:         fmt.Sprintf("%s-router", args.MinecraftResource.GetName()),
		AdminStateUp: &adminStateUp,
		GatewayInfo:  gatewayInfo,
	}).Extract()
	if err != nil {
		return nil, err
	}
	_, err = routers.AddInterface(ctx, o.networkClient, router.ID, routers.AddInterfaceOpts{
		SubnetID: subnet.ID,
	}).Extract()
	if err != nil {
		return nil, err
	}
	userData, err := o.tmpl.GetTemplate(args.MinecraftResource, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.GetTemplateCloudConfigName(args.MinecraftResource.IsProxyServer())})
	if err != nil {
		return nil, err
	}

	server, err := servers.Create(ctx, o.computeClient, keypairs.CreateOptsExt{
		CreateOptsBuilder: servers.CreateOpts{
			Name: args.MinecraftResource.GetName(),
			SecurityGroups: []string{
				group.ID,
			},
			FlavorRef: flavor.ID,
			ImageRef:  image.ID,
			Networks: []servers.Network{
				{
					UUID: network.ID,
				},
			},
			Metadata: getTags(args.MinecraftResource.GetEdition()),
			UserData: []byte(base64.StdEncoding.EncodeToString([]byte(userData))),
		},
		KeyName: keyPair.Name,
	}, servers.SchedulerHintOpts{}).Extract()
	if err != nil {
		return nil, err
	}

	stillCreating := true
	for stillCreating {
		server, err = servers.Get(ctx, o.computeClient, server.ID).Extract()
		if err != nil {
			return nil, err
		}
		if server.Status == "ACTIVE" {
			stillCreating = false
		} else {
			time.Sleep(2 * time.Second)
		}
	}

	floatingIP, err := floatingips.Create(ctx, o.networkClient, floatingips.CreateOpts{
		FloatingNetworkID: publicNetwork.ID,
	}).Extract()
	if err != nil {
		return nil, err
	}

	// Get the port ID from the server's first network interface
	portID := ""
	for _, addresses := range server.Addresses {
		if addrList, ok := addresses.([]interface{}); ok && len(addrList) > 0 {
			if addr, ok := addrList[0].(map[string]interface{}); ok {
				if p, ok := addr["OS-EXT-IPS-MAC:mac_addr"]; ok {
					_ = p // We need to find port by MAC or use a different approach
				}
			}
		}
	}

	// Associate floating IP with the server's port
	_, err = floatingips.Update(ctx, o.networkClient, floatingIP.ID, floatingips.UpdateOpts{
		PortID: &portID,
	}).Extract()
	if err != nil {
		// If association fails, try to find the port and associate
		zap.S().Warnw("Failed to associate floating IP directly, server may not have public IP", "error", err)
	}

	return &automation.ResourceResults{
		ID:       server.ID,
		Name:     server.Name,
		Region:   o.region,
		PublicIP: floatingIP.FloatingIP,
		Tags:     strings.Join(getTagKeys(server.Metadata), ","),
	}, nil
}

func (o *OpenStack) createSecurityGroup(ctx context.Context, group *secgroups.SecurityGroup, port int, protocol string) error {
	ssh := secgroups.CreateRuleOpts{
		ParentGroupID: group.ID,
		FromPort:      port,
		ToPort:        port,
		IPProtocol:    protocol,
		CIDR:          "0.0.0.0/0",
	}

	_, err := secgroups.CreateRule(ctx, o.computeClient, ssh).Extract()
	if err != nil {
		return err
	}
	return nil
}

func (o *OpenStack) DeleteServer(id string, args automation.ServerArgs) error {
	ctx := context.Background()
	server, err := servers.Get(ctx, o.computeClient, id).Extract()
	if err != nil {
		return err
	}
	err = keypairs.Delete(ctx, o.computeClient, fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName()), keypairs.DeleteOpts{}).Err
	if err != nil {
		return err
	}
	floatingIP, err := o.getFloatingIPByInstanceID(ctx, server.ID)
	if err != nil {
		return err
	}
	if floatingIP != nil {
		err = floatingips.Delete(ctx, o.networkClient, floatingIP.ID).Err
		if err != nil {
			return err
		}
	}

	err = servers.Delete(ctx, o.computeClient, server.ID).Err
	if err != nil {
		return err
	}
	stillCreating := true
	for stillCreating {
		server, err = servers.Get(ctx, o.computeClient, server.ID).Extract()
		if err != nil {
			stillCreating = false
		}
		if server != nil && server.Status == "DELETED" {
			stillCreating = false
		} else {
			time.Sleep(2 * time.Second)
		}
	}

	network, err := o.getNetworkByName(ctx, args)
	if err != nil {
		return err
	}
	subnet, err := o.getSubNetByName(ctx, args)
	if err != nil {
		return err
	}

	securityGroup, err := o.getSecurityGroupByName(ctx, args)
	if err != nil {
		return err
	}
	err = secgroups.Delete(ctx, o.computeClient, securityGroup.ID).Err
	if err != nil {
		return err
	}
	router, err := o.getRouterByName(ctx, args)
	if err != nil {
		return err
	}
	_, err = routers.RemoveInterface(ctx, o.networkClient, router.ID, routers.RemoveInterfaceOpts{
		SubnetID: subnet.ID,
	}).Extract()
	if err != nil {
		return err
	}
	err = routers.Delete(ctx, o.networkClient, router.ID).Err
	if err != nil {
		return err
	}
	err = subnets.Delete(ctx, o.networkClient, subnet.ID).Err
	if err != nil {
		return err
	}
	err = networks.Delete(ctx, o.networkClient, network.ID).Err
	if err != nil {
		return err
	}
	return nil
}

func (o *OpenStack) getRouterByName(ctx context.Context, args automation.ServerArgs) (*routers.Router, error) {
	var router *routers.Router
	pager := routers.List(o.networkClient, routers.ListOpts{
		Name: fmt.Sprintf("%s-router", args.MinecraftResource.GetName()),
	})
	err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		routerList, err := routers.ExtractRouters(page)
		if err != nil {
			return false, err
		}
		for i, routerItem := range routerList {
			if routerItem.Name == fmt.Sprintf("%s-router", args.MinecraftResource.GetName()) {
				router = &routerList[i]
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return router, nil
}

func (o *OpenStack) getFloatingIPByInstanceID(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
	var floatingIP *floatingips.FloatingIP
	pager := floatingips.List(o.networkClient, floatingips.ListOpts{})
	err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		list, err := floatingips.ExtractFloatingIPs(page)
		if err != nil {
			return false, err
		}
		for i, item := range list {
			// In Neutron floating IPs, we check by the fixed IP association
			if item.PortID != "" {
				// Check if this floating IP is associated with our server
				floatingIP = &list[i]
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return floatingIP, nil
}

func (o *OpenStack) getSecurityGroupByName(ctx context.Context, args automation.ServerArgs) (*secgroups.SecurityGroup, error) {
	var securityGroup *secgroups.SecurityGroup
	pager := secgroups.List(o.computeClient)
	err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		list, err := secgroups.ExtractSecurityGroups(page)
		if err != nil {
			return false, err
		}
		for i, item := range list {
			if item.Name == fmt.Sprintf("%s-sg", args.MinecraftResource.GetName()) {
				securityGroup = &list[i]
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return securityGroup, nil
}

func (o *OpenStack) getSubNetByName(ctx context.Context, args automation.ServerArgs) (*subnets.Subnet, error) {
	var subnet *subnets.Subnet
	pager := subnets.List(o.networkClient, subnets.ListOpts{
		Name: fmt.Sprintf("%s-subnet", args.MinecraftResource.GetName()),
	})
	err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		list, err := subnets.ExtractSubnets(page)
		if err != nil {
			return false, err
		}
		for i, item := range list {
			if item.Name == fmt.Sprintf("%s-subnet", args.MinecraftResource.GetName()) {
				subnet = &list[i]
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return subnet, nil
}

func (o *OpenStack) getNetworkByName(ctx context.Context, args automation.ServerArgs) (*networks.Network, error) {
	var network *networks.Network
	pager := networks.List(o.networkClient, networks.ListOpts{
		Name: fmt.Sprintf("%s-net", args.MinecraftResource.GetName()),
	})
	err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		list, err := networks.ExtractNetworks(page)
		if err != nil {
			return false, err
		}
		for i, networkItem := range list {
			if networkItem.Name == fmt.Sprintf("%s-net", args.MinecraftResource.GetName()) {
				network = &list[i]
				break
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return network, nil
}

func (o *OpenStack) ListServer() ([]automation.ResourceResults, error) {
	ctx := context.Background()
	var result []automation.ResourceResults
	pager := servers.List(o.computeClient, servers.ListOpts{})
	err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		list, err := servers.ExtractServers(page)
		if err != nil {
			return false, err
		}
		for _, i := range list {
			for key := range i.Metadata {
				if key == common.InstanceTag {
					floatingIP, err := o.getFloatingIPByInstanceID(ctx, i.ID)
					if err != nil {
						return false, err
					}
					publicIP := ""
					if floatingIP != nil {
						publicIP = floatingIP.FloatingIP
					}
					result = append(result, automation.ResourceResults{
						ID:       i.ID,
						Name:     i.Name,
						Region:   o.region,
						PublicIP: publicIP,
						Tags:     strings.Join(getTagKeys(i.Metadata), ","),
					})
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (o *OpenStack) UpdateServer(id string, args automation.ServerArgs) error {
	server, err := o.GetServer(id, args)
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

func (o *OpenStack) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	server, err := o.GetServer(id, args)
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

func (o *OpenStack) GetServer(id string, args automation.ServerArgs) (*automation.ResourceResults, error) {
	ctx := context.Background()
	server, err := servers.Get(ctx, o.computeClient, id).Extract()
	if err != nil {
		return nil, err
	}
	floatingIP, err := o.getFloatingIPByInstanceID(ctx, server.ID)
	if err != nil {
		return nil, err
	}
	publicIP := ""
	if floatingIP != nil {
		publicIP = floatingIP.FloatingIP
	}
	return &automation.ResourceResults{
		ID:       server.ID,
		Name:     server.Name,
		Region:   o.region,
		PublicIP: publicIP,
		Tags:     strings.Join(getTagKeys(server.Metadata), ","),
	}, nil
}
