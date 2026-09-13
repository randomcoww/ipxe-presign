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
    ignition_s3_resource: "ignition/worker-1-v1.ign"
  - selector:
    - "aa:bb:cc:dd:ee:01"
    ignition_s3_resource: "ignition/worker-1-v2.ign"
    kargs:
    - "console=tty0"
    - "console=ttyS0,115200n8"
    - "worker-1-arg"

  - selector:
    - "aa:bb:cc:dd:ee:02"
    initrd_s3_resources:
    - "fcos/initramfs-2.img"
    ignition_s3_resource: "ignition/worker-2.ign"
    kargs:
    - "worker-2-arg"
  `

	expectedConfig := &Config{
		Profiles: map[string]*Profile{
			"aa:bb:cc:dd:ee:01": &Profile{
				KernelS3Resource: "fcos/vmlinuz",
				InitrdS3Resources: []string{
					"fcos/initramfs.img",
				},
				IgnitionS3Resource: "ignition/worker-1-v2.ign",
				RootfsS3Resource:   "fcos/worker.img",
				Kargs: []string{
					"ignition.firstboot",
					"ignition.platform.id=metal",
					"console=tty0",
					"console=ttyS0,115200n8",
					"worker-1-arg",
				},
				Selector: nil,
			},
			"aa:bb:cc:dd:ee:02": &Profile{
				KernelS3Resource: "fcos/vmlinuz",
				InitrdS3Resources: []string{
					"fcos/initramfs.img",
					"fcos/initramfs-2.img",
				},
				IgnitionS3Resource: "ignition/worker-2.ign",
				RootfsS3Resource:   "fcos/worker.img",
				Kargs: []string{
					"ignition.firstboot",
					"ignition.platform.id=metal",
					"worker-2-arg",
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
