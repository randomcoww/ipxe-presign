package main

import (
	"context"
	"fmt"
	"strings"
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
// credentials. The endpoint may be a bare host:port (assumed https)
// or carry an explicit http:// or https:// prefix. The region must be
// set so presigning never needs to resolve the bucket location
// remotely.
func NewPresigner(endpoint, region, accessKey, secretKey, bucket string, ttl time.Duration) (*Presigner, error) {
	secure := true
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		endpoint = strings.TrimPrefix(endpoint, "http://")
		secure = false
	case strings.HasPrefix(endpoint, "https://"):
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
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
