// Package update provides remote server operations via SSH.
package update

import (
	"fmt"
	"strings"

	minctlTemplate "github.com/dirien/minectl-sdk/template"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"

	"github.com/dirien/minectl-sdk/model"
	"github.com/melbahja/goph"
)

// ServerOperations defines remote server operations.
type ServerOperations interface {
	UpdateServer(*model.MinecraftResource) error
}

// RemoteServer represents a remote server connection.
type RemoteServer struct {
	ip            string
	privateSSHKey string
	user          string
}

// NewRemoteServer creates a new RemoteServer instance.
func NewRemoteServer(privateKey, ip, user string) *RemoteServer {
	ssh := &RemoteServer{
		ip:            ip,
		privateSSHKey: privateKey,
		user:          user,
	}
	return ssh
}

// UpdateServer updates the Minecraft server software.
func (r *RemoteServer) UpdateServer(args *model.MinecraftResource) error {
	tmpl := minctlTemplate.GetUpdateTemplate()
	var update string
	var err error

	switch args.GetEdition() {
	case "java":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateJavaBinary})
	case "bedrock":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateBedrockBinary})
	case "craftbukkit":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateSpigotBukkitBinary})
	case "spigot":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateSpigotBukkitBinary})
	case "fabric":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateFabricBinary})
		update = fmt.Sprintf("\nrm -rf /minecraft/minecraft-server.jar%s", update)
	case "forge":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateForgeBinary})
	case "papermc":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplatePaperMCBinary})
	case "purpur":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplatePurpurBinary})
	case "bungeecord":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateBungeeCordBinary})
	case "waterfall":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateWaterfallBinary})
	case "nukkit":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateNukkitBinary})
	case "powernukkit":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplatePowerNukkitBinary})
	case "velocity":
		update, err = tmpl.DoUpdate(args, &minctlTemplate.CreateUpdateTemplateArgs{Name: minctlTemplate.TemplateVelocityBinary})
	}

	if args.GetEdition() != "bedrock" {
		update = fmt.Sprintf("%s\napt-get install -y openjdk-%d-jre-headless\n", update, args.GetJDKVersion())
	}
	if err != nil {
		return err
	}

	cmd := `
cd /minecraft
sudo systemctl stop minecraft.service
sudo bash -c '` + update + `'
ls -la
sudo systemctl start minecraft.service
	`
	zap.S().Infof("server updated cmd %s", cmd)
	_, err = r.ExecuteCommand(strings.TrimSpace(cmd), args.GetSSHPort())
	if err != nil {
		return err
	}
	return nil
}

// TransferFile uploads a file to the remote server.
func (r *RemoteServer) TransferFile(src, dstPath string, port int) error {
	auth, err := goph.Key(r.privateSSHKey, "")
	if err != nil {
		return err
	}

	client, err := goph.NewConn(&goph.Config{
		User:     r.user,
		Addr:     r.ip,
		Port:     uint(port), //nolint:gosec // port is validated
		Auth:     auth,
		Callback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	})
	if err != nil {
		return err
	}

	defer func() { _ = client.Close() }()
	err = client.Upload(src, dstPath)
	if err != nil {
		return err
	}
	return nil
}

// ExecuteCommand runs a command on the remote server.
func (r *RemoteServer) ExecuteCommand(cmd string, port int) (string, error) {
	auth, err := goph.Key(r.privateSSHKey, "")
	if err != nil {
		return "", err
	}
	client, err := goph.NewConn(&goph.Config{
		User:     r.user,
		Addr:     r.ip,
		Port:     uint(port), //nolint:gosec // port is validated
		Auth:     auth,
		Callback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	})
	if err != nil {
		return "", err
	}

	defer func() { _ = client.Close() }()
	out, err := client.Run(cmd)
	return string(out), err
}
