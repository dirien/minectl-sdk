// Package template provides templates for cloud-init and bash scripts.
package template

import (
	"bytes"
	"embed"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/dirien/minectl-sdk/cloud"
	"github.com/dirien/minectl-sdk/model"
)

// Template wraps a text/template for generating scripts.
type Template struct {
	Template *template.Template
	Values   *templateValues
}

type templateValues struct {
	*model.MinecraftResource
	Mount        string
	SSHPublicKey string
	Properties   []string
}

// Name represents the name of a template.
type Name string

// Template name constants for different server editions.
const (
	TemplateBash               Name = "bash"
	TemplateCloudConfig        Name = "cloud-config"
	TemplateJavaBinary         Name = "java-binary"
	TemplateBedrockBinary      Name = "bedrock-binary"
	TemplateSpigotBukkitBinary Name = "spigotbukkit-binary"
	TemplateFabricBinary       Name = "fabric-binary"
	TemplateForgeBinary        Name = "forge-binary"
	TemplatePaperMCBinary      Name = "papermc-binary"
	TemplatePurpurBinary       Name = "purpur-binary"
	TemplateProxyCloudConfig   Name = "proxy-cloud-config"
	TemplateProxyBash          Name = "proxy-bash"
	TemplateBungeeCordBinary   Name = "bungeecord-binary"
	TemplateWaterfallBinary    Name = "waterfall-binary"
	TemplateVelocityBinary     Name = "velocity-binary"
	TemplateNukkitBinary       Name = "nukkit-binary"
	TemplatePowerNukkitBinary  Name = "powernukkit-binary"
)

// GetUpdateTemplate returns a template for update operations.
func GetUpdateTemplate() *Template {
	bash := template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseFS(templateFS, "templates/bash/*"))
	return &Template{
		Template: bash,
		Values:   &templateValues{},
	}
}

// DoUpdate executes the update template.
func (t *Template) DoUpdate(model *model.MinecraftResource, args *CreateUpdateTemplateArgs) (string, error) {
	return t.GetTemplate(model, args)
}

// CreateUpdateTemplateArgs contains arguments for template execution.
type CreateUpdateTemplateArgs struct {
	Mount        string
	SSHPublicKey string
	Name         Name
}

// GetTemplate executes the named template with the given model.
func (t *Template) GetTemplate(model *model.MinecraftResource, args *CreateUpdateTemplateArgs) (string, error) {
	var buff bytes.Buffer

	t.Values.MinecraftResource = model
	t.Values.Properties = strings.Split(model.GetProperties(), "\n")

	t.Values.Mount = args.Mount
	t.Values.SSHPublicKey = args.SSHPublicKey

	err := t.Template.ExecuteTemplate(&buff, string(args.Name), t.Values)
	if err != nil {
		return "", err
	}
	return buff.String(), nil
}

// GetTemplateCloudConfigName returns the appropriate cloud-config template name.
func GetTemplateCloudConfigName(isProxy bool) Name {
	if isProxy {
		return TemplateProxyCloudConfig
	}
	return TemplateCloudConfig
}

// GetTemplateBashName returns the appropriate bash template name.
func GetTemplateBashName(isProxy bool) Name {
	if isProxy {
		return TemplateProxyBash
	}
	return TemplateBash
}

//go:embed templates
var templateFS embed.FS

// NewTemplateBash creates a new bash template.
func NewTemplateBash() (*Template, error) {
	bash := template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseFS(templateFS, "templates/bash/*"))
	return &Template{
		Template: bash,
		Values:   &templateValues{},
	}, nil
}

// NewTemplateCloudConfig creates a new cloud-config template.
func NewTemplateCloudConfig() (*Template, error) {
	cloudInit := template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseFS(templateFS, "templates/cloud-init/*"))
	return &Template{
		Template: cloudInit,
		Values:   &templateValues{},
	}, nil
}

// NewTemplateConfig generates a configuration file from a wizard.
func NewTemplateConfig(value model.Wizard) (string, error) {
	var buff bytes.Buffer
	value.Provider = cloud.GetCloudProviderCode(value.Provider)
	config := template.Must(template.New("config").Funcs(sprig.TxtFuncMap()).ParseFS(templateFS, "templates/config/*"))
	err := config.ExecuteTemplate(&buff, "config", value)
	if err != nil {
		return "", err
	}
	return buff.String(), nil
}
