// Package gce implements the Automation interface for Google Compute Engine.
package gce

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/common"
	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"github.com/dirien/minectl-sdk/update"
	"github.com/pkg/errors"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/oslogin/v1"
)

const doneStatus = "DONE"

// Credentials represents GCE service account credentials.
type Credentials struct {
	ProjectID   string `json:"projectId"`
	ClientEmail string `json:"clientEmail"`
	ClientID    string `json:"clientId"`
}

// GCE implements the Automation interface for Google Compute Engine.
type GCE struct {
	client             *compute.Service
	user               *oslogin.Service
	projectID          string
	serviceAccountName string
	serviceAccountID   string
	zone               string
	tmpl               *minctlTemplate.Template
}

// NewGCE creates a new GCE instance using Application Default Credentials (ADC).
// Authentication is handled automatically via the standard Google credential chain:
// 1. GOOGLE_APPLICATION_CREDENTIALS environment variable (path to service account JSON)
// 2. gcloud CLI authentication (gcloud auth application-default login)
// 3. GCE metadata service (when running on Google Cloud)
//
// Required environment variables:
// - GOOGLE_PROJECT: The GCP project ID
// - GOOGLE_SERVICE_ACCOUNT_EMAIL: The service account email (for OS Login SSH access)
//
// Optional:
// - GOOGLE_APPLICATION_CREDENTIALS: Path to service account JSON (if not using gcloud CLI auth)
func NewGCE(zone string) (*GCE, error) {
	ctx := context.Background()

	// Get project ID from environment
	projectID := os.Getenv("GOOGLE_PROJECT")
	if projectID == "" {
		return nil, errors.New("GOOGLE_PROJECT environment variable is required")
	}

	// Get service account email from environment (required for OS Login)
	serviceAccountEmail := os.Getenv("GOOGLE_SERVICE_ACCOUNT_EMAIL")
	if serviceAccountEmail == "" {
		return nil, errors.New("GOOGLE_SERVICE_ACCOUNT_EMAIL environment variable is required")
	}

	// Use Application Default Credentials
	// This supports:
	// - GOOGLE_APPLICATION_CREDENTIALS env var pointing to service account JSON
	// - gcloud auth application-default login
	// - GCE metadata service
	creds, err := google.FindDefaultCredentials(ctx, compute.ComputeScope, oslogin.ComputeScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find default credentials. Run 'gcloud auth application-default login' or set GOOGLE_APPLICATION_CREDENTIALS")
	}

	computeService, err := compute.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create compute service")
	}

	userService, err := oslogin.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create oslogin service")
	}

	tmpl, err := minctlTemplate.NewTemplateBash()
	if err != nil {
		return nil, err
	}

	// Extract service account ID from email (the part before @)
	serviceAccountID := strings.Split(serviceAccountEmail, "@")[0]

	return &GCE{
		client:             computeService,
		projectID:          projectID,
		user:               userService,
		serviceAccountName: serviceAccountEmail,
		serviceAccountID:   serviceAccountID,
		zone:               zone,
		tmpl:               tmpl,
	}, nil
}

