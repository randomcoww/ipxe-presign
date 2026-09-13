package presigner

import (
  "bytes"
  "context"
  "crypto/tls"
  "fmt"
  "io"
  "net"
  "net/http"
  "net/url"
  "time"

  "github.com/minio/minio-go/v7"
  "github.com/minio/minio-go/v7/pkg/credentials"
)

// Presigner issues short-lived pre-signed GET URLs for objects in a
// single bucket. Presigning is a local SigV4 operation — with a region
// configured, no network round-trip to the S3 endpoint is performed.
type Presigner struct {
  client *minio.Client
  bucket string
  ttl    time.Duration
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

func (p *Presigner) uploadTest(ctx context.Context, key string, reader io.Reader) (int64, error) {
  buf := &bytes.Buffer{}
  size, err := io.Copy(buf, reader)
  if err != nil {
    return size, fmt.Errorf("upload: failed to create buffer: %w", err)
  }
  if size == 0 {
    return size, fmt.Errorf("upload: size is 0")
  }
  if _, err = p.client.PutObject(ctx, p.bucket, key, buf, size, minio.PutObjectOptions{
    AutoChecksum: minio.ChecksumCRC32,
  }); err != nil {
    if cleanupErr := p.client.RemoveIncompleteUpload(ctx, p.bucket, key); cleanupErr != nil {
      return size, fmt.Errorf("upload: failed to put object: %w\n  failed to cleanup incomplete upload: %w", err, cleanupErr)
    }
    return size, fmt.Errorf("upload: failed to put object: %w", err)
  }
  return size, nil
}
