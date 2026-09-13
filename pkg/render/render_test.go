package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderIpxe(t *testing.T) {
	ipxeServe := IpxeServe{
		KernelURL:   "https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k",
		InitrdURLs:  []string{"https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i"},
		Kargs:       []string{"console=tty0", "ignition.firstboot"},
		IgnitionURL: "https://minio.internal:9000/presigned/ignition/worker.ign?sig=g",
		RootfsURL:   "https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r",
	}

	expectedRender := `#!ipxe
kernel https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k console=tty0 ignition.firstboot ignition.url=https://minio.internal:9000/presigned/ignition/worker.ign?sig=g rootfs.url=https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r
initrd https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i
boot
`

	render, err := RenderIPXE(ipxeServe)
	assert.NoError(t, err)
	assert.Equal(t, expectedRender, render)
}

func TestRenderEntry(t *testing.T) {
	ipxeEntry := "https://ipxe.internal:8443/ipxe"

	expectedRender := `#!ipxe
chain https://ipxe.internal:8443/ipxe?mac=${mac:hexhyp}&uuid=${uuid}
`

	render, err := RenderEntry(ipxeEntry)
	assert.NoError(t, err)
	assert.Equal(t, expectedRender, render)
}
