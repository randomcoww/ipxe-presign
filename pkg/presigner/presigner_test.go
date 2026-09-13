// hits real local minio instance for test
// build with terraform/podman under test/

package presigner

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/randomcoww/ipxe-presign/pkg/tlsutil"
	"github.com/stretchr/testify/assert"
)

const (
	baseTestPath  string = "../../test/outputs"
	minioUser     string = "rootUser"
	minioPassword string = "rootPassword"
)

func TestPresign(t *testing.T) {
	tlsConfig, err := tlsutil.BuildTLSCAConfig(filepath.Join(baseTestPath, "minio", "certs", "CAs", "ca.crt"))
	if err != nil {
		t.Fatalf("Generate test CA: %v", err)
	}
	t.Setenv("AWS_ACCESS_KEY_ID", minioUser)
	t.Setenv("AWS_SECRET_ACCESS_KEY", minioPassword)

	presigner, err := NewPresigner("https://127.0.0.1:9000", "us-east-1", "ipxe", tlsConfig, 1*time.Second)
	assert.NoError(t, err)

	clientCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// --- upload some test data --- //

	if _, err := presigner.uploadTest(clientCtx, "test-key-1", bytes.NewBufferString("test-val-1")); err != nil {
		t.Fatalf("Create test data: %v", err)
	}

	// -- create signed URL for test data -- //

	presignCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url, err := presigner.URL(presignCtx, "test-key-1")
	assert.NoError(t, err)

	// -- test downloading without credentials -- //

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

	// -- should expire -- //

	time.Sleep(2 * time.Second)

	respExpired, err := client.Get(url)
	assert.NoError(t, err)
	defer respExpired.Body.Close()

	bodyExpired, err := io.ReadAll(respExpired.Body)
	assert.NoError(t, err)
	assert.Contains(t, string(bodyExpired), "Request has expired")
}
