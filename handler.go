package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// urlPresigner issues short-lived URLs for S3 objects. *Presigner
// satisfies it; tests use a fake.
type urlPresigner interface {
	URL(ctx context.Context, object string) (string, error)
}

// Handler serves the two-stage iPXE flow:
//
//	GET /      first-stage entry script (static template, chains to /pxe)
//	GET /pxe   second-stage boot script, selected by the ?mac= query
//	GET /healthz
//
// Every request must present a client certificate signed by the
// configured CA (enforced at the TLS layer) whose commonName is in the
// allowlist (enforced here). Profile selection is by MAC address only.
type Handler struct {
	advertiseURL string
	allowedCNs   map[string]struct{}
	macToProfile map[string]string
	profiles     map[string]Profile
	presigner    urlPresigner
}

// NewHandler builds a Handler. advertiseURL is the public base URL
// (scheme + host[:port]) that iPXE uses to reach this server.
func NewHandler(advertiseURL string, allowedCNs []string, cfg *Config, presigner urlPresigner) *Handler {
	h := &Handler{
		advertiseURL: strings.TrimRight(advertiseURL, "/"),
		macToProfile: cfg.MACToProfile,
		profiles:     cfg.Profiles,
		presigner:    presigner,
	}
	h.allowedCNs = make(map[string]struct{}, len(allowedCNs))
	for _, cn := range allowedCNs {
		h.allowedCNs[cn] = struct{}{}
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	case "/":
		h.serveEntry(w, r)
	case "/pxe":
		h.serveBoot(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveEntry renders the first-stage script. It carries no node
// identity, so only the CN allowlist applies.
func (h *Handler) serveEntry(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	script, err := RenderEntry(h.advertiseURL + "/pxe")
	if err != nil {
		log.Printf("rendering entry script: %v", err)
		http.Error(w, "failed to render entry script", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, script)
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

	profileName, matched := "", false
	if mac, err := NormalizeMAC(r.URL.Query().Get("mac")); err == nil {
		profileName, matched = h.macToProfile[mac]
	}
	if !matched {
		log.Printf("no profile matches MAC %q, serving exit script", r.URL.Query().Get("mac"))
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, exitScript)
		return
	}

	profile := h.profiles[profileName]

	urls, err := h.presignAll(r.Context(), profile)
	if err != nil {
		log.Printf("presigning resources for profile %q: %v", profileName, err)
		http.Error(w, "failed to generate resource URLs", http.StatusInternalServerError)
		return
	}

	script, err := RenderIPXE(ipxeScript{
		KernelURL:   urls.kernel,
		InitrdURLs:  urls.initrds,
		Kargs:       profile.Kargs,
		IgnitionURL: urls.ignition,
		RootfsURL:   urls.rootfs,
	})
	if err != nil {
		log.Printf("rendering boot script for profile %q: %v", profileName, err)
		http.Error(w, "failed to render boot script", http.StatusInternalServerError)
		return
	}

	log.Printf("served boot script for profile %q (MAC %s)", profileName, r.URL.Query().Get("mac"))
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, script)
}

// presignedSet holds the presigned URLs for one profile's resources.
type presignedSet struct {
	kernel   string
	initrds  []string
	ignition string
	rootfs   string
}

func (h *Handler) presignAll(ctx context.Context, p Profile) (*presignedSet, error) {
	kernel, err := h.presigner.URL(ctx, p.KernelS3Resource)
	if err != nil {
		return nil, err
	}
	initrds := make([]string, 0, len(p.InitrdS3Resources))
	for _, res := range p.InitrdS3Resources {
		u, err := h.presigner.URL(ctx, res)
		if err != nil {
			return nil, err
		}
		initrds = append(initrds, u)
	}
	ignition, err := h.presigner.URL(ctx, p.IgnitionS3Resource)
	if err != nil {
		return nil, err
	}
	rootfs, err := h.presigner.URL(ctx, p.RootfsS3Resource)
	if err != nil {
		return nil, err
	}
	return &presignedSet{kernel: kernel, initrds: initrds, ignition: ignition, rootfs: rootfs}, nil
}

// authorize enforces that the verified client certificate's commonName
// is on the allowlist. It writes the error response and returns false
// if the CN is unknown (including the absence of any peer certificate).
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	cn := clientCommonName(r)
	if _, ok := h.allowedCNs[cn]; !ok {
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
