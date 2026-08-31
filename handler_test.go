package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

func testConfig() *Config {
	return &Config{
		Profiles: map[string]Profile{
			"worker-profile": {
				KernelS3Resource:   "fcos/vmlinuz",
				InitrdS3Resources:  []string{"fcos/initramfs.img"},
				IgnitionS3Resource: "ignition/worker.ign",
				RootfsS3Resource:   "fcos/worker-rootfs.img",
				Kargs:              []string{"console=tty0", "ignition.firstboot"},
			},
		},
		MACToProfile: map[string]string{
			"aa:bb:cc:dd:ee:01": "worker-profile",
		},
	}
}

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

func TestClientCommonName(t *testing.T) {
	if cn := clientCommonName(newTLSRequest(t, "ipxe-node-1", "/")); cn != "ipxe-node-1" {
		t.Fatalf("clientCommonName = %q, want %q", cn, "ipxe-node-1")
	}
	noTLS, _ := http.NewRequest(http.MethodGet, "https://localhost/", nil)
	if cn := clientCommonName(noTLS); cn != "" {
		t.Fatalf("clientCommonName without TLS = %q, want empty", cn)
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"aa:bb:cc:dd:ee:ff": "aa:bb:cc:dd:ee:ff", // canonical (iPXE ${mac:hexhyp})
		"AA:BB:CC:DD:EE:FF": "aa:bb:cc:dd:ee:ff", // uppercase
		"aa-bb-cc-dd-ee-ff": "aa:bb:cc:dd:ee:ff", // dashed
		"aabb.ccdd.eeff":    "aa:bb:cc:dd:ee:ff", // dotted
		"aabbccddeeFF":      "aa:bb:cc:dd:ee:ff", // bare
		" aabbccddee01 ":    "aa:bb:cc:dd:ee:01", // padded + mixed case
	}
	for in, want := range cases {
		got, err := NormalizeMAC(in)
		if err != nil {
			t.Errorf("NormalizeMAC(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "aabb", "aabbccddee0", "aabbccddee012", "zz:bb:cc:dd:ee:ff"} {
		if _, err := NormalizeMAC(bad); err == nil {
			t.Errorf("NormalizeMAC(%q) should fail", bad)
		}
	}
}

func TestRenderIPXE(t *testing.T) {
	script, err := RenderIPXE(ipxeScript{
		KernelURL:   "https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k",
		InitrdURLs:  []string{"https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i"},
		Kargs:       []string{"console=tty0", "ignition.firstboot"},
		IgnitionURL: "https://minio.internal:9000/presigned/ignition/worker.ign?sig=g",
		RootfsURL:   "https://minio.internal:9000/presigned/fcos/worker-rootfs.img?sig=r",
	})
	if err != nil {
		t.Fatalf("RenderIPXE: %v", err)
	}
	for _, want := range []string{
		"#!ipxe",
		"kernel https://minio.internal:9000/presigned/fcos/vmlinuz?sig=k console=tty0 ignition.firstboot ignition.url=",
		"initrd https://minio.internal:9000/presigned/fcos/initramfs.img?sig=i",
		"\nboot\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("rendered script missing %q\n---\n%s", want, script)
		}
	}
}

func TestRenderEntry(t *testing.T) {
	script, err := RenderEntry("https://ipxe.internal:8443/pxe")
	if err != nil {
		t.Fatalf("RenderEntry: %v", err)
	}
	want := "#!ipxe\nchain https://ipxe.internal:8443/pxe?mac=${mac:hexhyp}&uuid=${uuid}\n"
	if script != want {
		t.Fatalf("entry script = %q, want %q", script, want)
	}
}

func TestEntryAuthorizedAndRenders(t *testing.T) {
	h := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	rec := invokeHandler(t, h, "ipxe-node-1", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "chain https://ipxe.internal:8443/pxe?mac=${mac:hexhyp}&uuid=${uuid}") {
		t.Fatalf("body = %q, want chain to /pxe", rec.Body.String())
	}
}

func TestBootMatchedMAC(t *testing.T) {
	presigner := &fakePresigner{}
	h := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), presigner)

	rec := invokeHandler(t, h, "ipxe-node-1", "/pxe?mac=AA-BB-CC-DD-EE-01&uuid=deadbeef")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	wantPresigned := []string{
		"fcos/vmlinuz",
		"fcos/initramfs.img",
		"ignition/worker.ign",
		"fcos/worker-rootfs.img",
	}
	if strings.Join(presigner.called, ",") != strings.Join(wantPresigned, ",") {
		t.Errorf("presigned objects = %v, want %v", presigner.called, wantPresigned)
	}
	for _, want := range []string{
		"#!ipxe",
		"kernel https://minio.internal:9000/presigned/fcos/vmlinuz",
		"initrd https://minio.internal:9000/presigned/fcos/initramfs.img",
		"ignition.url=https://minio.internal:9000/presigned/ignition/worker.ign",
		"rootfs.url=https://minio.internal:9000/presigned/fcos/worker-rootfs.img",
		"boot",
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body missing %q\n---\n%s", want, rec.Body.String())
		}
	}
}

