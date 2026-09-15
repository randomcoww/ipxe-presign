package render

import (
	"bytes"
	"fmt"
	"net/url"
	"text/template"

	"github.com/randomcoww/ipxe-presign/config"
)

type Render struct {
	BootIPXETemplate *template.Template
	ChainIPXEScript  string
	ExitIPXEScript   string
}

// ipxeScript is the template input for a rendered boot script.
type BootIPXE struct {
	KernelURL   string
	InitrdURLs  []string
	Kargs       []string
	IgnitionURL string
	RootfsURL   string
}

func NewRenderFromConfig(raw *config.YamlConfig) (*Render, error) {
	render := &Render{
		ExitIPXEScript: raw.ExitIPXEScript,
	}

	// --- advertise ---

	u, err := url.Parse(raw.AdvertiseURL)
	if err != nil {
		return nil, fmt.Errorf("parse advertise url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("advertise URL scheme must be HTTPS")
	}
	advertiseURL := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)

	// --- templates ---

	t, err := template.New("ChainIPXETemplate").Parse(raw.ChainIPXETemplate)
	if err != nil {
		return nil, fmt.Errorf("parse iPXE chain template: %w", err)
	}
	var script bytes.Buffer

	if err := t.Execute(&script, struct{ AdvertiseURL string }{advertiseURL}); err != nil {
		return nil, fmt.Errorf("rendering iPXE chain script: %w", err)
	}

	render.ChainIPXEScript = script.String()
	render.BootIPXETemplate, err = template.New("BootIPXETemplate").Parse(raw.BootIPXETemplate)
	if err != nil {
		return nil, fmt.Errorf("parse iPXE boot template: %w", err)
	}

	return render, nil
}

// RenderIPXE renders the second-stage boot script.
func (r *Render) RenderBootIPXE(s *BootIPXE) (string, error) {
	var buf bytes.Buffer
	if err := r.BootIPXETemplate.Execute(&buf, s); err != nil {
		return "", fmt.Errorf("rendering iPXE boot script: %w", err)
	}
	return buf.String(), nil
}
