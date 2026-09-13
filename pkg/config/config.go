package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Profile describes how a node boots. Every resource is an object key
// inside the configured S3 bucket and is handed to the node as a
// short-lived pre-signed URL.
type Profile struct {
	Selector           []string `yaml:"selector,omitempty"`
	KernelS3Resource   string   `yaml:"kernel_s3_resource,omitempty"`
	InitrdS3Resources  []string `yaml:"initrd_s3_resources,omitempty"`
	IgnitionS3Resource string   `yaml:"ignition_s3_resource,omitempty"`
	RootfsS3Resource   string   `yaml:"rootfs_s3_resource,omitempty"`
	Kargs              []string `yaml:"kargs,omitempty"`
}

// fileConfig is the on-disk shape of the YAML config.
type fileConfig struct {
	Global   *Profile   `yaml:"global"`
	Overlays []*Profile `yaml:"overlays"`
}

type Config struct {
	Profiles map[string]*Profile
}

// LoadConfig reads and validates the boot profile config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var raw fileConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &Config{
		Profiles: make(map[string]*Profile),
	}
	for _, p := range raw.Overlays {
		for _, mac := range p.Selector {
			if _, ok := cfg.Profiles[mac]; !ok {
				cfg.Profiles[mac] = &Profile{}
				cfg.Profiles[mac].mergeOverlay(raw.Global)
			}
			cfg.Profiles[mac].mergeOverlay(p)
		}
	}
	return cfg, nil
}

func (p *Profile) mergeOverlay(overlay *Profile) {
	if overlay.KernelS3Resource != "" {
		p.KernelS3Resource = overlay.KernelS3Resource
	}
	p.InitrdS3Resources = append(p.InitrdS3Resources, overlay.InitrdS3Resources...)
	if overlay.IgnitionS3Resource != "" {
		p.IgnitionS3Resource = overlay.IgnitionS3Resource
	}
	if overlay.RootfsS3Resource != "" {
		p.RootfsS3Resource = overlay.RootfsS3Resource
	}
	p.Kargs = append(p.Kargs, overlay.Kargs...)
}
