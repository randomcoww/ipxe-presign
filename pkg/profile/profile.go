package profile

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	GlobalProfile   *Profile   `yaml:"globalProfile"`
	OverlayProfiles []*Profile `yaml:"overlayProfiles"`
}

type Config struct {
	GlobalProfile *Profile
	Profiles      map[string]*Profile
}

// Profile describes how a node boots. Every resource is an object key
// inside the configured S3 bucket and is handed to the node as a
// short-lived pre-signed URL.
type Profile struct {
	Selector         []string `yaml:"selector,omitempty"`
	Kernel           string   `yaml:"kernel,omitempty"`
	Initrds          []string `yaml:"initrds,omitempty"`
	Ignition         string   `yaml:"ignition,omitempty"`
	Rootfs           string   `yaml:"rootfs,omitempty"`
	Kargs            []string `yaml:"kargs,omitempty"`
	BootIPXETemplate string   `yaml:"bootIPXETemplate,omitempty"`
}

// LoadConfig reads and validates the boot profile config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var raw yamlConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &Config{
		GlobalProfile: &Profile{

			// Second-stage boot script. The presigned kernel,
			// initrd, ignition and rootfs URLs are substituted in; ignition and
			// rootfs are passed as kernel arguments.
			BootIPXETemplate: `#!ipxe
kernel {{.Kernel}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.Ignition}} coreos.live.rootfs_url={{.Rootfs}}
initrd {{range .Initrds}} {{.}}{{end}}
boot
`,
		},
		Profiles: make(map[string]*Profile),
	}
	cfg.GlobalProfile.mergeOverlay(raw.GlobalProfile)
	for _, p := range raw.OverlayProfiles {
		for _, mac := range p.Selector {
			mac = strings.ToLower(mac)
			if _, ok := cfg.Profiles[mac]; !ok {
				cfg.Profiles[mac] = &Profile{}
				cfg.Profiles[mac].mergeOverlay(raw.GlobalProfile)
			}
			cfg.Profiles[mac].mergeOverlay(p)
		}
	}
	return cfg, nil
}

func (p *Profile) mergeOverlay(overlay *Profile) {
	if overlay.Kernel != "" {
		p.Kernel = overlay.Kernel
	}
	if overlay.Ignition != "" {
		p.Ignition = overlay.Ignition
	}
	if overlay.Rootfs != "" {
		p.Rootfs = overlay.Rootfs
	}
	if overlay.BootIPXETemplate != "" {
		p.BootIPXETemplate = overlay.BootIPXETemplate
	}
	p.Initrds = append(p.Initrds, overlay.Initrds...)
	p.Kargs = append(p.Kargs, overlay.Kargs...)
	slices.Sort(p.Kargs)
	p.Kargs = slices.Compact(p.Kargs)
}
