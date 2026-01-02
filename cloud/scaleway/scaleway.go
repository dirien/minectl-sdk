// Package scaleway implements the Automation interface for Scaleway cloud provider.
package scaleway

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/common"
	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"github.com/dirien/minectl-sdk/update"
	iam "github.com/scaleway/scaleway-sdk-go/api/iam/v1alpha1"
	"github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

// Scaleway implements the Automation interface for Scaleway.
type Scaleway struct {
	instanceAPI    *instance.API
	iamAPI         *iam.API
	organizationID string
	tmpl           *minctlTemplate.Template
}

// NewScaleway creates a new Scaleway instance.
func NewScaleway(accessKey, secretKey, organizationID, region string) (*Scaleway, error) {
	zone, err := scw.ParseZone(region)
	if err != nil {
		return nil, err
	}

	client, err := scw.NewClient(
		scw.WithAuth(accessKey, secretKey),
		scw.WithDefaultOrganizationID(organizationID),
		scw.WithDefaultZone(zone),
	)
	if err != nil {
		return nil, err
	}
	tmpl, err := minctlTemplate.NewTemplateCloudConfig()
	if err != nil {
		return nil, err
	}
	return &Scaleway{
		instanceAPI:    instance.NewAPI(client),
		iamAPI:         iam.NewAPI(client),
		organizationID: organizationID,
		tmpl:           tmpl,
	}, nil
}

// CreateServer creates a new Minecraft server on Scaleway.
func (s *Scaleway) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	publicKey, err := cloud.GetSSHPublicKey(args)
	if err != nil {
		return nil, err
	}
	_, err = s.iamAPI.CreateSSHKey(&iam.CreateSSHKeyRequest{
		Name:      fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName()),
		PublicKey: *publicKey,
		ProjectID: s.organizationID,
	})
	if err != nil {
		return nil, err
	}
	server, err := s.instanceAPI.CreateServer(&instance.CreateServerRequest{
		Name:              args.MinecraftResource.GetName(),
		CommercialType:    args.MinecraftResource.GetSize(),
		Image:             scw.StringPtr("ubuntu_jammy"),
		Tags:              []string{"minectl"},
		DynamicIPRequired: scw.BoolPtr(true),
	})
	if err != nil {
		return nil, err
	}

	var mount string
	if args.MinecraftResource.GetVolumeSize() > 0 {
		volume, err := s.instanceAPI.CreateVolume(&instance.CreateVolumeRequest{
			Name:       fmt.Sprintf("%s-vol", args.MinecraftResource.GetName()),
			VolumeType: instance.VolumeVolumeTypeBSSD,
			Size:       scw.SizePtr(scw.Size(args.MinecraftResource.GetVolumeSize()) * scw.GB), //nolint:gosec // volume size is validated
		})
		if err != nil {
			return nil, err
		}
		_, err = s.instanceAPI.AttachVolume(&instance.AttachVolumeRequest{
			VolumeID: volume.Volume.ID,
			ServerID: server.Server.ID,
		})
		if err != nil {
			return nil, err
		}
		mount = "sda"
	}
	userData, err := s.tmpl.GetTemplate(args.MinecraftResource, &minctlTemplate.CreateUpdateTemplateArgs{Mount: mount, Name: minctlTemplate.GetTemplateCloudConfigName(args.MinecraftResource.IsProxyServer())})
	if err != nil {
		return nil, err
	}
	err = s.instanceAPI.SetServerUserData(&instance.SetServerUserDataRequest{
		ServerID: server.Server.ID,
		Key:      "cloud-init",
		Content:  strings.NewReader(userData),
	})
	if err != nil {
		return nil, err
	}

	duration := 2 * time.Second
	err = s.instanceAPI.ServerActionAndWait(&instance.ServerActionAndWaitRequest{
		ServerID:      server.Server.ID,
		Action:        instance.ServerActionPoweron,
		RetryInterval: &duration,
	})
	if err != nil {
		return nil, err
	}

	getServer, err := s.instanceAPI.GetServer(&instance.GetServerRequest{
		ServerID: server.Server.ID,
	})
	if err != nil {
		return nil, err
	}

	var publicIP string
	if len(getServer.Server.PublicIPs) > 0 {
		publicIP = getServer.Server.PublicIPs[0].Address.String()
	}
	return &automation.ResourceResults{
		ID:       server.Server.ID,
		Name:     server.Server.Name,
		Region:   server.Server.Zone.String(),
		PublicIP: publicIP,
		Tags:     strings.Join(server.Server.Tags, ","),
	}, err
}

