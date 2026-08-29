package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// ipxeScript is the template input for a rendered boot script.
type ipxeScript struct {
	KernelURL   string
	InitrdURLs  []string
	Kargs       []string
	IgnitionURL string
	RootfsURL   string
}

// ipxeTmpl renders a boot script. The pre-signed ignition and rootfs
// URLs are appended to the profile's kargs as ignition.url= and
// rootfs.url=, matching the FCOS kernel argument convention used by
// the profile's own kargs (e.g. ignition.firstboot). If your initramfs
// expects different argument names, adjust this template.
var ipxeTmpl = template.Must(template.New("ipxe").Parse(`#!ipxe
kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.url={{.IgnitionURL}} rootfs.url={{.RootfsURL}}
{{range .InitrdURLs}}initrd {{.}}
{{end}}boot
`))

// exitScript is rendered for clients whose certificate does not match
// any configured profile: iPXE prints a message and stops the chain.
const exitScript = "#!ipxe\nexit\n"

// RenderIPXE renders the boot script for the given inputs.
func RenderIPXE(s ipxeScript) (string, error) {
	var buf bytes.Buffer
	if err := ipxeTmpl.Execute(&buf, s); err != nil {
		return "", fmt.Errorf("rendering iPXE script: %w", err)
	}
	return buf.String(), nil
}