// CreateServer creates a new Minecraft server on GCE.
func (g *GCE) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	imageFamily := "ubuntu-2204-lts"

	if args.MinecraftResource.IsArm() {
		imageFamily = "ubuntu-minimal-2204-lts-arm64"
	}
	image, err := g.client.Images.GetFromFamily("ubuntu-os-cloud", imageFamily).Context(context.Background()).Do()
	if err != nil {
		return nil, err
	}

	publicKey, err := cloud.GetSSHPublicKey(args)
	if err != nil {
		return nil, err
	}

	_, err = g.user.Users.ImportSshPublicKey(fmt.Sprintf("users/%s", g.serviceAccountName), &oslogin.SshPublicKey{
		Key:                *publicKey,
		ExpirationTimeUsec: 0,
	}).Context(context.Background()).Do()
	if err != nil {
		return nil, err
	}

	stillCreating := true
	var mount string
	if args.MinecraftResource.GetVolumeSize() > 0 {
		diskInsertOp, err := g.client.Disks.Insert(g.projectID, args.MinecraftResource.GetRegion(), &compute.Disk{
			Name:   fmt.Sprintf("%s-vol", args.MinecraftResource.GetName()),
			SizeGb: int64(args.MinecraftResource.GetVolumeSize()),
			Type:   fmt.Sprintf("zones/%s/diskTypes/pd-standard", args.MinecraftResource.GetRegion()),
		}).Context(context.Background()).Do()
		if err != nil {
			return nil, err
		}

		for stillCreating {
			diskInsertOps, err := g.client.ZoneOperations.Get(g.projectID, args.MinecraftResource.GetRegion(), diskInsertOp.Name).Context(context.Background()).Do()
			if err != nil {
				return nil, err
			}
			if diskInsertOps.Status == doneStatus {
				stillCreating = false
			} else {
				time.Sleep(2 * time.Second)
			}
		}
		mount = "sdb"
	}

	userData, err := g.tmpl.GetTemplate(args.MinecraftResource, &minctlTemplate.CreateUpdateTemplateArgs{Mount: mount, Name: minctlTemplate.GetTemplateBashName(args.MinecraftResource.IsProxyServer())})
	if err != nil {
		return nil, err
	}

	scheduling := &compute.Scheduling{
		AutomaticRestart: googleapi.Bool(!args.MinecraftResource.IsSpot()),
	}
	if args.MinecraftResource.IsSpot() {
		scheduling.ProvisioningModel = "SPOT"
		scheduling.OnHostMaintenance = "TERMINATE"
	} else {
		scheduling.ProvisioningModel = "STANDARD"
		scheduling.OnHostMaintenance = "MIGRATE"
	}

	instance := &compute.Instance{
		Name:        args.MinecraftResource.GetName(),
		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", args.MinecraftResource.GetRegion(), args.MinecraftResource.GetSize()),
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       "PERSISTENT",
				DiskSizeGb: 10,
				InitializeParams: &compute.AttachedDiskInitializeParams{
					SourceImage: fmt.Sprintf("projects/ubuntu-os-cloud/global/images/%s", image.Name),
				},
			},
		},
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{
				{
					Key:   "enable-oslogin",
					Value: googleapi.String("TRUE"),
				},
				{
					Key:   "startup-script",
					Value: &userData,
				},
			},
		},
		Scheduling: scheduling,
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				AccessConfigs: []*compute.AccessConfig{
					{
						Type: "ONE_TO_ONE_NAT",
						Name: "External NAT",
					},
				},
				Network: "/global/networks/default",
			},
		},
		ServiceAccounts: []*compute.ServiceAccount{
			{
				Email: g.serviceAccountName,
				Scopes: []string{
					compute.DevstorageFullControlScope,
					compute.ComputeScope,
				},
			},
		},
		Labels: map[string]string{
			common.InstanceTag: "true",
		},
		Tags: &compute.Tags{
			Items: []string{common.InstanceTag, args.MinecraftResource.GetEdition()},
		},
	}
	if args.MinecraftResource.GetVolumeSize() > 0 {
		instance.Disks = append(instance.Disks, &compute.AttachedDisk{
			Source: fmt.Sprintf("zones/%s/disks/%s-vol", args.MinecraftResource.GetRegion(),
				args.MinecraftResource.GetName()),
		})
	}

	insertInstanceOp, err := g.client.Instances.Insert(g.projectID, args.MinecraftResource.GetRegion(), instance).Context(context.Background()).Do()
	if err != nil {
		return nil, err
	}

	stillCreating = true
	for stillCreating {
		insertInstanceOp, err := g.client.ZoneOperations.Get(g.projectID, args.MinecraftResource.GetRegion(), insertInstanceOp.Name).Context(context.Background()).Do()
		if err != nil {
			return nil, err
		}
		if insertInstanceOp.Status == doneStatus {
			stillCreating = false
		} else {
			time.Sleep(2 * time.Second)
		}
	}

	firewallRule := &compute.Firewall{
		Name:        fmt.Sprintf("%s-fw", args.MinecraftResource.GetName()),
		Description: "Firewall rule created by minectl",
		Network:     fmt.Sprintf("projects/%s/global/networks/default", g.projectID),
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "tcp",
			},
		},
		SourceRanges: []string{"0.0.0.0/0"},
		Direction:    "INGRESS",
		TargetTags:   []string{common.InstanceTag},
	}
	_, err = g.client.Firewalls.Insert(g.projectID, firewallRule).Context(context.Background()).Do()
	if err != nil {
		return nil, err
	}

	instanceListOp, err := g.client.Instances.List(g.projectID, args.MinecraftResource.GetRegion()).
		Filter(fmt.Sprintf("(name=%s)", args.MinecraftResource.GetName())).
		Context(context.Background()).
		Do()
	if err != nil {
		return nil, err
	}

	if len(instanceListOp.Items) == 1 {
		instance := instanceListOp.Items[0]
		ip := instance.NetworkInterfaces[0].AccessConfigs[0].NatIP
		return &automation.ResourceResults{
			ID:       strconv.FormatUint(instance.Id, 10),
			Name:     instance.Name,
			Region:   instance.Zone,
			PublicIP: ip,
			Tags:     strings.Join(instance.Tags.Items, ","),
		}, err
	}
	return nil, errors.New("no instances created")
}

