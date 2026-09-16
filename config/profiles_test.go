package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProfileConfig(t *testing.T) {
	yamlConfig := &YamlConfig{
		Profiles: []*Profile{
			{
				Selector: map[string][]string{},
				InitrdURLs: []string{
					"fcos/initramfs.img",
				},
			},
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
						"aarch64",
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
					"buildarch:uristring": []string{
						"x86_64",
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

	profiles := NewProfileConfig(yamlConfig)

	mergedProfiles, ok := profiles.GetMerged(map[string]string{
		"mac:hexhyp":          "aa-bb-cc-dd-ee-01",
		"buildarch:uristring": "x86_64",
	})
	expectedMergedProfile := &Profile{
		Selector: nil,
		InitrdURLs: []string{
			"fcos/initramfs.img",
		},
		Kargs: []string{
			"console=tty0",
			"console=ttyS0,115200n8",
		},
	}
	assert.Equal(t, expectedMergedProfile, mergedProfiles)
	assert.True(t, ok)
}
