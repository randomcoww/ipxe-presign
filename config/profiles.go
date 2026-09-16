package config

import (
	"slices"
)

type Profiles struct {
	Profiles       []*Profile
	selectorMap    map[string]map[string][]*Profile
	selectorRevMap map[*Profile]map[string]map[string]struct{}
}

func NewProfileConfig(raw *YamlConfig) (*Profiles, error) {
	profiles := &Profiles{
		Profiles:       raw.Profiles,
		selectorMap:    make(map[string]map[string][]*Profile),
		selectorRevMap: make(map[*Profile]map[string]map[string]struct{}),
	}

	for _, p := range profiles.Profiles {
		if _, ok := profiles.selectorRevMap[p]; !ok {
			profiles.selectorRevMap[p] = make(map[string]map[string]struct{})
		}
		for t, values := range p.Selector {
			if _, ok := profiles.selectorMap[t]; !ok {
				profiles.selectorMap[t] = make(map[string][]*Profile)
			}
			if _, ok := profiles.selectorRevMap[p][t]; !ok {
				profiles.selectorRevMap[p][t] = make(map[string]struct{})
			}
			for _, k := range values {
				if _, ok := profiles.selectorMap[t][k]; !ok {
					profiles.selectorMap[t][k] = []*Profile{}
				}
				profiles.selectorMap[t][k] = append(profiles.selectorMap[t][k], p)
				profiles.selectorRevMap[p][t][k] = struct{}{}
			}
		}
	}
	return profiles, nil
}

func (profiles *Profiles) GetMerged(selectors map[string]string) *Profile {
	res := &Profile{}

	for _, p := range profiles.Profiles {
		for k, v := range selectors {
			if _, ok := profiles.selectorRevMap[p][k][v]; !ok {
				goto nextProfile
			}
		}
		// AND match eveyrthing and merge into one profile
		res.mergeOverlay(p)

	nextProfile:
	}
	return res
}

func (p *Profile) mergeOverlay(overlay *Profile) {
	if overlay == nil {
		return
	}
	if overlay.KernelURL != "" {
		p.KernelURL = overlay.KernelURL
	}
	p.InitrdURLs = append(p.InitrdURLs, overlay.InitrdURLs...)
	p.Kargs = append(p.Kargs, overlay.Kargs...)
	slices.Sort(p.Kargs)
	p.Kargs = slices.Compact(p.Kargs)
}
