package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProfileConfig(t *testing.T) {
	yamlConfig := &YamlConfig{
		Profiles: []*Profile{
			{
				Selector: map[string][]string{
					"mac:hexhyp": []string{
						"aa-bb-cc-dd-ee-01",
						"aa-bb-cc-dd-ee-02",
					},
					"buildarch:uristring": []string{
						"x86_64",
					},
				},
				Kargs: []string{
					"console=tty0",
				},
			},
			{
				Selector: map[string][]string{
					"mac:hexhyp": []string{
						"aa-bb-cc-dd-ee-01",
					},
				},
				Kargs: []string{
					"console=ttyS0,115200n8",
				},
			},
			{
				Selector: map[string][]string{
					"mac:hexhyp": []string{
						"aa-bb-cc-dd-ee-01",
					},
					"buildarch:uristring": []string{
						"x86_64",
					},
				},
				Kargs: []string{
					"console=ttyS0",
				},
			},
			{
				Selector: map[string][]string{
					"mac:hexhyp": []string{
						"aa-bb-cc-dd-ee-02",
					},
				},
				InitrdURLs: []string{
					"fcos/initramfs-2.img",
				},
				Kargs: []string{
					"console=tty0",
				},
			},
		},
	}

	expectedSelectorMap := map[string]map[string][]*Profile{
		"mac:hexhyp": map[string][]*Profile{
			"aa-bb-cc-dd-ee-01": []*Profile{
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-01",
							"aa-bb-cc-dd-ee-02",
						},
						"buildarch:uristring": []string{
							"x86_64",
						},
					},
					Kargs: []string{
						"console=tty0",
					},
				},
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-01",
						},
					},
					Kargs: []string{
						"console=ttyS0,115200n8",
					},
				},
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-01",
						},
						"buildarch:uristring": []string{
							"x86_64",
						},
					},
					Kargs: []string{
						"console=ttyS0",
					},
				},
			},
			"aa-bb-cc-dd-ee-02": []*Profile{
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-01",
							"aa-bb-cc-dd-ee-02",
						},
						"buildarch:uristring": []string{
							"x86_64",
						},
					},
					Kargs: []string{
						"console=tty0",
					},
				},
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-02",
						},
					},
					InitrdURLs: []string{
						"fcos/initramfs-2.img",
					},
					Kargs: []string{
						"console=tty0",
					},
				},
			},
		},
		"buildarch:uristring": map[string][]*Profile{
			"x86_64": []*Profile{
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-01",
							"aa-bb-cc-dd-ee-02",
						},
						"buildarch:uristring": []string{
							"x86_64",
						},
					},
					Kargs: []string{
						"console=tty0",
					},
				},
				{
					Selector: map[string][]string{
						"mac:hexhyp": []string{
							"aa-bb-cc-dd-ee-01",
						},
						"buildarch:uristring": []string{
							"x86_64",
						},
					},
					Kargs: []string{
						"console=ttyS0",
					},
				},
			},
		},
	}

	expectedMergedProfile := &Profile{
		Selector: nil,
		Kargs: []string{
			"console=tty0",
			"console=ttyS0",
		},
	}

	profiles, err := NewProfileConfig(yamlConfig)
	assert.NoError(t, err)
	assert.Equal(t, expectedSelectorMap, profiles.selectorMap)

	mergeddProfiles := profiles.GetMerged(map[string]string{
		"mac:hexhyp":          "aa-bb-cc-dd-ee-01",
		"buildarch:uristring": "x86_64",
	})
	assert.Equal(t, expectedMergedProfile, mergeddProfiles)
}
