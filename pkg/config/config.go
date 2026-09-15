package config

import (
	"crypto/tls"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"time"

	"github.com/randomcoww/ipxe-presign/pkg/presigner"
	"github.com/randomcoww/ipxe-presign/pkg/tlsutil"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
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
}

type Config struct {
	Listen           string
	ServerTLSConfig  *tls.Config
	AllowedClientCNs []string
	AdvertiseURL     string
	Presigner        *presigner.Presigner
}

// LoadConfig reads and validates the boot profile config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	raw := &yamlConfig{
		Listen:     "0.0.0.0:8443",
		S3Endpoint: "https://s3.amazonaws.com",
		S3Region:   "us-east-1",
		PresignTTL: 60 * time.Second,
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// --- client CNs ---

	cfg := &Config{}

	if len(raw.AllowedClientCNs) == 0 {
		return nil, fmt.Errorf("allowedClientCNs must include at least one commonName")
	}
	cfg.AllowedClientCNs = raw.AllowedClientCNs

	// --- listen ---

	addrPort, err := netip.ParseAddrPort(raw.Listen)
	if err != nil {
		return nil, fmt.Errorf("parse listen address and port: %w", err)
	}
	cfg.Listen = fmt.Sprintf("%s:%d", addrPort.Addr(), addrPort.Port())

	// --- server tls ---

	cfg.ServerTLSConfig, err = tlsutil.BuildTLSConfig(raw.ServerCert, raw.ServerKey, raw.TrustedCAs)
	if err != nil {
		return nil, fmt.Errorf("building server TLS config: %w", err)
	}

	// --- presigner ---

	if raw.PresignTTL <= 0 {
		return nil, fmt.Errorf("presignTTL must be greater than 0")
	}
	if raw.S3Bucket == "" {
		return nil, fmt.Errorf("missing s3Bucket")
	}
	u, err := url.Parse(raw.S3Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse advertise url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("S3 URL scheme must be HTTPS")
	}
	tlsConfig, err := tlsutil.BuildTLSCAConfig(raw.S3TrustedCAs)
	if err != nil {
		return nil, fmt.Errorf("building S3 TLS config: %w", err)
	}
	cfg.Presigner, err = presigner.NewPresigner(fmt.Sprintf("%s://%s", u.Scheme, u.Host), raw.S3Region, raw.S3Bucket, tlsConfig, raw.PresignTTL)
	if err != nil {
		return nil, fmt.Errorf("presigner client: %v", err)
	}

	return cfg, nil
}
