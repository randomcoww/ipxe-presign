package config

import (
	"slices"
	"strings"
)

type Profiles struct {
	BaseProfile *Profile
	Profiles    map[string]*Profile
}

// LoadConfig reads and validates the boot profile config file.
func NewProfileConfig(raw *YamlConfig) (*Profiles, error) {
	profiles := &Profiles{
		BaseProfile: &Profile{},
		Profiles:    make(map[string]*Profile),
	}

	profiles.BaseProfile.mergeOverlay(raw.BaseProfile)
	for _, p := range raw.OverlayProfiles {
		for _, mac := range p.Selector {
			mac = strings.ToLower(mac)
			if _, ok := profiles.Profiles[mac]; !ok {
				profiles.Profiles[mac] = &Profile{}
				profiles.Profiles[mac].mergeOverlay(profiles.BaseProfile)
			}
			profiles.Profiles[mac].mergeOverlay(p)
		}
	}
	return profiles, nil
}

func (p *Profile) mergeOverlay(overlay *Profile) {
	if overlay == nil {
		return
	}
	if overlay.KernelResource != "" {
		p.KernelResource = overlay.KernelResource
	}
	if overlay.IgnitionResource != "" {
		p.IgnitionResource = overlay.IgnitionResource
	}
	if overlay.RootfsResource != "" {
		p.RootfsResource = overlay.RootfsResource
	}
	p.InitrdResources = append(p.InitrdResources, overlay.InitrdResources...)
	p.Kargs = append(p.Kargs, overlay.Kargs...)
	slices.Sort(p.Kargs)
	p.Kargs = slices.Compact(p.Kargs)
}