func TestBootUnknownMACExits(t *testing.T) {
	presigner := &fakePresigner{}
	h := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), presigner)

	for _, target := range []string{"/pxe?mac=11:22:33:44:55:66", "/pxe?mac=garbage", "/pxe"} {
		rec := invokeHandler(t, h, "ipxe-node-1", target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, rec.Code)
		}
		if rec.Body.String() != exitScript {
			t.Fatalf("%s: body = %q, want exit script", target, rec.Body.String())
		}
	}
	if len(presigner.called) != 0 {
		t.Fatalf("presigner called for unmatched MAC: %v", presigner.called)
	}
}

func TestRejectedCN(t *testing.T) {
	h := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	for _, target := range []string{"/", "/pxe?mac=aa:bb:cc:dd:ee:01"} {
		rec := invokeHandler(t, h, "not-allowed", target)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403", target, rec.Code)
		}
	}
}

func TestBootPresignFailure(t *testing.T) {
	presigner := &fakePresigner{fail: context.DeadlineExceeded}
	h := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), presigner)
	rec := invokeHandler(t, h, "ipxe-node-1", "/pxe?mac=aa:bb:cc:dd:ee:01")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	h := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLoadConfig(t *testing.T) {
	path := t.TempDir() + "/profiles.yaml"

	valid := `profiles:
  worker-profile:
    kernel_s3_resource: "fcos/vmlinuz"
    initrd_s3_resources:
      - "fcos/initramfs.img"
    ignition_s3_resource: "ignition/worker.ign"
    rootfs_s3_resource: "fcos/worker-rootfs.img"
    kargs:
      - "console=tty0"
groups:
  worker-profile:
    - "AA:BB:CC:DD:EE:01"
    - "0a:1b:2c:3d:4e:5f"
`
	// Group referencing an unknown profile must be rejected.
	if err := os.WriteFile(path, []byte(strings.Replace(valid, "  worker-profile:\n    - \"AA:BB", "  control-plane:\n    - \"AA:BB", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for group referencing unknown profile")
	}

	// Duplicate MAC across groups must be rejected.
	dup := `profiles:
  a:
    kernel_s3_resource: k
    initrd_s3_resources: [i]
    ignition_s3_resource: g
    rootfs_s3_resource: r
  b:
    kernel_s3_resource: k
    initrd_s3_resources: [i]
    ignition_s3_resource: g
    rootfs_s3_resource: r
groups:
  a:
    - "aa:bb:cc:dd:ee:01"
  b:
    - "aabbccddee01"
`
	if err := os.WriteFile(path, []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for MAC assigned to multiple groups")
	}

	// Happy path.
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.MACToProfile["aa:bb:cc:dd:ee:01"]; got != "worker-profile" {
		t.Errorf("MAC aa:bb:cc:dd:ee:01 -> %q, want worker-profile", got)
	}
	if got := cfg.MACToProfile["0a:1b:2c:3d:4e:5f"]; got != "worker-profile" {
		t.Errorf("MAC 0a:1b:2c:3d:4e:5f -> %q, want worker-profile", got)
	}
	p := cfg.Profiles["worker-profile"]
	if p.KernelS3Resource == "" || len(p.InitrdS3Resources) != 1 || p.IgnitionS3Resource == "" || p.RootfsS3Resource == "" {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestProfileValidate(t *testing.T) {
	if err := (Profile{}).Validate(); err == nil {
		t.Fatal("expected error for empty profile")
	}
	good := Profile{
		KernelS3Resource:   "k",
		InitrdS3Resources:  []string{"i"},
		IgnitionS3Resource: "g",
		RootfsS3Resource:   "r",
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good profile rejected: %v", err)
	}
}

func TestBuildTLSConfigRequiresClientCert(t *testing.T) {
	dir := t.TempDir()
	if err := writeTestCA(t, dir); err != nil {
		t.Fatal(err)
	}

	cfg, err := buildTLSConfig(dir+"/server.crt", dir+"/server.key", dir+"/ca.pem")
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil || len(cfg.ClientCAs.Subjects()) != 1 {
		t.Fatal("ClientCAs pool is empty or has wrong count")
	}
}

// --- helpers ---

func writeTestCA(t *testing.T, dir string) error {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ipxe-presign-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serverTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ipxe-presign.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"ipxe-presign.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTmpl, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return err
	}

	files := map[string][]byte{
		"ca.pem":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		"server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		"server.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}),
	}
	for name, data := range files {
		if err := os.WriteFile(dir+"/"+name, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
