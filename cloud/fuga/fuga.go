// Package fuga implements the Automation interface for Fuga cloud provider.
package fuga

import (
	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud/openstack"
)

// Fuga implements the Automation interface for Fuga.
type Fuga struct {
	openshift *openstack.OpenStack
}

const imageName = "Ubuntu 22.04 LTS"

// NewFuga creates a new Fuga instance.
func NewFuga() (*Fuga, error) {
	client, err := openstack.NewOpenStack(imageName)
	if err != nil {
		return nil, err
	}
	return &Fuga{
		openshift: client,
	}, nil
}

// CreateServer creates a new Minecraft server on Fuga.
func (f *Fuga) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	return f.openshift.CreateServer(args)
}

// DeleteServer deletes a Minecraft server on Fuga.
func (f *Fuga) DeleteServer(id string, args automation.ServerArgs) error {
	return f.openshift.DeleteServer(id, args)
}

// ListServer lists all Minecraft servers on Fuga.
func (f *Fuga) ListServer() ([]automation.ResourceResults, error) {
	return f.openshift.ListServer()
}

// UpdateServer updates a Minecraft server on Fuga.
func (f *Fuga) UpdateServer(id string, args automation.ServerArgs) error {
	return f.openshift.UpdateServer(id, args)
}

// UploadPlugin uploads a plugin to a Minecraft server on Fuga.
func (f *Fuga) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	return f.openshift.UploadPlugin(id, args, plugin, destination)
}

// GetServer gets a Minecraft server on Fuga.
func (f *Fuga) GetServer(id string, args automation.ServerArgs) (*automation.ResourceResults, error) {
	return f.openshift.GetServer(id, args)
}
