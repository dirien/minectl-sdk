// Package automation provides interfaces for cloud automation operations.
package automation

import "github.com/dirien/minectl-sdk/model"

// Automation defines the interface for cloud provider operations.
type Automation interface {
	CreateServer(args ServerArgs) (*ResourceResults, error)
	DeleteServer(id string, args ServerArgs) error
	ListServer() ([]ResourceResults, error)
	UpdateServer(id string, args ServerArgs) error
	UploadPlugin(id string, args ServerArgs, plugin, destination string) error
	GetServer(id string, args ServerArgs) (*ResourceResults, error)
}

// Rcon represents RCON configuration for server management.
type Rcon struct {
	Password  string
	Enabled   bool
	Port      int
	Broadcast bool
}

// ServerArgs contains arguments for server operations.
type ServerArgs struct {
	ID                string
	MinecraftResource *model.MinecraftResource
	SSHPrivateKeyPath string
}

// ResourceResults contains the results of a server operation.
type ResourceResults struct {
	ID       string
	Name     string
	Region   string
	PublicIP string
	Tags     string
}
