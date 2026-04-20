// Package tls wraps caddyserver/certmagic to provide automatic HTTPS
// with HTTP-01 challenge for a mutable set of domains.
package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/caddyserver/certmagic"
)

// Config configures the certificate manager.
type Config struct {
	// StorageDir is the directory certmagic uses for cert/key/account files.
	StorageDir string
	// Email is the ACME account email.
	Email string
	// CADirURL overrides the ACME directory URL. Leave empty for Let's Encrypt prod.
	CADirURL string
	// Staging uses the Let's Encrypt staging endpoint when CADirURL is empty.
	Staging bool
}

// CertManager wraps certmagic.Config.
type CertManager struct {
	mu      sync.Mutex
	magic   *certmagic.Config
	issuer  *certmagic.ACMEIssuer
	domains map[string]struct{}
}

// New builds a CertManager with HTTP-01 challenge handling.
func New(c Config) (*CertManager, error) {
	if c.StorageDir == "" {
		return nil, fmt.Errorf("StorageDir is required")
	}
	storage := &certmagic.FileStorage{Path: c.StorageDir}

	// Two-step init so renewal uses the very config we built below (with
	// our CA / staging settings) instead of certmagic.NewDefault(), which
	// would silently redirect renewals to Let's Encrypt production.
	var mCfg *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
			return mCfg, nil
		},
	})
	mCfg = certmagic.New(cache, certmagic.Config{Storage: storage})

	ca := c.CADirURL
	if ca == "" {
		ca = certmagic.LetsEncryptProductionCA
		if c.Staging {
			ca = certmagic.LetsEncryptStagingCA
		}
	}
	issuer := certmagic.NewACMEIssuer(mCfg, certmagic.ACMEIssuer{
		CA:                      ca,
		Email:                   c.Email,
		Agreed:                  true,
		DisableTLSALPNChallenge: true,
	})
	mCfg.Issuers = []certmagic.Issuer{issuer}

	return &CertManager{
		magic:   mCfg,
		issuer:  issuer,
		domains: make(map[string]struct{}),
	}, nil
}

// ManageDomains ensures certmagic is managing (issuing/renewing) exactly
// the given set of domains. Callers must pass the UNION of domains across
// all services — a call with a subset will stop renewing everything else.
// Removed domains stop being renewed but files are not deleted (they
// expire naturally — safer than aggressive deletion).
func (c *CertManager) ManageDomains(domains []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	next := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		next[d] = struct{}{}
	}

	var toAdd []string
	for d := range next {
		if _, ok := c.domains[d]; !ok {
			toAdd = append(toAdd, d)
		}
	}
	if len(toAdd) > 0 {
		if err := c.magic.ManageAsync(context.Background(), toAdd); err != nil {
			return fmt.Errorf("certmagic manage: %w", err)
		}
	}
	c.domains = next
	return nil
}

// TLSConfig returns a *tls.Config suitable for http.Server.
func (c *CertManager) TLSConfig() *tls.Config {
	return c.magic.TLSConfig()
}

// HTTPChallengeHandler returns an http.Handler that answers ACME HTTP-01
// challenges and falls through to fallback for all other requests.
// Install this on the :80 listener.
func (c *CertManager) HTTPChallengeHandler(fallback http.Handler) http.Handler {
	return c.issuer.HTTPChallengeHandler(fallback)
}
