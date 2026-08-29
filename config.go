package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile describes how a node boots. A profile is selected when the
// client certificate's commonName equals the profile's map key.
type Profile struct {
	KernelURL          string   `yaml:"kernel_url"`
	InitrdURLs         []string `yaml:"initrd_urls"`
	IgnitionS3Resource string   `yaml:"ignition_s3_resource"`
	RootfsS3Resource   string   `yaml:"rootfs_s3_resource"`
	Kargs              []string `yaml:"kargs"`
}

// fileConfig is the on-disk shape of the YAML config.
type fileConfig struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

// LoadConfig reads and validates the boot profile config file.
// The returned map is keyed by profile name (client CN).
func LoadConfig(path string) (map[string]Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg fileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if len(cfg.Profiles) == 0 {
		return nil, fmt.Errorf("config %s defines no profiles", path)
	}

	profiles := make(map[string]Profile, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("config defines a profile with an empty name")
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		profiles[name] = p
	}
	return profiles, nil
}

// Validate checks that a profile has everything needed to render a boot script.
func (p Profile) Validate() error {
	var problems []string
	if p.KernelURL == "" {
		problems = append(problems, "kernel_url is required")
	}
	if len(p.InitrdURLs) == 0 {
		problems = append(problems, "initrd_urls must contain at least one URL")
	}
	if p.IgnitionS3Resource == "" {
		problems = append(problems, "ignition_s3_resource is required")
	}
	if p.RootfsS3Resource == "" {
		problems = append(problems, "rootfs_s3_resource is required")
	}
	for i, k := range p.Kargs {
		if strings.TrimSpace(k) == "" {
			problems = append(problems, fmt.Sprintf("kargs[%d] is empty", i))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, ", "))
	}
	return nil
}
