package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"text/template"
	"time"

	"github.com/randomcoww/ipxe-presign/config"
	"github.com/randomcoww/ipxe-presign/pkg/render"
	"github.com/randomcoww/ipxe-presign/pkg/tlsutil"
	"github.com/randomcoww/ipxe-presign/tlstest"
	"github.com/stretchr/testify/assert"
)

// fakePresigner returns fixed URLs instead of contacting S3.
type fakePresigner struct {
	fail   error
	called []string
}

func (f *fakePresigner) URL(ctx context.Context, object string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.called = append(f.called, object)
	return "https://minio.internal:9000/presigned/" + object, nil
}

func testConfig() *config.Profiles {
	return &config.Profiles{
		BaseProfile: &config.Profile{},
		Profiles: map[string]*config.Profile{
			"aa-bb-cc-dd-ee-01": {
				KernelResource:   "fcos/vmlinuz-${buildarch:uristring}",
				InitrdResources:  []string{"fcos/initramfs-${buildarch:uristring}.img"},
				IgnitionResource: "ignition/worker-${mac:hexhyp}.ign",
				RootfsResource:   "fcos/worker-rootfs-${buildarch:uristring}.img",
				Kargs:            []string{"console=tty0", "ignition.firstboot"},
				Selector:         nil,
			},
		},
	}
}

func testRenderer() *render.Render {
	return &render.Render{
		BootIPXETemplate: template.Must(template.New("boot").Parse(`#!ipxe
kernel {{.KernelURL}}{{range .Kargs}} {{.}}{{end}} ignition.config.url={{.IgnitionURL}} coreos.live.rootfs_url={{.RootfsURL}}
initrd{{range .InitrdURLs}} {{.}}{{end}}
boot
`)),
		ChainIPXEScript: `#!ipxe
chain https://ipxe.internal:8443/ipxe?mac:hexhyp=${mac:hexhyp}&buildarch:uristring=${buildarch:uristring}
`,
		ExitIPXEScript: `#!ipxe
exit
`,
	}
}

func TestClientCommonName(t *testing.T) {
	tlsCNRequest := newTLSRequest(t, "ipxe-node-1", "/")
	assert.Equal(t, "ipxe-node-1", clientCommonName(tlsCNRequest))

	noTLSRequest, _ := http.NewRequest(http.MethodGet, "https://localhost/", nil)
	assert.Equal(t, "", clientCommonName(noTLSRequest))
}

func TestEntryAuthorizedAndRenders(t *testing.T) {
	h, err := NewHandler(testRenderer(), []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := invokeHandler(t, h, "ipxe-node-1", "/boot.ipxe")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `#!ipxe
chain https://ipxe.internal:8443/ipxe?mac:hexhyp=${mac:hexhyp}&buildarch:uristring=${buildarch:uristring}
`, rec.Body.String())
}

func TestBootMatchedMAC(t *testing.T) {
	presigner := &fakePresigner{}
	h, err := NewHandler(testRenderer(), []string{"ipxe-node-1"}, testConfig(), presigner)
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := invokeHandler(t, h, "ipxe-node-1", "/ipxe?mac:hexhyp=aa-bb-cc-dd-ee-01&buildarch:uristring=x86_64")

	assert.Equal(t, http.StatusOK, rec.Code)
	expectedPresigned := []string{
		"fcos/vmlinuz-x86_64",
		"fcos/initramfs-x86_64.img",
		"ignition/worker-aa-bb-cc-dd-ee-01.ign",
		"fcos/worker-rootfs-x86_64.img",
	}
	assert.Equal(t, expectedPresigned, presigner.called)
	assert.Equal(t, `#!ipxe
kernel https://minio.internal:9000/presigned/fcos/vmlinuz-x86_64 console=tty0 ignition.firstboot ignition.config.url=https://minio.internal:9000/presigned/ignition/worker-aa-bb-cc-dd-ee-01.ign coreos.live.rootfs_url=https://minio.internal:9000/presigned/fcos/worker-rootfs-x86_64.img
initrd https://minio.internal:9000/presigned/fcos/initramfs-x86_64.img
boot
`, rec.Body.String())
}

func TestBootUnknownMACExits(t *testing.T) {
	h, err := NewHandler(testRenderer(), []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	for _, target := range []string{"/ipxe?mac:hexhyp=11-22-33-44-55-66", "/ipxe?mac:hexhyp=garbage", "/ipxe"} {
		rec := invokeHandler(t, h, "ipxe-node-1", target)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, `#!ipxe
exit
`, rec.Body.String())
	}
}

func TestRejectedCN(t *testing.T) {
	h, err := NewHandler(testRenderer(), []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	for _, target := range []string{"/boot.ipxe", "/ipxe?mac:hexhyp=aa-bb-cc-dd-ee-01&buildarch:uristring=x86_64"} {
		rec := invokeHandler(t, h, "not-allowed", target)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	}
}

func TestBootPresignFailure(t *testing.T) {
	h, err := NewHandler(testRenderer(), []string{"ipxe-node-1"}, testConfig(), &fakePresigner{fail: context.DeadlineExceeded})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := invokeHandler(t, h, "ipxe-node-1", "/ipxe?mac:hexhyp=aa-bb-cc-dd-ee-01&buildarch:uristring=x86_64")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHealthz(t *testing.T) {
	h, err := NewHandler(testRenderer(), []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBuildTLSConfigRequiresClientCert(t *testing.T) {
	tlsDir := t.TempDir()
	if err := tlstest.WriteTestCA(t, tlsDir); err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := tlsutil.BuildTLSConfig(filepath.Join(tlsDir, "server.crt"), filepath.Join(tlsDir, "server.key"), []string{
		filepath.Join(tlsDir, "ca.pem"),
	})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}

	assert.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
	assert.NotNil(t, tlsConfig.ClientCAs)
}

// --- helpers ---

// newTLSRequest builds an http.Request with a fake verified peer
// certificate carrying the given CN, as http.Request.TLS would hold.
func newTLSRequest(t *testing.T, cn string, target string) *http.Request {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://localhost"+target, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	return req
}

// invokeHandler runs the handler directly with a request whose r.TLS
// carries a fake verified peer certificate — how the handler sees a
// completed mTLS handshake without a live TLS server.
func invokeHandler(t *testing.T, h http.Handler, cn, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newTLSRequest(t, cn, target))
	return rec
}
