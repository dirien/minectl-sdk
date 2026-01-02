// Package vexxhost implements the Automation interface for VEXXHOST cloud provider.
package vexxhost

import (
	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud/openstack"
)

// VEXXHOST implements the Automation interface for VEXXHOST.
type VEXXHOST struct {
	openshift *openstack.OpenStack
}

const imageName = "Ubuntu 20.04.3 LTS"

// NewVEXXHOST creates a new VEXXHOST instance.
func NewVEXXHOST() (*VEXXHOST, error) {
	client, err := openstack.NewOpenStack(imageName)
	if err != nil {
		return nil, err
	}
	return &VEXXHOST{
		openshift: client,
	}, nil
}

// CreateServer creates a new Minecraft server on VEXXHOST.
func (v *VEXXHOST) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	return v.openshift.CreateServer(args)
}

// DeleteServer deletes a Minecraft server on VEXXHOST.
func (v *VEXXHOST) DeleteServer(id string, args automation.ServerArgs) error {
	return v.openshift.DeleteServer(id, args)
}

// ListServer lists all Minecraft servers on VEXXHOST.
func (v *VEXXHOST) ListServer() ([]automation.ResourceResults, error) {
	return v.openshift.ListServer()
}

// UpdateServer updates a Minecraft server on VEXXHOST.
func (v *VEXXHOST) UpdateServer(id string, args automation.ServerArgs) error {
	return v.openshift.UpdateServer(id, args)
}

// UploadPlugin uploads a plugin to a Minecraft server on VEXXHOST.
func (v *VEXXHOST) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	return v.openshift.UploadPlugin(id, args, plugin, destination)
}

// GetServer gets a Minecraft server on VEXXHOST.
func (v *VEXXHOST) GetServer(id string, args automation.ServerArgs) (*automation.ResourceResults, error) {
	return v.openshift.GetServer(id, args)
}
