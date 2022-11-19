package cloud

import (
	"fmt"
	"os"
	"strings"

	"github.com/dirien/minectl-sdk/automation"
	"github.com/dirien/minectl-sdk/common"
)

// CloudProvider mapping cloud provider from short name to full name
var cloudProvider = map[string]string{
	"do":        "DigitalOcean",
	"civo":      "Civo",
	"scaleway":  "Scaleway",
	"hetzner":   "Hetzner",
	"linode":    "Linode",
	"ovh":       "OVHcloud",
	"equinix":   "Equinix Metal",
	"gce":       "Google Compute Engine",
	"vultr":     "vultr",
	"azure":     "Azure",
	"oci":       "Oracle Cloud Infrastructure",
	"ionos":     "IONOS Cloud",
	"aws":       "Amazon WebServices",
	"vexxhost":  "VEXXHOST",
	"exoscale":  "Exoscale",
	"multipass": "Ubuntu Multipass",
	"fuga":      "Fuga Cloud",
}

func GetCloudProviderFullName(cloud string) string {
	return cloudProvider[cloud]
}

func GetCloudProviderCode(fullName string) string {
	for code, name := range cloudProvider {
		if name == fullName {
			return code
		}
	}
	return ""
}

func GetSSHPublicKey(args automation.ServerArgs) (*string, error) {
	var err error
	var pubKeyFile []byte
	if !strings.HasSuffix(args.MinecraftResource.GetSSHKeyFile(), ".pub") {
		return nil, fmt.Errorf("SSH key file must have .pub extension")
	}
	if len(args.MinecraftResource.GetSSHKeyFile()) > 0 {
		pubKeyFile, err = os.ReadFile(args.MinecraftResource.GetSSHKeyFile())
		if err != nil {
			return nil, err
		}
	} else if len(args.MinecraftResource.GetSSHPublicKey()) > 0 {
		pubKeyFile = []byte(args.MinecraftResource.GetSSHPublicKey())
	}
	return common.StringPtr(string(pubKeyFile)), nil
}
