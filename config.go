package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

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
}

// fileConfig is the on-disk shape of the YAML config.
type fileConfig struct {
	Profiles map[string]Profile  `yaml:"profiles"`
	Groups   map[string][]string `yaml:"groups"`
}

// Config is the validated, ready-to-serve form of the profile config.
type Config struct {
	// Profiles keyed by profile name.
	Profiles map[string]Profile
	// MACToProfile maps a normalized MAC address (aa:bb:cc:dd:ee:ff)
	// to the profile name its group resolves to.
	MACToProfile map[string]string
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
	if len(raw.Profiles) == 0 {
		return nil, fmt.Errorf("config %s defines no profiles", path)
	}

	cfg := &Config{
		Profiles:     make(map[string]Profile, len(raw.Profiles)),
		MACToProfile: make(map[string]string),
	}
	for name, p := range raw.Profiles {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("config defines a profile with an empty name")
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		cfg.Profiles[name] = p
	}

	for groupName, macs := range raw.Groups {
		if _, ok := cfg.Profiles[groupName]; !ok {
			return nil, fmt.Errorf("group %q references unknown profile %q", groupName, groupName)
		}
		for _, mac := range macs {
			norm, err := NormalizeMAC(mac)
			if err != nil {
				return nil, fmt.Errorf("group %q: invalid MAC address %q: %w", groupName, mac, err)
			}
			if existing, dup := cfg.MACToProfile[norm]; dup {
				return nil, fmt.Errorf("MAC %s is assigned to multiple profiles (%q and %q)", norm, existing, groupName)
			}
			cfg.MACToProfile[norm] = groupName
		}
	}
	return cfg, nil
}

// Validate checks that a profile has everything needed to render a boot script.
func (p Profile) Validate() error {
	var problems []string
	if p.KernelS3Resource == "" {
		problems = append(problems, "kernel_s3_resource is required")
	}
	if len(p.InitrdS3Resources) == 0 {
		problems = append(problems, "initrd_s3_resources must contain at least one resource")
	}
	for i, r := range p.InitrdS3Resources {
		if strings.TrimSpace(r) == "" {
			problems = append(problems, fmt.Sprintf("initrd_s3_resources[%d] is empty", i))
		}
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

// NormalizeMAC lowercases and strips separators, then returns the
// canonical aa:bb:cc:dd:ee:ff form. It accepts the colon, dash and dot
// separated forms as well as bare 12-hex-digit addresses, so MACs in
// config and the ${mac:hexhyp} value iPXE sends are interchangeable.
func NormalizeMAC(s string) (string, error) {
	var hex bytes.Buffer
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20 // lowercase ASCII
		if isHex(c) {
			hex.WriteByte(c)
			continue
		}
		switch c {
		case ':', '-', '.', ' ', '	':
			// separators and whitespace are ignored
		default:
			return "", fmt.Errorf("unsupported character %q", s[i])
		}
	}
	if hex.Len() != 12 {
		return "", fmt.Errorf("expected 12 hex digits, got %d", hex.Len())
	}
	b := hex.String()
	return b[0:2] + ":" + b[2:4] + ":" + b[4:6] + ":" + b[6:8] + ":" + b[8:10] + ":" + b[10:12], nil
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
