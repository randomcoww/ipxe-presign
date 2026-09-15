package render

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	rawYaml := `
advertiseURL: "https://ipxe.local:8443/ipxe"
ChainIPXETemplate: |
  #!ipxe
  chain {{.AdvertiseURL}}?mac=${mac:hexhyp}
BootIPXETemplate: |
  #!ipxe
  kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.IgnitionURL}} coreos.live.rootfs_url={{.RootfsURL}}
  initrd{{range .InitrdURLs}} {{.}}{{end}}
  boot
`

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(rawYaml), 0644)
	if err != nil {
		t.Fatalf("Create test config: %v", err)
	}

	parsed, err := LoadConfig(filepath.Join(dir, "config.yaml"))
	assert.NoError(t, err)

	assert.Equal(t, fmt.Sprintf(`#!ipxe
chain %s?mac=${mac:hexhyp}
`, "https://ipxe.local:8443/ipxe"), parsed.ChainIPXEScript)

	assert.Equal(t, `#!ipxe
exit
`, parsed.ExitIPXEScript)
}

func TestRenderBootIPXE(t *testing.T) {
	cfg := &Config{
		BootIPXETemplate: template.Must(template.New("boot").Parse(`#!ipxe
kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.IgnitionURL}} coreos.live.rootfs_url={{.RootfsURL}}
initrd{{range .InitrdURLs}} {{.}}{{end}}
boot
`)),
	}

	c := &BootIPXE{
		KernelURL:   "https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k",
		InitrdURLs:  []string{"https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i"},
		Kargs:       []string{"console=tty0", "ignition.firstboot"},
		IgnitionURL: "https://minio.internal:9000/presigned/ignition/worker.ign?sig=g",
		RootfsURL:   "https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r",
	}

	expectedRender := `#!ipxe
kernel https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k console=tty0 ignition.firstboot ignition.config.url=https://minio.internal:9000/presigned/ignition/worker.ign?sig=g coreos.live.rootfs_url=https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r
initrd https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i
boot
`

	render, err := cfg.RenderBootIPXE(c)
	assert.NoError(t, err)
	assert.Equal(t, expectedRender, render)
}
