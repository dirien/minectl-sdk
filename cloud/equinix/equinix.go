package equinix

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/common"
	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"github.com/dirien/minectl-sdk/update"
	metal "github.com/equinix/equinix-sdk-go/services/metalv1"
)

type Equinix struct {
	client  *metal.APIClient
	project string
	tmpl    *minctlTemplate.Template
}

func NewEquinix(apiKey, project string) (*Equinix, error) {
	tmpl, err := minctlTemplate.NewTemplateBash()
	if err != nil {
		return nil, err
	}

	configuration := metal.NewConfiguration()
	configuration.AddDefaultHeader("X-Auth-Token", apiKey)

	return &Equinix{
		client:  metal.NewAPIClient(configuration),
		project: project,
		tmpl:    tmpl,
	}, nil
}

func (e *Equinix) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	ctx := context.Background()
	publicKey, err := cloud.GetSSHPublicKey(args)
	if err != nil {
		return nil, err
	}

	userData, err := e.tmpl.GetTemplate(args.MinecraftResource, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.GetTemplateBashName(args.MinecraftResource.IsProxyServer())})
	if err != nil {
		return nil, err
	}

	server, _, err := e.client.DevicesApi.CreateDevice(ctx, e.project).CreateDeviceRequest(metal.CreateDeviceRequest{
		DeviceCreateInMetroInput: &metal.DeviceCreateInMetroInput{
			Hostname:        metal.PtrString(args.MinecraftResource.GetName()),
			OperatingSystem: "ubuntu_22_04",
			Plan:            args.MinecraftResource.GetSize(),
			Tags:            []string{common.InstanceTag, args.MinecraftResource.GetEdition()},
			SshKeys: []metal.SSHKeyInput{
				{
					Label: metal.PtrString(fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName())),
					Key:   metal.PtrString(*publicKey),
				},
			},
			Userdata:     metal.PtrString(userData),
			BillingCycle: metal.DeviceCreateInputBillingCycle.Ptr(metal.DEVICECREATEINPUTBILLINGCYCLE_HOURLY),
			Metro:        args.MinecraftResource.GetRegion(),
			SpotInstance: metal.PtrBool(false),
		},
	}).Execute()
	if err != nil {
		return nil, err
	}
	stillCreating := true
	for stillCreating {
		server, _, err = e.client.DevicesApi.FindDeviceById(ctx, server.GetId()).Execute()
		if err != nil {
			return nil, err
		}

		if server.GetState() == metal.DEVICESTATE_ACTIVE && len(server.GetIpAddresses()) > 1 {
			stillCreating = false
		} else {
			time.Sleep(2 * time.Second)
		}
	}

	return &automation.ResourceResults{
		ID:       server.GetId(),
		Name:     server.GetHostname(),
		Region:   server.Metro.GetCode(),
		PublicIP: getIP4(server),
		Tags:     strings.Join(server.GetTags(), ","),
	}, err
}

func (e *Equinix) DeleteServer(id string, args automation.ServerArgs) error {
	ctx := context.Background()
	keys, _, err := e.client.SSHKeysApi.FindProjectSSHKeys(ctx, e.project).Query(fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName())).Execute()
	if err != nil {
		return err
	}
	for _, key := range keys.SshKeys {
		_, err := e.client.SSHKeysApi.DeleteSSHKey(ctx, key.GetId()).Execute()
		if err != nil {
			return err
		}

	}
	_, err = e.client.DevicesApi.DeleteDevice(ctx, id).ForceDelete(true).Execute()
	if err != nil {
		return err
	}
	return nil
}

func (e *Equinix) ListServer() ([]automation.ResourceResults, error) {
	ctx := context.Background()
	list, _, err := e.client.DevicesApi.FindProjectDevices(ctx, e.project).Search(common.InstanceTag).Execute()

	if err != nil {
		return nil, err
	}
	var result []automation.ResourceResults
	for _, server := range list.Devices {
		result = append(result, automation.ResourceResults{
			ID:       server.GetId(),
			Name:     server.GetHostname(),
			Region:   server.Metro.GetCode(),
			PublicIP: getIP4(&server),
			Tags:     strings.Join(server.GetTags(), ","),
		})
	}
	return result, nil
}

func (e *Equinix) UpdateServer(id string, args automation.ServerArgs) error {
	ctx := context.Background()
	instance, _, err := e.client.DevicesApi.FindDeviceById(ctx, id).Execute()
	if err != nil {
		return err
	}

	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, getIP4(instance), "root")
	err = remoteCommand.UpdateServer(args.MinecraftResource)
	if err != nil {
		return err
	}
	return nil
}

func getIP4(server *metal.Device) string {
	ip4 := ""
	for _, network := range server.GetIpAddresses() {
		if network.GetPublic() {
			ip4 = network.GetAddress()
			break
		}
	}
	return ip4
}

func (e *Equinix) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	ctx := context.Background()
	instance, _, err := e.client.DevicesApi.FindDeviceById(ctx, id).Execute()
	if err != nil {
		return err
	}

	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, getIP4(instance), "root")
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

func (e *Equinix) GetServer(id string, _ automation.ServerArgs) (*automation.ResourceResults, error) {
	ctx := context.Background()
	instance, _, err := e.client.DevicesApi.FindDeviceById(ctx, id).Execute()
	if err != nil {
		return nil, err
	}

	return &automation.ResourceResults{
		ID:       instance.GetId(),
		Name:     instance.GetHostname(),
		Region:   instance.Metro.GetCode(),
		PublicIP: getIP4(instance),
		Tags:     strings.Join(instance.GetTags(), ","),
	}, err
}