// DeleteServer deletes a Minecraft server on Scaleway.
func (s *Scaleway) DeleteServer(id string, args automation.ServerArgs) error {
	getServer, err := s.instanceAPI.GetServer(&instance.GetServerRequest{
		ServerID: id,
	})
	if err != nil {
		return err
	}
	duration := 2 * time.Second
	err = s.instanceAPI.ServerActionAndWait(&instance.ServerActionAndWaitRequest{
		ServerID:      getServer.Server.ID,
		Action:        instance.ServerActionPoweroff,
		RetryInterval: &duration,
	})
	if err != nil {
		return err
	}
	err = s.instanceAPI.DeleteServer(&instance.DeleteServerRequest{
		ServerID: getServer.Server.ID,
	})
	if err != nil {
		return err
	}
	for _, volume := range getServer.Server.Volumes {
		err := s.instanceAPI.DeleteVolume(&instance.DeleteVolumeRequest{
			VolumeID: volume.ID,
		})
		if err != nil {
			return err
		}
	}
	keys, err := s.iamAPI.ListSSHKeys(&iam.ListSSHKeysRequest{
		Name: scw.StringPtr(fmt.Sprintf("%s-ssh", args.MinecraftResource.GetName())),
	})
	if err != nil {
		return err
	}
	for _, key := range keys.SSHKeys {
		err := s.iamAPI.DeleteSSHKey(&iam.DeleteSSHKeyRequest{
			SSHKeyID: key.ID,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ListServer lists all Minecraft servers on Scaleway.
func (s *Scaleway) ListServer() ([]automation.ResourceResults, error) {
	servers, err := s.instanceAPI.ListServers(&instance.ListServersRequest{
		Tags: []string{common.InstanceTag},
	})
	if err != nil {
		return nil, err
	}
	var result []automation.ResourceResults
	for _, server := range servers.Servers {
		var publicIP string
		if len(server.PublicIPs) > 0 {
			publicIP = server.PublicIPs[0].Address.String()
		}
		result = append(result, automation.ResourceResults{
			ID:       server.ID,
			PublicIP: publicIP,
			Name:     server.Name,
			Region:   server.Zone.String(),
			Tags:     strings.Join(server.Tags, ","),
		})
	}
	return result, nil
}

// UpdateServer updates a Minecraft server on Scaleway.
func (s *Scaleway) UpdateServer(id string, args automation.ServerArgs) error {
	inst, err := s.instanceAPI.GetServer(&instance.GetServerRequest{
		ServerID: id,
	})
	if err != nil {
		return err
	}

	var publicIP string
	if len(inst.Server.PublicIPs) > 0 {
		publicIP = inst.Server.PublicIPs[0].Address.String()
	}
	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, publicIP, "root")
	err = remoteCommand.UpdateServer(args.MinecraftResource)
	if err != nil {
		return err
	}
	return nil
}

// UploadPlugin uploads a plugin to a Minecraft server on Scaleway.
func (s *Scaleway) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	inst, err := s.instanceAPI.GetServer(&instance.GetServerRequest{
		ServerID: id,
	})
	if err != nil {
		return err
	}

	var publicIP string
	if len(inst.Server.PublicIPs) > 0 {
		publicIP = inst.Server.PublicIPs[0].Address.String()
	}
	remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, publicIP, "root")
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

// GetServer gets a Minecraft server on Scaleway.
func (s *Scaleway) GetServer(id string, _ automation.ServerArgs) (*automation.ResourceResults, error) {
	inst, err := s.instanceAPI.GetServer(&instance.GetServerRequest{
		ServerID: id,
	})
	if err != nil {
		return nil, err
	}
	var publicIP string
	if len(inst.Server.PublicIPs) > 0 {
		publicIP = inst.Server.PublicIPs[0].Address.String()
	}
	return &automation.ResourceResults{
		ID:       inst.Server.ID,
		Name:     inst.Server.Name,
		Region:   inst.Server.Zone.String(),
		PublicIP: publicIP,
		Tags:     strings.Join(inst.Server.Tags, ","),
	}, err
}
