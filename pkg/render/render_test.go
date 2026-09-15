package render

import (
	"fmt"
	"testing"

	"github.com/randomcoww/ipxe-presign/config"
	"github.com/stretchr/testify/assert"
)

func TestRenderConfig(t *testing.T) {
	yamlConfig := &config.YamlConfig{
		AdvertiseURL: "https://ipxe.local:8443/ipxe",
		ChainIPXETemplate: `#!ipxe
chain {{.AdvertiseURL}}?mac=${mac:hexhyp}
`,
		BootIPXETemplate: `#!ipxe
kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.IgnitionURL}} coreos.live.rootfs_url={{.RootfsURL}}
initrd{{range .InitrdURLs}} {{.}}{{end}}
boot
`,
		ExitIPXEScript: `#!ipxe
exit
`,
	}

	render, err := NewRenderFromConfig(yamlConfig)
	assert.NoError(t, err)

	assert.Equal(t, fmt.Sprintf(`#!ipxe
chain %s?mac=${mac:hexhyp}
`, "https://ipxe.local:8443/ipxe"), render.ChainIPXEScript)

	assert.Equal(t, `#!ipxe
exit
`, render.ExitIPXEScript)

	c := &BootIPXE{
		KernelURL:   "https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k",
		InitrdURLs:  []string{"https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i"},
		Kargs:       []string{"console=tty0", "ignition.firstboot"},
		IgnitionURL: "https://minio.internal:9000/presigned/ignition/worker.ign?sig=g",
		RootfsURL:   "https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r",
	}

	expectedBootIPXE := `#!ipxe
kernel https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k console=tty0 ignition.firstboot ignition.config.url=https://minio.internal:9000/presigned/ignition/worker.ign?sig=g coreos.live.rootfs_url=https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r
initrd https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i
boot
`

	bootIPXE, err := render.RenderBootIPXE(c)
	assert.NoError(t, err)
	assert.Equal(t, expectedBootIPXE, bootIPXE)
}
