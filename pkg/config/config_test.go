package config

import (
  "os"
  "path/filepath"
  "testing"

  "github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
  rawYaml := `
  profiles:
  - kernel_s3_resource: "fcos/vmlinuz"
    initrd_s3_resources:
    - "fcos/initramfs.img"
    ignition_s3_resource: "ignition/worker.ign"
    rootfs_s3_resource: "fcos/worker.img"
    kargs:
    - "console=tty0"
    - "console=ttyS0,115200n8"
    - "ignition.firstboot"
    - "ignition.platform.id=metal"
    macs:
    - "aa:bb:cc:dd:ee:01"
    - "aa:bb:cc:dd:ee:02"	
  `

  expectedConfig := &Config{
    ProfileByMac: map[string]*Profile{
      "aa:bb:cc:dd:ee:01": &Profile{
        KernelS3Resource:   "fcos/vmlinuz",
        InitrdS3Resources:  []string{"fcos/initramfs.img"},
        IgnitionS3Resource: "ignition/worker.ign",
        RootfsS3Resource:   "fcos/worker.img",
        Kargs: []string{
          "console=tty0",
          "console=ttyS0,115200n8",
          "ignition.firstboot",
          "ignition.platform.id=metal",
        },
        Macs: []string{
          "aa:bb:cc:dd:ee:01",
          "aa:bb:cc:dd:ee:02",
        },
      },
      "aa:bb:cc:dd:ee:02": &Profile{
        KernelS3Resource:   "fcos/vmlinuz",
        InitrdS3Resources:  []string{"fcos/initramfs.img"},
        IgnitionS3Resource: "ignition/worker.ign",
        RootfsS3Resource:   "fcos/worker.img",
        Kargs: []string{
          "console=tty0",
          "console=ttyS0,115200n8",
          "ignition.firstboot",
          "ignition.platform.id=metal",
        },
        Macs: []string{
          "aa:bb:cc:dd:ee:01",
          "aa:bb:cc:dd:ee:02",
        },
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
