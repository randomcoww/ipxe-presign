package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	rawYaml := `
  global:
    kernel_s3_resource: "fcos/vmlinuz"
    initrd_s3_resources:
    - "fcos/initramfs.img"
    ignition_s3_resource: "ignition/worker.ign"
    rootfs_s3_resource: "fcos/worker.img"
    kargs:
    - "ignition.firstboot"
    - "ignition.platform.id=metal"

  overlays:
  - selector:
    - "aa:bb:cc:dd:ee:01"
    - "aa:bb:cc:dd:ee:02"
    ignition_s3_resource: "ignition/worker-v1.ign"
    kargs:
    - "console=tty0"

  - selector:
    - "aa:bb:cc:dd:ee:01"
    ignition_s3_resource: "ignition/worker-v2.ign"
    kargs:
    - "console=ttyS0,115200n8"

  - selector:
    - "aa:bb:cc:dd:ee:02"
    initrd_s3_resources:
    - "fcos/initramfs-2.img"
    kargs:
    - "console=tty0"
  `

	expectedConfig := &Config{
		Profiles: map[string]*Profile{
			"aa:bb:cc:dd:ee:01": &Profile{
				KernelS3Resource: "fcos/vmlinuz",
				InitrdS3Resources: []string{
					"fcos/initramfs.img",
				},
				IgnitionS3Resource: "ignition/worker-v2.ign",
				RootfsS3Resource:   "fcos/worker.img",
				Kargs: []string{
					"console=tty0",
					"console=ttyS0,115200n8",
					"ignition.firstboot",
					"ignition.platform.id=metal",
				},
				Selector: nil,
			},
			"aa:bb:cc:dd:ee:02": &Profile{
				KernelS3Resource: "fcos/vmlinuz",
				InitrdS3Resources: []string{
					"fcos/initramfs.img",
					"fcos/initramfs-2.img",
				},
				IgnitionS3Resource: "ignition/worker-v1.ign",
				RootfsS3Resource:   "fcos/worker.img",
				Kargs: []string{
					"console=tty0",
					"ignition.firstboot",
					"ignition.platform.id=metal",
				},
				Selector: nil,
			},
		},
	}

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(rawYaml), 0644)
	if err != nil {
		t.Fatalf("Create tst config: %v", err)
	}

	parsed, err := LoadConfig(filepath.Join(dir, "config.yaml"))
	assert.NoError(t, err)
	assert.Equal(t, expectedConfig, parsed)
}
