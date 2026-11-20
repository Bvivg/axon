package mtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

var ErrClientNotAllowed = errors.New("mtls: client identity is not allowed")

type Config struct {
	CertFile string
	KeyFile  string
	CAFile   string
	Allowed  []string
	Reload   time.Duration
}

func FromEnv(l *config.Loader) Config {
	return Config{
		CertFile: l.StringDefault("MTLS_CERT_FILE", ""),
		KeyFile:  l.StringDefault("MTLS_KEY_FILE", ""),
		CAFile:   l.StringDefault("MTLS_CA_FILE", ""),
		Allowed:  l.StringSlice("MTLS_ALLOWED_CLIENTS", nil),
		Reload:   l.DurationDefault("MTLS_RELOAD_INTERVAL", 30*time.Second),
	}
}

func (c Config) Enabled() bool { return c.CertFile != "" || c.KeyFile != "" || c.CAFile != "" }

type material struct {
	cert     *tls.Certificate
	roots    *x509.CertPool
	modified [3]time.Time
}

type Identity struct {
	cfg     Config
	now     func() time.Time
	mu      sync.Mutex
	current material
	checked time.Time
}

func Load(cfg Config) (*Identity, error) {
	if cfg.CertFile == "" || cfg.KeyFile == "" || cfg.CAFile == "" {
		return nil, errors.New("mtls: MTLS_CERT_FILE, MTLS_KEY_FILE and MTLS_CA_FILE must be set together")
	}
	if cfg.Reload <= 0 {
		cfg.Reload = 30 * time.Second
	}
	id := &Identity{cfg: cfg, now: time.Now}
	m, err := id.read()
	if err != nil {
		return nil, err
	}
	id.current, id.checked = m, id.now()
	return id, nil
}

func (id *Identity) read() (material, error) {
	var m material
	for i, path := range []string{id.cfg.CertFile, id.cfg.KeyFile, id.cfg.CAFile} {
		info, err := os.Stat(path)
		if err != nil {
			return material{}, fmt.Errorf("mtls: %w", err)
		}
		m.modified[i] = info.ModTime()
	}
	cert, err := tls.LoadX509KeyPair(id.cfg.CertFile, id.cfg.KeyFile)
	if err != nil {
		return material{}, fmt.Errorf("mtls: load key pair: %w", err)
	}
	pem, err := os.ReadFile(id.cfg.CAFile)
	if err != nil {
		return material{}, fmt.Errorf("mtls: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return material{}, errors.New("mtls: no certificates in MTLS_CA_FILE")
	}
	m.cert, m.roots = &cert, roots
	return m, nil
}

func (id *Identity) material() material {
	id.mu.Lock()
	defer id.mu.Unlock()
	now := id.now()
	if now.Sub(id.checked) < id.cfg.Reload {
		return id.current
	}
	id.checked = now
	var modified [3]time.Time
	for i, path := range []string{id.cfg.CertFile, id.cfg.KeyFile, id.cfg.CAFile} {
		info, err := os.Stat(path)
		if err != nil {
			return id.current
		}
		modified[i] = info.ModTime()
	}
	if modified == id.current.modified {
		return id.current
	}
	if next, err := id.read(); err == nil {
		id.current = next
	}
	return id.current
}

func (id *Identity) Certificate() *x509.Certificate {
	m := id.material()
	if m.cert.Leaf != nil {
		return m.cert.Leaf
	}
	leaf, err := x509.ParseCertificate(m.cert.Certificate[0])
	if err != nil {
		return nil
	}
	return leaf
}

func (id *Identity) ServerConfig() *tls.Config {
	allowed := slices.Clone(id.cfg.Allowed)
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			m := id.material()
			return &tls.Config{
				MinVersion:   tls.VersionTLS13,
				NextProtos:   []string{"h2", "http/1.1"},
				Certificates: []tls.Certificate{*m.cert},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    m.roots,
				VerifyConnection: func(state tls.ConnectionState) error {
					if len(state.PeerCertificates) == 0 {
						return ErrClientNotAllowed
					}
					return Authorize(state.PeerCertificates[0], allowed)
				},
			}, nil
		},
	}
}

func Authorize(peer *x509.Certificate, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	names := append([]string{peer.Subject.CommonName}, peer.DNSNames...)
	for _, name := range names {
		if slices.Contains(allowed, strings.ToLower(name)) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrClientNotAllowed, peer.Subject.CommonName)
}

func (id *Identity) clientConfig(serverName string) *tls.Config {
	m := id.material()
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2", "http/1.1"},
		Certificates: []tls.Certificate{*m.cert},
		RootCAs:      m.roots,
		ServerName:   serverName,
	}
}

type TransportOption func(*transportOptions)

type transportOptions struct {
	serverName string
	http1Only  bool
}

func ServerName(name string) TransportOption {
	return func(o *transportOptions) { o.serverName = name }
}

func HTTP1Only() TransportOption {
	return func(o *transportOptions) { o.http1Only = true }
}

func (id *Identity) Transport(options ...TransportOption) *http.Transport {
	var o transportOptions
	for _, option := range options {
		option(&o)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = !o.http1Only
	transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		name := host
		if o.serverName != "" {
			name = o.serverName
		}
		cfg := id.clientConfig(name)
		if o.http1Only {
			cfg.NextProtos = []string{"http/1.1"}
		}
		raw, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, cfg)
		if err := conn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}
	return transport
}

func Serve(srv *http.Server, id *Identity) error {
	if id == nil {
		return srv.ListenAndServe()
	}
	srv.TLSConfig = id.ServerConfig()
	return srv.ListenAndServeTLS("", "")
}
