package config

import (
	"slices"
)

type Profiles struct {
	Profiles       []*Profile
	selectorRevMap map[*Profile]map[string]map[string]struct{}
}

func NewProfileConfig(raw *YamlConfig) *Profiles {
	profiles := &Profiles{
		Profiles:       raw.Profiles,
		selectorRevMap: make(map[*Profile]map[string]map[string]struct{}),
	}
	for _, p := range profiles.Profiles {
		if _, ok := profiles.selectorRevMap[p]; !ok {
			profiles.selectorRevMap[p] = make(map[string]map[string]struct{})
		}
		for t, values := range p.Selector {
			if _, ok := profiles.selectorRevMap[p][t]; !ok {
				profiles.selectorRevMap[p][t] = make(map[string]struct{})
			}
			for _, k := range values {
				if _, ok := profiles.selectorRevMap[p][t][k]; !ok {
					profiles.selectorRevMap[p][t][k] = struct{}{}
				}
			}
		}
	}
	return profiles
}

func (profiles *Profiles) GetMerged(selectors map[string]string) (*Profile, bool) {
	res := &Profile{}
	ok := false

	for _, p := range profiles.Profiles {
		for k, v := range selectors {
			// reject if type (e.g. mac:hexhyp) exists but doesn't contain our selector value
			// this matches profiles with a blank selector
			if _, ok := profiles.selectorRevMap[p][k]; ok {
				if _, ok := profiles.selectorRevMap[p][k][v]; !ok {
					goto nextProfile
				}
			}
		}
		res.mergeOverlay(p)
		ok = true

	nextProfile:
	}
	return res, ok
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
