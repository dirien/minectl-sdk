package template

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/dirien/minectl-sdk/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

// loadGolden loads expected output from a golden file in testdata/
func loadGolden(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file: %s", path)
	return string(data)
}

// updateGolden writes the actual output to a golden file (only when -update flag is set)
func updateGolden(t *testing.T, name string, actual string) {
	t.Helper()
	if !*update {
		return
	}
	path := filepath.Join("testdata", name+".golden")
	err := os.WriteFile(path, []byte(actual), 0644)
	require.NoError(t, err, "failed to update golden file: %s", path)
}

// assertGolden compares actual output against golden file, updating if -update flag is set
func assertGolden(t *testing.T, name string, actual string) {
	t.Helper()
	updateGolden(t, name, actual)
	expected := loadGolden(t, name)
	assert.Equal(t, expected, actual)
}

// Test fixture helpers

func makeBaseResource(edition string, port int, monitoring bool) model.MinecraftResource {
	return model.MinecraftResource{
		Spec: model.Spec{
			Server: model.Server{
				Port: port,
			},
			Minecraft: model.Minecraft{
				Edition:    edition,
				Properties: "level-seed=minectlrocks\nview-distance=10\nenable-jmx-monitoring=false\n",
				Eula:       true,
			},
			Monitoring: model.Monitoring{
				Enabled: monitoring,
			},
		},
	}
}

func makeJavaResource(edition, version string, openJDK int, monitoring bool) model.MinecraftResource {
	r := makeBaseResource(edition, 25565, monitoring)
	r.Spec.Minecraft.Version = version
	r.Spec.Minecraft.Java = model.Java{
		OpenJDK: openJDK,
		Xms:     "2G",
		Xmx:     "2G",
		Rcon: model.Rcon{
			Port:      2,
			Password:  "test",
			Enabled:   true,
			Broadcast: true,
		},
	}
	return r
}

func makeBedrockResource(version string, monitoring bool) model.MinecraftResource {
	r := makeBaseResource("bedrock", 19132, monitoring)
	r.Spec.Minecraft.Version = version
	r.Spec.Minecraft.Eula = false
	return r
}

// Pre-defined test fixtures
var (
	bedrock = makeBedrockResource("1.17.10.04", true)

	bedrockNoMon = makeBedrockResource("1.17.10.04", false)

	java = makeJavaResource("java", "1.17", 16, true)

	javaAdditionalOptions = func() model.MinecraftResource {
		r := makeJavaResource("java", "1.17", 16, true)
		r.Spec.Minecraft.Java.Options = []string{
			"-XX:+UseG1GC",
			"-XX:+ParallelRefProcEnabled",
		}
		return r
	}()

	papermc = makeJavaResource("papermc", "1.17.1-157", 16, true)

	craftbukkit = makeJavaResource("craftbukkit", "1.17.1-138", 16, true)

	fabric = makeJavaResource("fabric", "1.17.1-138", 16, true)

	fabricNoMon = makeJavaResource("fabric", "1.17.1-138", 16, false)

	forge = makeJavaResource("forge", "1.17.1-138", 16, true)

	spigot = makeJavaResource("spigot", "1.17.1-138", 16, true)

	nukkit = func() model.MinecraftResource {
		r := makeJavaResource("nukkit", "1.0-SNAPSHOT", 8, false)
		r.Spec.Server.Port = 19132
		return r
	}()

	powerNukkit = func() model.MinecraftResource {
		r := makeJavaResource("powernukkit", "1.5.1.0-PN", 8, false)
		r.Spec.Server.Port = 19132
		return r
	}()

	purpur = makeJavaResource("purpur", "1.19", 16, false)
)

