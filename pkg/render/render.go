package render

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"text/template"

	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	AdvertiseURL      string `yaml:"advertiseURL"`
	BootIPXETemplate  string `yaml:"bootIPXETemplate,omitempty"`
	ChainIPXETemplate string `yaml:"chainIPXETemplate,omitempty"`
	ExitIPXEScript    string `yaml:"exitIPXETemplate,omitempty"`
}

type Config struct {
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

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	raw := &yamlConfig{
		// First-stage script. iPXE substitutes ${mac:hexhyp}
		// and ${uuid} at parse time, then chains to the second-stage URL with
		// the node's MAC address — which is what selects the boot profile.
		ChainIPXETemplate: `#!ipxe
chain {{.AdvertiseURL}}?mac=${mac:hexhyp}
`,
		// Second-stage boot script. The presigned kernel,
		// initrd, ignition and rootfs URLs are substituted in; ignition and
		// rootfs are passed as kernel arguments.
		BootIPXETemplate: `#!ipxe
kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.IgnitionURL}} coreos.live.rootfs_url={{.RootfsURL}}
initrd{{range .InitrdURLs}} {{.}}{{end}}
boot
`,
		ExitIPXEScript: `#!ipxe
exit
`,
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
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

	// -- templates ---

	cfg := &Config{
		ExitIPXEScript: raw.ExitIPXEScript,
	}
	t, err := template.New("ChainIPXETemplate").Parse(raw.ChainIPXETemplate)
	if err != nil {
		return nil, fmt.Errorf("parse iPXE chain template: %w", err)
	}
	var script bytes.Buffer

	if err := t.Execute(&script, struct{ AdvertiseURL string }{advertiseURL}); err != nil {
		return nil, fmt.Errorf("rendering iPXE chain script: %w", err)
	}

	cfg.ChainIPXEScript = script.String()

	cfg.BootIPXETemplate, err = template.New("BootIPXETemplate").Parse(raw.BootIPXETemplate)
	if err != nil {
		return nil, fmt.Errorf("parse iPXE boot template: %w", err)
	}

	return cfg, nil
}

// RenderIPXE renders the second-stage boot script.
func (cfg *Config) RenderBootIPXE(s *BootIPXE) (string, error) {
	var buf bytes.Buffer
	if err := cfg.BootIPXETemplate.Execute(&buf, s); err != nil {
		return "", fmt.Errorf("rendering iPXE boot script: %w", err)
	}
	return buf.String(), nil
}
