package handler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"text/template"

	"github.com/randomcoww/ipxe-presign/config"
)

// First-stage script. iPXE substitutes ${mac:hexhyp}
// and ${uuid} at parse time, then chains to the second-stage URL with
// the node's MAC address — which is what selects the boot profile.
const ipxeChain = `#!ipxe
chain ipxe?mac:hexhyp=${mac:hexhyp}&buildarch:uristring=${buildarch:uristring}&uuid=${uuid}
`

// Second-stage boot script. The presigned kernel,
// initrd, ignition and rootfs URLs are substituted in; ignition and
// rootfs are passed as kernel arguments.
var ipxeTemplate = template.Must(template.New("ipxeTemplate").Parse(`#!ipxe
kernel {{.KernelURL}}{{range $karg := .Kargs}} {{$karg}}{{end}}
{{- range $element := .InitrdURLs }}
initrd {{$element}}
{{- end}}
boot
`))

const ipxeExit = `#!ipxe
exit
`

func (h *Handler) renderIPXETemplate(ctx context.Context, p *config.Profile, selector map[string]string) (string, error) {
	var err error
	for k, v := range selector {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	tmpl := template.New("presigner").Funcs(template.FuncMap{
		"presign": func(resource string) (string, error) {
			url, err := h.Presigner.URL(ctx, resource)
			if err != nil {
				return "", fmt.Errorf("presign URL: %w", err)
			}
			return url, nil
		},
	})

	kernelURL, err := h.presignElement(tmpl, os.ExpandEnv(p.KernelURL))
	if err != nil {
		return "", fmt.Errorf("parse kernel URL: %w", err)
	}
	kargs := []string{}
	for _, karg := range p.Kargs {
		k, err := h.presignElement(tmpl, os.ExpandEnv(karg))
		if err != nil {
			return "", fmt.Errorf("parse karg: %w", err)
		}
		kargs = append(kargs, k)
	}
	initrdURLs := []string{}
	for _, initrdURL := range p.InitrdURLs {
		i, err := h.presignElement(tmpl, os.ExpandEnv(initrdURL))
		if err != nil {
			return "", fmt.Errorf("parse initrd URL: %w", err)
		}
		initrdURLs = append(initrdURLs, i)
	}

	var buf bytes.Buffer
	err = ipxeTemplate.Execute(&buf, &config.Profile{
		KernelURL:  kernelURL,
		Kargs:      kargs,
		InitrdURLs: initrdURLs,
	})
	if err != nil {
		return "", fmt.Errorf("parse ipxe template: %w", err)
	}
	return buf.String(), nil
}

func (h *Handler) presignElement(tmpl *template.Template, param string) (string, error) {
	t, err := tmpl.Parse(param)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", param, err)
	}
	b := bytes.Buffer{}
	if err := t.Execute(&b, struct{}{}); err != nil {
		return "", fmt.Errorf("render %s: %w", param, err)
	}
	return b.String(), nil
}