// Table-driven tests for template generation
func TestBashTemplates(t *testing.T) {
	tests := []struct {
		name       string
		resource   *model.MinecraftResource
		mount      string
		goldenFile string
	}{
		{"BedrockBash", &bedrock, "", "bedrock_bash_want"},
		{"BedrockBashNoMon", &bedrockNoMon, "", "bedrock_bash_no_mon_want"},
		{"JavaBash", &java, "", "java_bash_want"},
		{"BedrockBashMount", &bedrock, "sdc", "bedrock_bash_mount_want"},
		{"JavaBashMount", &java, "sdc", "java_bash_mount_want"},
		{"JavaAdditionalOptionsMount", &javaAdditionalOptions, "sdc", "java_bash_additional_options_mount_want"},
		{"PaperMCBash", &papermc, "sdc", "paper_m_c_bash_mount_want"},
		{"CraftBukkitBash", &craftbukkit, "sda", "craftbukkit_bash_want"},
		{"FabricBash", &fabric, "sda", "fabric_bash_want"},
		{"FabricBashNoMon", &fabricNoMon, "sda", "fabric_bash_no_mon_want"},
		{"ForgeBash", &forge, "sda", "forge_bash_want"},
		{"SpigotBash", &spigot, "sda", "spigot_bash_want"},
		{"NukkitBash", &nukkit, "sda", "nukkit_bash_want"},
		{"PowerNukkitBash", &powerNukkit, "sda", "power_nukkit_bash_want"},
		{"PurpurBash", &purpur, "sda", "purpur_bash_want"},
	}

	tmpl, err := NewTemplateBash()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tmpl.GetTemplate(tt.resource, &CreateUpdateTemplateArgs{
				Mount: tt.mount,
				Name:  TemplateBash,
			})
			require.NoError(t, err)
			assertGolden(t, tt.goldenFile, got)
		})
	}
}

func TestCloudInitTemplates(t *testing.T) {
	tests := []struct {
		name       string
		resource   *model.MinecraftResource
		mount      string
		goldenFile string
	}{
		{"BedrockCloudInit", &bedrock, "sda", "bedrock_cloud_init_want"},
		{"JavaCloudInit", &java, "sda", "java_cloud_init_want"},
		{"PaperMCCloudInit", &papermc, "sda", "paper_m_c_cloud_init_want"},
		{"CraftBukkitCloudInit", &craftbukkit, "sda", "craftbukkit_cloud_init_want"},
		{"FabricCloudInit", &fabric, "sda", "fabric_cloud_init_want"},
		{"FabricCloudInitNoMon", &fabricNoMon, "sda", "fabric_cloud_init_no_mon_want"},
		{"ForgeCloudInit", &forge, "sda", "forge_cloud_init_want"},
		{"SpigotCloudInit", &spigot, "sda", "spigot_cloud_init_want"},
		{"NukkitCloudInit", &nukkit, "sda", "nukkit_cloud_init_want"},
		{"PowerNukkitCloudInit", &powerNukkit, "sda", "power_nukkit_cloud_init_want"},
		{"PurpurCloudInit", &purpur, "sda", "purpur_cloud_init_want"},
	}

	tmpl, err := NewTemplateCloudConfig()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tmpl.GetTemplate(tt.resource, &CreateUpdateTemplateArgs{
				Mount: tt.mount,
				Name:  TemplateCloudConfig,
			})
			require.NoError(t, err)
			assertGolden(t, tt.goldenFile, got)
		})
	}
}

// Helper for config template tests
func makeWizardMock() model.Wizard {
	return model.Wizard{
		Name:       "minecraft",
		Provider:   "DigitalOcean",
		Plan:       "xxx",
		Region:     "xxx",
		SSH:        "xxx",
		Features:   []string{"Monitoring", "RCON"},
		Java:       "8",
		Heap:       "2G",
		RconPw:     "xxx",
		Edition:    "java",
		Version:    "xx",
		Properties: "level-seed=minectlrocks\nview-distance=10\nenable-jmx-monitoring=false",
	}
}

func TestConfigTemplates(t *testing.T) {
	tests := []struct {
		name       string
		wizardMod  func(w model.Wizard) model.Wizard
		goldenFile string
	}{
		{
			name:       "FullFeatureJava",
			wizardMod:  func(w model.Wizard) model.Wizard { return w },
			goldenFile: "full_feature_java",
		},
		{
			name: "JavaWithoutRcon",
			wizardMod: func(w model.Wizard) model.Wizard {
				w.Features = []string{"Monitoring"}
				return w
			},
			goldenFile: "java_without_rcon",
		},
		{
			name: "PlainJavaNoThrill",
			wizardMod: func(w model.Wizard) model.Wizard {
				w.Features = []string{}
				return w
			},
			goldenFile: "plain_java_no_thrill",
		},
		{
			name: "BedrockConfig",
			wizardMod: func(w model.Wizard) model.Wizard {
				w.Edition = "bedrock"
				return w
			},
			goldenFile: "bedrock_config",
		},
		{
			name: "NukkitConfig",
			wizardMod: func(w model.Wizard) model.Wizard {
				w.Edition = "nukkit"
				return w
			},
			goldenFile: "nukkit_config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wizard := tt.wizardMod(makeWizardMock())
			got, err := NewTemplateConfig(wizard)
			require.NoError(t, err)
			assertGolden(t, tt.goldenFile, got)
		})
	}
}
