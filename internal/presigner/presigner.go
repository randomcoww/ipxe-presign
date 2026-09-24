package presigner

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/randomcoww/ipxe-presign/config"
	"github.com/randomcoww/ipxe-presign/internal/tlsutil"
)

// Presigner issues short-lived pre-signed GET URLs for objects in a
// single bucket. Presigning is a local SigV4 operation — with a region
// configured, no network round-trip to the S3 endpoint is performed.
type Presigner struct {
	client    *minio.Client
	bucket    string
	ttl       time.Duration
	TLSConfig *tls.Config
}

func NewPresignerFromConfig(raw *config.YamlConfig) (*Presigner, error) {
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
	presigner, err := NewPresigner(fmt.Sprintf("%s://%s", u.Scheme, u.Host), raw.S3Region, raw.S3Bucket, tlsConfig, raw.PresignTTL)
	if err != nil {
		return nil, fmt.Errorf("presigner client: %v", err)
	}
	return presigner, nil
}

// NewPresigner builds a S3 client from endpoint, region, and static
// credentials. The endpoint may be https://host:port.
// set so presigning never needs to resolve the bucket location
// remotely.
func NewPresigner(endpoint, region, bucket string, tlsConfig *tls.Config, ttl time.Duration) (*Presigner, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("S3 URL scheme must be HTTPS")
	}
	opts := &minio.Options{
		Creds:  credentials.NewEnvAWS(),
		Secure: true,
		Region: region,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 30 * time.Second, // value taken from http.DefaultTransport
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second, // value taken from http.DefaultTransport
			TLSClientConfig:     tlsConfig,
		},
	}
	client, err := minio.New(u.Host, opts)
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}
	return &Presigner{client: client, bucket: bucket, ttl: ttl}, nil
}

// URL returns a pre-signed GET URL for the given object key, valid for
// the configured TTL.
func (p *Presigner) URL(ctx context.Context, object string) (string, error) {
	u, err := p.client.PresignedGetObject(ctx, p.bucket, object, p.ttl, nil)
	if err != nil {
		return "", fmt.Errorf("presigning %s/%s: %w", p.bucket, object, err)
	}
	return u.String(), nil
}
