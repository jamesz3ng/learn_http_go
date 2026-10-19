package gocurl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"
)

// Exit codes, borrowed from curl so scripts can tell failures apart.
const (
	exitGeneric             = 1
	exitUsage               = 2
	exitUnsupportedProtocol = 1
	exitURLMalformed        = 3
	exitResolve             = 6
	exitConnect             = 7
	exitHTTPError           = 22
	exitWriteError          = 23
	exitReadError           = 26
	exitTimeout             = 28
	exitSSLConnect          = 35
	exitTooManyRedirects    = 47
	exitRecv                = 56
	exitSSLCert             = 60
	exitCACert              = 77
	exitInterrupted         = 130
)

// curlError carries an exit code and a message.
type curlError struct {
	code int
	msg  string
}

func (e *curlError) Error() string { return e.msg }

var errTooManyRedirects = errors.New("too many redirects")

// newClient builds an http.Client configured from the options. hops
// collects the head of every intermediate redirect response for -i -L.
func newClient(o *options, hops *strings.Builder) (*http.Client, error) {
	// Start from a clone of DefaultTransport: it has sensible timeouts,
	// honours HTTP(S)_PROXY, and has ForceAttemptHTTP2 set.
	tr := http.DefaultTransport.(*http.Transport).Clone()

	// Stage 18: --connect-timeout limits only the TCP connect.
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if o.connectTimeout > 0 {
		dialer.Timeout = seconds(o.connectTimeout)
	}
	tr.DialContext = dialer.DialContext

	// Stage 24: TLS options.
	if o.insecure || o.cacert != "" {
		conf := &tls.Config{}
		if o.insecure {
			conf.InsecureSkipVerify = true
		}
		if o.cacert != "" {
			pem, err := os.ReadFile(o.cacert)
			if err != nil {
				return nil, &curlError{exitCACert, "error reading CA cert: " + err.Error()}
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, &curlError{exitCACert, "no certificates found in " + o.cacert}
			}
			conf.RootCAs = pool
		}
		// Setting TLSClientConfig would normally switch off automatic HTTP/2.
		// ForceAttemptHTTP2 (already true in the clone) turns it back on.
		tr.TLSClientConfig = conf
		tr.ForceAttemptHTTP2 = true
	}
	if o.http11 {
		// A non-nil, empty TLSNextProto map disables HTTP/2.
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}

	// Stage 14: a cookie jar, so cookies set during a redirect chain are
	// sent on the following requests (curl calls it the "cookie engine").
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: tr,
		Jar:       jar,
		// Stage 12: redirect policy.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !o.location {
				// Return the 3xx response itself instead of following it.
				return http.ErrUseLastResponse
			}
			if o.maxRedirs >= 0 && len(via) > o.maxRedirs {
				return errTooManyRedirects
			}
			// req.Response is the redirect response that led to req.
			if (o.include || o.head) && req.Response != nil {
				var sb strings.Builder
				writeHead(&sb, req.Response)
				hops.WriteString(sb.String())
			}
			return nil
		},
	}, nil
}

// classify maps a client error to curl's exit code and message.
func classify(err error) error {
	var (
		netErr   net.Error
		dnsErr   *net.DNSError
		opErr    *net.OpError
		certErr  *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostErr  x509.HostnameError
		invalid  x509.CertificateInvalidError
		recErr   tls.RecordHeaderError
		alertErr tls.AlertError
	)
	switch {
	case errors.Is(err, errTooManyRedirects):
		return &curlError{exitTooManyRedirects, "Maximum redirects followed"}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return &curlError{exitTimeout, "Operation timed out: " + err.Error()}
	case errors.Is(err, context.Canceled):
		return &curlError{exitInterrupted, "Interrupted"}
	case errors.As(err, &certErr), errors.As(err, &unknown), errors.As(err, &hostErr), errors.As(err, &invalid):
		return &curlError{exitSSLCert, "SSL certificate problem: " + err.Error()}
	case errors.As(err, &recErr), errors.As(err, &alertErr):
		return &curlError{exitSSLConnect, "SSL connect error: " + err.Error()}
	case errors.As(err, &dnsErr):
		return &curlError{exitResolve, "Could not resolve host: " + err.Error()}
	case errors.As(err, &opErr) && opErr.Op == "dial":
		return &curlError{exitConnect, "Failed to connect: " + err.Error()}
	default:
		return &curlError{exitRecv, fmt.Sprintf("Failure receiving data: %v", err)}
	}
}
