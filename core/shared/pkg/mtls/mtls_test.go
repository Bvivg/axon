package mtls_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/mtls"
)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newAuthority(t *testing.T, name string) authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return authority{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

var serial int64 = 10

func (a authority) issue(t *testing.T, dir, name string) mtls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial++
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	cfg := mtls.Config{
		CertFile: filepath.Join(dir, name+".crt"),
		KeyFile:  filepath.Join(dir, name+".key"),
		CAFile:   filepath.Join(dir, "ca-"+name+".crt"),
	}
	write(t, cfg.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	write(t, cfg.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	write(t, cfg.CAFile, a.pem)
	return cfg
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func server(t *testing.T, cfg mtls.Config) *httptest.Server {
	t.Helper()
	id, err := mtls.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.TLS.PeerCertificates[0].Subject.CommonName + " " + r.Proto))
	}))
	srv.TLS = id.ServerConfig()
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, transport http.RoundTripper, url string) (string, error) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	res, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	body := make([]byte, 64)
	n, _ := res.Body.Read(body)
	return string(body[:n]), nil
}

func TestOnlyAllowedClientsOfTheSameAuthorityGetThrough(t *testing.T) {
	dir := t.TempDir()
	ca := newAuthority(t, "axon-ca")
	serverCfg := ca.issue(t, dir, "auth")
	serverCfg.Allowed = []string{"gateway", "chat"}
	srv := server(t, serverCfg)
	url := "https://" + srv.Listener.Addr().String()

	client := func(cfg mtls.Config) http.RoundTripper {
		id, err := mtls.Load(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return id.Transport(mtls.ServerName("auth"))
	}
	if body, err := call(t, client(ca.issue(t, dir, "gateway")), url); err != nil || body != "gateway HTTP/2.0" {
		t.Fatal("allowed client over h2", body, err)
	}
	if _, err := call(t, client(ca.issue(t, dir, "intruder")), url); err == nil {
		t.Fatal("a client outside the allow-list got through")
	}
	rogue := newAuthority(t, "rogue-ca")
	stolen := rogue.issue(t, dir, "gateway")
	stolen.CAFile = serverCfg.CAFile
	if _, err := call(t, client(stolen), url); err == nil {
		t.Fatal("a certificate from another authority got through")
	}
	plain := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool(ca), ServerName: "auth"}}
	if _, err := call(t, plain, url); err == nil {
		t.Fatal("a client without a certificate got through")
	}
	wrongName := ca.issue(t, dir, "chat")
	id, _ := mtls.Load(wrongName)
	if _, err := call(t, id.Transport(mtls.ServerName("game")), url); err == nil {
		t.Fatal("client accepted a server certificate issued for another name")
	}
}

func pool(a authority) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(a.cert)
	return p
}

func TestRenewedCertificateIsPickedUpWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	ca := newAuthority(t, "axon-ca")
	cfg := ca.issue(t, dir, "game")
	cfg.Reload = time.Millisecond
	srv := server(t, cfg)
	clientCfg := ca.issue(t, dir, "gateway")
	pair, err := tls.LoadX509KeyPair(clientCfg.CertFile, clientCfg.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	serials := map[string]bool{}
	for range 2 {
		raw, err := (&tls.Dialer{Config: &tls.Config{
			MinVersion: tls.VersionTLS13, RootCAs: pool(ca), ServerName: "game", Certificates: []tls.Certificate{pair},
		}}).DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conn, ok := raw.(*tls.Conn)
		if !ok {
			t.Fatalf("dialer returned %T", raw)
		}
		serials[conn.ConnectionState().PeerCertificates[0].SerialNumber.String()] = true
		_ = conn.Close()
		ca.issue(t, dir, "game")
		future := time.Now().Add(time.Second)
		_ = os.Chtimes(cfg.CertFile, future, future)
		_ = os.Chtimes(cfg.KeyFile, future, future)
		time.Sleep(5 * time.Millisecond)
	}
	if len(serials) != 2 {
		t.Fatal("server kept presenting the old certificate", serials)
	}
}

func TestLoadRequiresAllFiles(t *testing.T) {
	if _, err := mtls.Load(mtls.Config{CertFile: "a"}); err == nil {
		t.Fatal("partial configuration accepted")
	}
	if (mtls.Config{}).Enabled() {
		t.Fatal("empty configuration must mean plain HTTP")
	}
}

func TestAuthorizeMatchesCommonNameOrSAN(t *testing.T) {
	peer := &x509.Certificate{Subject: pkix.Name{CommonName: "gateway"}, DNSNames: []string{"gateway.axon.internal"}}
	cases := []struct {
		allowed []string
		ok      bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"gateway"}, true},
		{[]string{"gateway.axon.internal"}, true},
		{[]string{"chat", "game"}, false},
	}
	for _, tc := range cases {
		err := mtls.Authorize(peer, tc.allowed)
		if (err == nil) != tc.ok || (err != nil && !errors.Is(err, mtls.ErrClientNotAllowed)) {
			t.Fatal(tc.allowed, err)
		}
	}
}
