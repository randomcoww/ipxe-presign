package config

import (
	"crypto/tls"
	"fmt"
	"net/netip"
	"os"
	"time"

	"github.com/randomcoww/ipxe-presign/pkg/tlsutil"
	"gopkg.in/yaml.v3"
)

type YamlConfig struct {
	Listen           string        `yaml:"listen,omitempty"`
	ServerCert       string        `yaml:"serverCert"`
	ServerKey        string        `yaml:"serverKey"`
	TrustedCAs       []string      `yaml:"trustedCAs,omitempty"`
	AllowedClientCNs []string      `yaml:"allowedClientCNs"`
	S3Endpoint       string        `yaml:"s3Endpoint,omitempty"`
	S3Bucket         string        `yaml:"s3Bucket"`
	S3Region         string        `yaml:"s3Region,omitempty"`
	S3TrustedCAs     []string      `yaml:"s3TrustedCAs,omitempty"`
	PresignTTL       time.Duration `yaml:"presignTTL,omitempty"`

	AdvertiseURL      string `yaml:"advertiseURL"`
	BootIPXETemplate  string `yaml:"bootIPXETemplate,omitempty"`
	ChainIPXETemplate string `yaml:"chainIPXETemplate,omitempty"`
	ExitIPXEScript    string `yaml:"exitIPXETemplate,omitempty"`

	BaseProfile     *Profile   `yaml:"baseProfile"`
	OverlayProfiles []*Profile `yaml:"overlayProfiles"`

	ServerTLSConfig *tls.Config
}

// Profile describes how a node boots. Every resource is an object key
// inside the configured S3 bucket and is handed to the node as a
// short-lived pre-signed URL.
type Profile struct {
	Selector         []string `yaml:"selector,omitempty"`
	KernelResource   string   `yaml:"kernelResource,omitempty"`
	InitrdResources  []string `yaml:"initrdResources,omitempty"`
	IgnitionResource string   `yaml:"ignitionResource,omitempty"`
	RootfsResource   string   `yaml:"rootfsResource,omitempty"`
	Kargs            []string `yaml:"kargs,omitempty"`
}

func LoadConfig(path string) (*YamlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	raw := &YamlConfig{
		Listen:     "0.0.0.0:8443",
		S3Endpoint: "https://s3.amazonaws.com",
		S3Region:   "us-east-1",
		PresignTTL: 60 * time.Second,

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

	if len(raw.AllowedClientCNs) == 0 {
		return nil, fmt.Errorf("allowedClientCNs must include at least one commonName")
	}

	// --- listen ---

	addrPort, err := netip.ParseAddrPort(raw.Listen)
	if err != nil {
		return nil, fmt.Errorf("parse listen address and port: %w", err)
	}
	raw.Listen = fmt.Sprintf("%s:%d", addrPort.Addr(), addrPort.Port())

	// --- server tls ---

	raw.ServerTLSConfig, err = tlsutil.BuildTLSConfig(raw.ServerCert, raw.ServerKey, raw.TrustedCAs)
	if err != nil {
		return nil, fmt.Errorf("building server TLS config: %w", err)
	}

	return raw, nil
}
