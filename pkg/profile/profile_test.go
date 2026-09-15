package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	rawYaml := `
  globalProfile:
    bootIPXETemplate: |
      #!ipxe
      kernel {{.Kernel}}{{range .Kargs}} {{.}}{{end}} ignition_url={{.Ignition}} rootfs_url={{.Rootfs}}
      initrd {{range .Initrds}} {{.}}{{end}}
      boot
    kernel: "fcos/vmlinuz"
    initrds:
    - "fcos/initramfs.img"
    ignition: "ignition/worker.ign"
    rootfs: "fcos/worker.img"
    kargs:
    - "ignition.firstboot"
    - "ignition.platform.id=metal"

  overlayProfiles:
  - selector:
    - "aa-bb-cc-dd-ee-01"
    - "aa-bb-cc-dd-ee-02"
    ignition: "ignition/worker-v1.ign"
    kargs:
    - "console=tty0"

  - selector:
    - "aa-bb-cc-dd-ee-01"
    bootIPXETemplate: |
      #!ipxe
      kernel {{.Kernel}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.Ignition}} coreos.live.rootfs_url={{.Rootfs}}
      initrd {{range .Initrds}} {{.}}{{end}}
      boot
    ignition: "ignition/worker-v2.ign"
    kargs:
    - "console=ttyS0,115200n8"

  - selector:
    - "aa-bb-cc-dd-ee-02"
    initrds:
    - "fcos/initramfs-2.img"
    kargs:
    - "console=tty0"
  `

	expectedConfig := &Config{
		GlobalProfile: &Profile{
			BootIPXETemplate: `#!ipxe
kernel {{.Kernel}}{{range .Kargs}} {{.}}{{end}} ignition_url={{.Ignition}} rootfs_url={{.Rootfs}}
initrd {{range .Initrds}} {{.}}{{end}}
boot
`,
			Kernel: "fcos/vmlinuz",
			Initrds: []string{
				"fcos/initramfs.img",
			},
			Ignition: "ignition/worker.ign",
			Rootfs:   "fcos/worker.img",
			Kargs: []string{
				"ignition.firstboot",
				"ignition.platform.id=metal",
			},
			Selector: nil,
		},
		Profiles: map[string]*Profile{
			"aa-bb-cc-dd-ee-01": &Profile{
				BootIPXETemplate: `#!ipxe
kernel {{.Kernel}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.Ignition}} coreos.live.rootfs_url={{.Rootfs}}
initrd {{range .Initrds}} {{.}}{{end}}
boot
`,
				Kernel: "fcos/vmlinuz",
				Initrds: []string{
					"fcos/initramfs.img",
				},
				Ignition: "ignition/worker-v2.ign",
				Rootfs:   "fcos/worker.img",
				Kargs: []string{
					"console=tty0",
					"console=ttyS0,115200n8",
					"ignition.firstboot",
					"ignition.platform.id=metal",
				},
				Selector: nil,
			},
			"aa-bb-cc-dd-ee-02": &Profile{
				BootIPXETemplate: `#!ipxe
kernel {{.Kernel}}{{range .Kargs}} {{.}}{{end}} ignition_url={{.Ignition}} rootfs_url={{.Rootfs}}
initrd {{range .Initrds}} {{.}}{{end}}
boot
`,
				Kernel: "fcos/vmlinuz",
				Initrds: []string{
					"fcos/initramfs.img",
					"fcos/initramfs-2.img",
				},
				Ignition: "ignition/worker-v1.ign",
				Rootfs:   "fcos/worker.img",
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
