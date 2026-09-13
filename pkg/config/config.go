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
	KernelS3Resource   string   `yaml:"kernel_s3_resource"`
	InitrdS3Resources  []string `yaml:"initrd_s3_resources"`
	IgnitionS3Resource string   `yaml:"ignition_s3_resource"`
	RootfsS3Resource   string   `yaml:"rootfs_s3_resource"`
	Kargs              []string `yaml:"kargs"`
	Macs               []string `yaml:"macs"`
}

// fileConfig is the on-disk shape of the YAML config.
type fileConfig struct {
	Profiles []*Profile `yaml:"profiles"`
}

type Config struct {
	ProfileByMac map[string]*Profile
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
		ProfileByMac: make(map[string]*Profile),
	}
	for _, p := range raw.Profiles {
		for _, mac := range p.Macs {
			cfg.ProfileByMac[mac] = p
		}
	}
	return cfg, nil
}
