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

// NewHandler builds the HTTP handler: any GET renders the boot script
// for the client certificate's commonName; /healthz is a probe target.
func NewHandler(profiles map[string]Profile, presigner urlPresigner) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", handleBoot(profiles, presigner))
	return mux
}

func handleBoot(profiles map[string]Profile, presigner urlPresigner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cn := clientCommonName(r)
		profile, ok := profiles[cn]
		if !ok {
			log.Printf("no boot profile matches client CN %q, serving exit script", cn)
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, exitScript)
			return
		}

		ignitionURL, err := presigner.URL(r.Context(), profile.IgnitionS3Resource)
		if err != nil {
			log.Printf("presigning ignition resource for CN %q: %v", cn, err)
			http.Error(w, "failed to generate ignition URL", http.StatusInternalServerError)
			return
		}
		rootfsURL, err := presigner.URL(r.Context(), profile.RootfsS3Resource)
		if err != nil {
			log.Printf("presigning rootfs resource for CN %q: %v", cn, err)
			http.Error(w, "failed to generate rootfs URL", http.StatusInternalServerError)
			return
		}

		script, err := RenderIPXE(ipxeScript{
			KernelURL:   profile.KernelURL,
			InitrdURLs:  profile.InitrdURLs,
			Kargs:       profile.Kargs,
			IgnitionURL: ignitionURL,
			RootfsURL:   rootfsURL,
		})
		if err != nil {
			log.Printf("rendering script for CN %q: %v", cn, err)
			http.Error(w, "failed to render boot script", http.StatusInternalServerError)
			return
		}

		log.Printf("served boot script for CN %q", cn)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, script)
	}
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
