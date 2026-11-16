package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

func TestMutualTLSHandshake(t *testing.T) {
	t.Parallel()
	fixtures := newTestCertificates(t)
	for _, tc := range []struct {
		name    string
		cert    *tls.Certificate
		allowed bool
	}{
		{"trusted", &fixtures.client, true},
		{"no certificate", nil, false},
		{"untrusted", &fixtures.untrusted, false},
		{"serverAuth only", &fixtures.server, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var handled atomic.Int32
			server := httptest.NewUnstartedServer(authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handled.Add(1)
				allow, err := (readAuthorizer{}).Authorize(r.Context(), httpapi.ConsumerRoutes()[0])
				require.NoError(t, err)
				require.True(t, allow)
				w.WriteHeader(http.StatusNoContent)
			})))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{
				MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{fixtures.server},
				ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: fixtures.roots,
			}
			server.StartTLS()
			t.Cleanup(server.Close)
			// Verify the server's localhost DNS SAN, not httptest's IP.
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			url := "https://" + net.JoinHostPort("localhost", port)
			client := fixtures.httpClient(t, tc.cert)
			resp, err := client.Get(url)
			if !tc.allowed {
				require.Error(t, err)
				require.Equal(t, int32(0), handled.Load(), "rejected TLS must not reach HTTP")
				return
			}
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusNoContent, resp.StatusCode)
			require.Equal(t, int32(1), handled.Load())
		})
	}
}

type testCertificates struct {
	server, client, untrusted tls.Certificate
	roots                     *x509.CertPool
	certFile, keyFile, caFile string
}

func newTestCertificates(t *testing.T) testCertificates {
	t.Helper()
	ca, caKey, caPEM, _ := issueTestCertificate(t, nil, nil, true, nil)
	_, _, certPEM, keyPEM := issueTestCertificate(t, ca, caKey, false, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	server, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	_, _, clientPEM, clientKeyPEM := issueTestCertificate(t, ca, caKey, false, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	client, err := tls.X509KeyPair(clientPEM, clientKeyPEM)
	require.NoError(t, err)
	otherCA, otherKey, _, _ := issueTestCertificate(t, nil, nil, true, nil)
	_, _, otherPEM, otherKeyPEM := issueTestCertificate(t, otherCA, otherKey, false, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	untrusted, err := tls.X509KeyPair(otherPEM, otherKeyPEM)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))
	dir := t.TempDir()
	certFile, keyFile, caFile := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem"), filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0600))
	require.NoError(t, os.WriteFile(caFile, caPEM, 0600))
	return testCertificates{server, client, untrusted, roots, certFile, keyFile, caFile}
}

func issueTestCertificate(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, isCA bool, eku []x509.ExtKeyUsage) (*x509.Certificate, *ecdsa.PrivateKey, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	cert := &x509.Certificate{
		SerialNumber: serial, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		BasicConstraintsValid: true, IsCA: isCA, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: eku,
		DNSNames: []string{"localhost"},
	}
	if isCA {
		cert.KeyUsage |= x509.KeyUsageCertSign
		parent, parentKey = cert, key
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, parentKey)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return parsed, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func (f testCertificates) httpClient(t *testing.T, cert *tls.Certificate) *http.Client {
	t.Helper()
	config := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: f.roots}
	if cert != nil {
		// Offer even an invalid fixture so the server, not client CA selection,
		// rejects the untrusted chain or incorrect EKU.
		config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return cert, nil }
	}
	transport := &http.Transport{TLSClientConfig: config}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}
