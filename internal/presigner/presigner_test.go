// hits real local minio instance for test
// build with terraform/podman under test/

package presigner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/randomcoww/ipxe-presign/config"
	"github.com/randomcoww/ipxe-presign/internal/tlsutil"
	"github.com/stretchr/testify/assert"
)

const (
	baseTestPath  string = "../../test/outputs"
	minioUser     string = "rootUser"
	minioPassword string = "rootPassword"
)

func TestPresigner(t *testing.T) {
	yamlConfig := &config.YamlConfig{
		PresignTTL:   1 * time.Second,
		S3Endpoint:   "https://127.0.0.1:9000",
		S3Bucket:     "ipxe",
		S3Region:     "us-east-1",
		S3TrustedCAs: []string{filepath.Join(baseTestPath, "minio", "certs", "CAs", "ca.crt")},
	}

	t.Setenv("AWS_ACCESS_KEY_ID", minioUser)
	t.Setenv("AWS_SECRET_ACCESS_KEY", minioPassword)

	presigner, err := NewPresignerFromConfig(yamlConfig)
	assert.NoError(t, err)

	clientCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// --- upload some test data ---

	if _, err := uploadTestData(t, presigner, clientCtx, "test-key-1", bytes.NewBufferString("test-val-1")); err != nil {
		t.Fatalf("Create test data: %v", err)
	}

	// --- create signed URL for test data ---

	presignCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url, err := presigner.URL(presignCtx, "test-key-1")
	assert.NoError(t, err)

	// --- test downloading without credentials ---

	tlsConfig, err := tlsutil.BuildTLSCAConfig([]string{filepath.Join(baseTestPath, "minio", "certs", "CAs", "ca.crt")})
	if err != nil {
		t.Fatal("Create test TLSConfig: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Get(url)
	assert.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)

	assert.Equal(t, "test-val-1", string(body))

	// --- should expire ---

	time.Sleep(2 * time.Second)

	respExpired, err := client.Get(url)
	assert.NoError(t, err)
	defer respExpired.Body.Close()

	bodyExpired, err := io.ReadAll(respExpired.Body)
	assert.NoError(t, err)
	assert.Contains(t, string(bodyExpired), "Request has expired")
}

// --- helper ---

func uploadTestData(t *testing.T, p *Presigner, ctx context.Context, key string, reader io.Reader) (int64, error) {
	t.Helper()

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