// DeleteServer deletes a Minecraft server on GCE.
func (g *GCE) DeleteServer(id string, args automation.ServerArgs) error {
	profileGetOp, err := g.user.Users.GetLoginProfile(fmt.Sprintf("users/%s", g.serviceAccountName)).Context(context.Background()).Do()
	if err != nil {
		return err
	}
	for _, posixAccount := range profileGetOp.PosixAccounts {
		_, err := g.user.Users.Projects.Delete(posixAccount.Name).Context(context.Background()).Do()
		if err != nil {
			return err
		}
	}
	for _, publicKey := range profileGetOp.SshPublicKeys {
		_, err = g.user.Users.SshPublicKeys.Delete(publicKey.Name).Context(context.Background()).Do()
		if err != nil {
			return err
		}
	}
	instancesListOp, err := g.client.Instances.List(g.projectID, args.MinecraftResource.GetRegion()).
		Filter(fmt.Sprintf("(id=%s)", id)).
		Context(context.Background()).
		Do()
	if err != nil {
		return err
	}
	if len(instancesListOp.Items) == 1 {
		instanceDeleteOp, err := g.client.Instances.Delete(g.projectID, args.MinecraftResource.GetRegion(), instancesListOp.Items[0].Name).
			Context(context.Background()).
			Do()
		if err != nil {
			return err
		}
		stillDeleting := true
		for stillDeleting {
			instanceDeleteOp, err := g.client.ZoneOperations.Get(g.projectID, args.MinecraftResource.GetRegion(), instanceDeleteOp.Name).Context(context.Background()).Do()
			if err != nil {
				return err
			}
			if instanceDeleteOp.Status == doneStatus {
				stillDeleting = false
			} else {
				time.Sleep(2 * time.Second)
			}
		}

	}

	diskListOp, err := g.client.Disks.List(g.projectID, args.MinecraftResource.GetRegion()).
		Filter(fmt.Sprintf("(name=%s)", fmt.Sprintf("%s-vol", args.MinecraftResource.GetName()))).
		Context(context.Background()).
		Do()
	if err != nil {
		return err
	}
	for _, disk := range diskListOp.Items {
		_, err := g.client.Disks.Delete(g.projectID, args.MinecraftResource.GetRegion(), disk.Name).Context(context.Background()).Do()
		if err != nil {
			return err
		}
	}

	firewallListOps, err := g.client.Firewalls.List(g.projectID).Filter(fmt.Sprintf("(name=%s)", fmt.Sprintf("%s-fw", args.MinecraftResource.GetName()))).Context(context.Background()).Do()
	if err != nil {
		return err
	}
	for _, firewall := range firewallListOps.Items {
		_, err := g.client.Firewalls.Delete(g.projectID, firewall.Name).Context(context.Background()).Do()
		if err != nil {
			return err
		}
	}

	return nil
}

