package tests

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

// selfSignedCert writes a self-signed certificate for 127.0.0.1/localhost
// and its key to dir, returning their paths and a pool that trusts it.
func selfSignedCert(t *testing.T, dir string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "learn_http_go test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	cert, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

func TestStage24_ServerSpeaksHTTPS(t *testing.T) {
	certFile, keyFile, pool := selfSignedCert(t, t.TempDir())
	rs := startRun(t, gobin.Config{TLSCertFile: certFile, TLSKeyFile: keyFile}, "https")

	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := c.Get(rs.url + "/get")
	if err != nil {
		t.Fatalf("HTTPS request to Run with TLSCertFile/TLSKeyFile failed: %v%s", err,
			hint("srv.ServeTLS(ln, cfg.TLSCertFile, cfg.TLSKeyFile)"))
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	wantStatus(t, resp, 200, "GET https://.../get")
	if resp.ProtoMajor != 2 {
		t.Fatalf("over TLS the server negotiated %s, want HTTP/2%s", resp.Proto,
			hint("ServeTLS enables HTTP/2 automatically, unless TLSNextProto is set to a non-nil map"))
	}
	if !strings.HasPrefix(decodeObject(t, string(body))["url"].(string), "https://") {
		t.Fatalf("/get should report an https:// url over TLS, got %s", body)
	}

	// An HTTP/1.1-only client must work too.
	tr1 := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	defer tr1.CloseIdleConnections()
	resp, err = (&http.Client{Transport: tr1, Timeout: 5 * time.Second}).Get(rs.url + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.ProtoMajor != 1 {
		t.Fatalf("a client that only offers HTTP/1.1 got %s", resp.Proto)
	}
}

// tlsTestServer is an HTTPS test server that speaks HTTP/2, plus its
// certificate as a PEM file.
func tlsTestServer(t *testing.T) (*httptest.Server, string) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "secure hello")
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pemFile := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(pemFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o644)
	return srv, pemFile
}

func TestStage24_ClientRejectsUnknownCA(t *testing.T) {
	srv, _ := tlsTestServer(t)
	res := curl(t, srv.URL)
	res.wantCode(t, 60, "gocurl to a server with a self-signed certificate",
		"errors.As(err, new(*tls.CertificateVerificationError)) identifies certificate problems")
}

func TestStage24_Insecure(t *testing.T) {
	srv, _ := tlsTestServer(t)
	for _, flag := range []string{"-k", "--insecure"} {
		res := curl(t, flag, "-i", srv.URL)
		res.wantCode(t, 0, "gocurl "+flag)
		if !strings.HasPrefix(res.Stdout, "HTTP/2.0 200") {
			t.Fatalf("gocurl %s -i first line = %q, want HTTP/2.0 200%s", flag, strings.SplitN(res.Stdout, "\n", 2)[0],
				hint("setting Transport.TLSClientConfig turns off automatic HTTP/2 unless ForceAttemptHTTP2 is true (cloning http.DefaultTransport keeps it on)"))
		}
	}
}

func TestStage24_CACert(t *testing.T) {
	srv, pemFile := tlsTestServer(t)
	res := curl(t, "--cacert", pemFile, "-i", srv.URL)
	res.wantCode(t, 0, "gocurl --cacert", "x509.NewCertPool + AppendCertsFromPEM, then tls.Config{RootCAs: pool}")
	if !strings.HasPrefix(res.Stdout, "HTTP/2.0 200") || !strings.HasSuffix(res.Stdout, "secure hello") {
		t.Fatalf("gocurl --cacert -i output = %q", res.Stdout)
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	os.WriteFile(empty, []byte("not a certificate"), 0o644)
	curl(t, "--cacert", empty, srv.URL).wantCode(t, 77, "gocurl --cacert with a file containing no certificates")
	curl(t, "--cacert", filepath.Join(t.TempDir(), "missing.pem"), srv.URL).wantCode(t, 77, "gocurl --cacert with a missing file")
}

func TestStage24_HTTP11(t *testing.T) {
	srv, _ := tlsTestServer(t)
	res := curl(t, "--http1.1", "-k", "-i", srv.URL)
	res.wantCode(t, 0, "gocurl --http1.1 -k")
	if !strings.HasPrefix(res.Stdout, "HTTP/1.1 200") {
		t.Fatalf("--http1.1 first line = %q, want HTTP/1.1 200%s", strings.SplitN(res.Stdout, "\n", 2)[0],
			hint("a non-nil, empty Transport.TLSNextProto map disables HTTP/2"))
	}
}
