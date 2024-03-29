package vexxhost

import (
	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/cloud/openstack"
)

type VEXXHOST struct {
	openshift *openstack.OpenStack
}

const imageName = "Ubuntu 20.04.3 LTS"

func NewVEXXHOST() (*VEXXHOST, error) {
	client, err := openstack.NewOpenStack(imageName)
	if err != nil {
		return nil, err
	}
	return &VEXXHOST{
		openshift: client,
	}, nil
}

func (v *VEXXHOST) CreateServer(args automation.ServerArgs) (*automation.ResourceResults, error) {
	return v.openshift.CreateServer(args)
}

func (v *VEXXHOST) DeleteServer(id string, args automation.ServerArgs) error {
	return v.openshift.DeleteServer(id, args)
}

func (v *VEXXHOST) ListServer() ([]automation.ResourceResults, error) {
	return v.openshift.ListServer()
}

func (v *VEXXHOST) UpdateServer(id string, args automation.ServerArgs) error {
	return v.openshift.UpdateServer(id, args)
}

func (v *VEXXHOST) UploadPlugin(id string, args automation.ServerArgs, plugin, destination string) error {
	return v.openshift.UploadPlugin(id, args, plugin, destination)
}

func (v *VEXXHOST) GetServer(id string, args automation.ServerArgs) (*automation.ResourceResults, error) {
	return v.openshift.GetServer(id, args)
}
