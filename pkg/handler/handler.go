package handler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"

	"github.com/randomcoww/ipxe-presign/config"
)

// urlPresigner issues short-lived URLs for S3 objects. *Presigner
// satisfies it; tests use a fake.
type URLPresigner interface {
	URL(ctx context.Context, object string) (string, error)
}

// Handler serves the two-stage iPXE flow:
//
//	GET /boot.ipxe first-stage entry script (static template, chains to /ipxe)
//	GET /ipxe      second-stage boot script, selected by the ?mac= query
//	GET /healthz
//
// Every request must present a client certificate signed by the
// configured CA (enforced at the TLS layer) whose commonName is in the
// allowlist (enforced here). Profile selection is by MAC address only.
type Handler struct {
	advertiseURL     string
	Profiles         *config.Profiles
	Presigner        URLPresigner
	allowedClientCNs map[string]struct{}
}

// NewHandler builds a Handler. advertiseURL is the public base URL
// (scheme + host[:port]) that iPXE uses to reach this server.
func NewHandler(advertiseURL string, allowedClientCNs []string, p *config.Profiles, pr URLPresigner) (*Handler, error) {
	h := &Handler{
		advertiseURL:     advertiseURL,
		Profiles:         p,
		Presigner:        pr,
		allowedClientCNs: make(map[string]struct{}, len(allowedClientCNs)),
	}
	for _, cn := range allowedClientCNs {
		h.allowedClientCNs[cn] = struct{}{}
	}
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	case "/boot.ipxe":
		h.serveChain(w, r)
	case "/ipxe":
		h.serveBoot(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveEntry renders the first-stage script. It carries no node
// identity, so only the CN allowlist applies.
func (h *Handler) serveChain(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// First-stage script. iPXE substitutes ${mac:hexhyp}
	// and ${uuid} at parse time, then chains to the second-stage URL with
	// the node's MAC address — which is what selects the boot profile.
	_, _ = fmt.Fprint(w, fmt.Sprintf(`#!ipxe
chain %s/ipxe?mac:hexhyp=${mac:hexhyp}&buildarch:uristring=${buildarch:uristring}&uuid=${uuid}
`, h.advertiseURL))
}

// serveBoot selects the profile by MAC address and renders the
// second-stage script with presigned resource URLs.
func (h *Handler) serveBoot(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	queryKV := make(map[string]string)
	for k, v := range r.URL.Query() {
		queryKV[k] = v[len(v)-1]
	}

	profile, ok := h.Profiles.GetMerged(queryKV)
	if !ok {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `#!ipxe
exit
`)
		return
	}

	script, err := h.renderIPXEBoot(r.Context(), profile, queryKV)
	if err != nil {
		log.Printf("presigning resources for profile: %v", err)
		http.Error(w, "failed to generate resource URLs", http.StatusInternalServerError)
		return
	}

	log.Printf("served boot script for profile (Selector %v)", queryKV)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, script)
}

func (h *Handler) renderIPXEBoot(ctx context.Context, p *config.Profile, queryKV map[string]string) (string, error) {
	var err error
	for k, v := range queryKV {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	tmpl := template.New("presigner").Funcs(template.FuncMap{
		"presign": func(resource string) (string, error) {
			url, err := h.Presigner.URL(ctx, resource)
			if err != nil {
				return "", fmt.Errorf("presign URL: %w", err)
			}
			return url, nil
		},
	})

	kernelURL, err := h.renderParam(tmpl, os.ExpandEnv(p.KernelURL))
	if err != nil {
		return "", fmt.Errorf("parse kernel URL: %w", err)
	}
	kargs := []string{}
	for _, karg := range p.Kargs {
		k, err := h.renderParam(tmpl, os.ExpandEnv(karg))
		if err != nil {
			return "", fmt.Errorf("parse karg: %w", err)
		}
		kargs = append(kargs, k)
	}
	initrdURLs := []string{}
	for _, initrdURL := range p.InitrdURLs {
		i, err := h.renderParam(tmpl, os.ExpandEnv(initrdURL))
		if err != nil {
			return "", fmt.Errorf("parse initrd URL: %w", err)
		}
		initrdURLs = append(initrdURLs, i)
	}

	// Second-stage boot script. The presigned kernel,
	// initrd, ignition and rootfs URLs are substituted in; ignition and
	// rootfs are passed as kernel arguments.
	ipxe := fmt.Sprintf(`#!ipxe
kernel %s %s
initrd %s
boot
`, kernelURL, strings.Join(kargs, " "), strings.Join(initrdURLs, " "))

	return ipxe, nil
}

func (h *Handler) renderParam(tmpl *template.Template, param string) (string, error) {
	t, err := tmpl.Parse(param)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", param, err)
	}
	b := bytes.Buffer{}
	if err := t.Execute(&b, struct{}{}); err != nil {
		return "", fmt.Errorf("render %s: %w", param, err)
	}
	return b.String(), nil
}

// authorize enforces that the verified client certificate's commonName
// is on the allowlist. It writes the error response and returns false
// if the CN is unknown (including the absence of any peer certificate).
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	cn := clientCommonName(r)
	if _, ok := h.allowedClientCNs[cn]; !ok {
		log.Printf("rejected client with CN %q: not an allowed iPXE client", cn)
		http.Error(w, "unauthorized client", http.StatusForbidden)
		return false
	}
	return true
}

// clientCommonName extracts the commonName from the verified client
// certificate. It is empty when no peer certificate is present, which
// never happens under RequireAndVerifyClientCert (the handshake fails
// first) but keeps the function safe for tests.
func clientCommonName(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return strings.TrimSpace(r.TLS.PeerCertificates[0].Subject.CommonName)
}