// ListServer lists all Minecraft servers on GCE.
func (g *GCE) ListServer() ([]automation.ResourceResults, error) {
	instanceListOp, err := g.client.Instances.List(g.projectID, g.zone).
		Filter(fmt.Sprintf("(labels.%s=true)", common.InstanceTag)).
		Context(context.Background()).Do()
	if err != nil {
		return nil, err
	}
	var result []automation.ResourceResults
	for _, instance := range instanceListOp.Items {
		result = append(result, automation.ResourceResults{
			ID:       strconv.FormatUint(instance.Id, 10),
			Name:     instance.Name,
			Region:   instance.Zone,
			PublicIP: instance.NetworkInterfaces[0].AccessConfigs[0].NatIP,
			Tags:     strings.Join(instance.Tags.Items, ","),
		})
	}
	return result, nil
}

func (g *GCE) getInstanceList(id, region string) ([]*compute.Instance, error) {
	instancesListOp, err := g.client.Instances.List(g.projectID, region).
		Filter(fmt.Sprintf("(id=%s)", id)).
		Context(context.Background()).
		Do()
	if err != nil {
		return nil, err
	}
	return instancesListOp.Items, nil
}

// UpdateServer updates a Minecraft server on GCE.
func (g *GCE) UpdateServer(id string, args automation.ServerArgs) error {
	instancesList, err := g.getInstanceList(id, args.MinecraftResource.GetRegion())
	if err != nil {
		return err
	}
	if len(instancesList) == 1 {
		instance := instancesList[0]
		remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, instance.NetworkInterfaces[0].AccessConfigs[0].NatIP, fmt.Sprintf("sa_%s", g.serviceAccountID))
		err = remoteCommand.UpdateServer(args.MinecraftResource)
		if err != nil {
			return err
		}
	}
	return nil
}

// UploadPlugin uploads a plugin to a Minecraft server on GCE.
func (g *GCE) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	instancesList, err := g.getInstanceList(id, args.MinecraftResource.GetRegion())
	if err != nil {
		return err
	}
	if len(instancesList) == 1 {
		instance := instancesList[0]
		remoteCommand := update.NewRemoteServer(args.SSHPrivateKeyPath, instance.NetworkInterfaces[0].AccessConfigs[0].NatIP, fmt.Sprintf("sa_%s", g.serviceAccountID))
		err = remoteCommand.TransferFile(plugin, filepath.Join(destination, filepath.Base(plugin)), args.MinecraftResource.GetSSHPort())
		if err != nil {
			return err
		}
		_, err = remoteCommand.ExecuteCommand("systemctl restart minecraft.service", args.MinecraftResource.GetSSHPort())
		if err != nil {
			return err
		}
	}
	return nil
}

// GetServer gets a Minecraft server on GCE.
func (g *GCE) GetServer(id string, args automation.ServerArgs) (*automation.ResourceResults, error) {
	instancesListOp, err := g.client.Instances.List(g.projectID, args.MinecraftResource.GetRegion()).
		Filter(fmt.Sprintf("(id=%s)", id)).
		Context(context.Background()).
		Do()
	if err != nil {
		return nil, err
	}
	if len(instancesListOp.Items) == 1 {
		instance := instancesListOp.Items[0]
		ip := instance.NetworkInterfaces[0].AccessConfigs[0].NatIP
		return &automation.ResourceResults{
			ID:       strconv.FormatUint(instance.Id, 10),
			Name:     instance.Name,
			Region:   instance.Zone,
			PublicIP: ip,
			Tags:     strings.Join(instance.Tags.Items, ","),
		}, err
	}
	return nil, nil
}
