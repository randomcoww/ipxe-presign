package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProfileConfig(t *testing.T) {
	yamlConfig := &YamlConfig{
		BaseProfile: &Profile{
			KernelResource: "fcos/vmlinuz",
			InitrdResources: []string{
				"fcos/initramfs.img",
			},
			IgnitionResource: "ignition/worker.ign",
			RootfsResource:   "fcos/worker.img",
			Kargs: []string{
				"ignition.firstboot",
				"ignition.platform.id=metal",
			},
		},
		OverlayProfiles: []*Profile{
			{
				Selector: []string{
					"aa-bb-cc-dd-ee-01",
					"aa-bb-cc-dd-ee-02",
				},
				IgnitionResource: "ignition/worker-v1.ign",
				Kargs: []string{
					"console=tty0",
				},
			},
			{
				Selector: []string{
					"aa-bb-cc-dd-ee-01",
				},
				IgnitionResource: "ignition/worker-v2.ign",
				Kargs: []string{
					"console=ttyS0,115200n8",
				},
			},
			{
				Selector: []string{
					"aa-bb-cc-dd-ee-02",
				},
				InitrdResources: []string{
					"fcos/initramfs-2.img",
				},
				Kargs: []string{
					"console=tty0",
				},
			},
		},
	}

	expectedProfiles := &Profiles{
		BaseProfile: &Profile{
			KernelResource: "fcos/vmlinuz",
			InitrdResources: []string{
				"fcos/initramfs.img",
			},
			IgnitionResource: "ignition/worker.ign",
			RootfsResource:   "fcos/worker.img",
			Kargs: []string{
				"ignition.firstboot",
				"ignition.platform.id=metal",
			},
			Selector: nil,
		},
		Profiles: map[string]*Profile{
			"aa-bb-cc-dd-ee-01": &Profile{
				KernelResource: "fcos/vmlinuz",
				InitrdResources: []string{
					"fcos/initramfs.img",
				},
				IgnitionResource: "ignition/worker-v2.ign",
				RootfsResource:   "fcos/worker.img",
				Kargs: []string{
					"console=tty0",
					"console=ttyS0,115200n8",
					"ignition.firstboot",
					"ignition.platform.id=metal",
				},
				Selector: nil,
			},
			"aa-bb-cc-dd-ee-02": &Profile{
				KernelResource: "fcos/vmlinuz",
				InitrdResources: []string{
					"fcos/initramfs.img",
					"fcos/initramfs-2.img",
				},
				IgnitionResource: "ignition/worker-v1.ign",
				RootfsResource:   "fcos/worker.img",
				Kargs: []string{
					"console=tty0",
					"ignition.firstboot",
					"ignition.platform.id=metal",
				},
				Selector: nil,
			},
		},
	}

	profiles, err := NewProfileConfig(yamlConfig)
	assert.NoError(t, err)
	assert.Equal(t, expectedProfiles, profiles)
}
