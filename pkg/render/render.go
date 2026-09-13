package render

import (
  "bytes"
  "fmt"
  "text/template"
)

// ipxeScript is the template input for a rendered boot script.
type IpxeServe struct {
  KernelURL   string
  InitrdURLs  []string
  Kargs       []string
  IgnitionURL string
  RootfsURL   string
}

// ipxeTmpl renders the second-stage boot script. The presigned kernel,
// initrd, ignition and rootfs URLs are substituted in; ignition and
// rootfs are passed as kernel arguments. If your initramfs expects
// different argument names, adjust this template.
var ipxeTmpl = template.Must(template.New("ipxe").Parse(`#!ipxe
kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.url={{.IgnitionURL}} rootfs.url={{.RootfsURL}}
{{range .InitrdURLs}}initrd {{.}}
{{end}}boot
`))

// entryTmpl is the first-stage script. iPXE substitutes ${mac:hexhyp}
// and ${uuid} at parse time, then chains to the second-stage URL with
// the node's MAC address — which is what selects the boot profile.
var entryTmpl = template.Must(template.New("entry").Parse(`#!ipxe
chain {{.ChainURL}}?mac=${mac:hexhyp}&uuid=${uuid}
`))

// exitScript is served to authenticated clients whose MAC address
// matches no group: iPXE stops the boot chain.
const ExitScript = "#!ipxe\nexit\n"

// RenderIPXE renders the second-stage boot script.
func RenderIPXE(s IpxeServe) (string, error) {
  var buf bytes.Buffer
  if err := ipxeTmpl.Execute(&buf, s); err != nil {
    return "", fmt.Errorf("rendering iPXE boot script: %w", err)
  }
  return buf.String(), nil
}

// RenderEntry renders the first-stage entry script that chains back to
// the given second-stage URL.
func RenderEntry(chainURL string) (string, error) {
  var buf bytes.Buffer
  if err := entryTmpl.Execute(&buf, struct{ ChainURL string }{chainURL}); err != nil {
    return "", fmt.Errorf("rendering iPXE entry script: %w", err)
  }
  return buf.String(), nil
}
