// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mailtest runs in-process IMAP and SMTP servers over TLS, with fault injection, for the email
// connector's tests and its example's real-Dex tests. The IMAP server is go-imap's in-memory server; the
// SMTP server is go-smtp's server with a recording backend. Neither stores anything on disk.
package mailtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Certificates is a throwaway certificate authority and one server certificate for localhost and the
// loopback addresses.
type Certificates struct {
	// RootCAs trusts only the throwaway authority.
	RootCAs *x509.CertPool
	// AuthorityPEM is the authority certificate in PEM form, for a Worker that loads a CA file.
	AuthorityPEM      []byte
	serverCertificate tls.Certificate
}

// NewCertificates creates a new authority and server certificate valid for one hour.
func NewCertificates(t testing.TB) *Certificates {
	t.Helper()
	authorityKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	authorityTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mailtest authority"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, IsCA: true, BasicConstraintsValid: true,
	}
	authorityDER, err := x509.CreateCertificate(rand.Reader, authorityTemplate, authorityTemplate, &authorityKey.PublicKey, authorityKey)
	require.NoError(t, err)
	authority, err := x509.ParseCertificate(authorityDER)
	require.NoError(t, err)
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, authority, &serverKey.PublicKey, authorityKey)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(authority)
	return &Certificates{
		RootCAs:           pool,
		AuthorityPEM:      pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: authorityDER}),
		serverCertificate: tls.Certificate{Certificate: [][]byte{serverDER}, PrivateKey: serverKey},
	}
}

// serverTLSConfig returns a server configuration that presents the server certificate.
func (certificates *Certificates) serverTLSConfig() *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{certificates.serverCertificate}, MinVersion: tls.VersionTLS12}
}

// Security selects how a test server encrypts its connections.
type Security uint8

const (
	// ImplicitTLS starts TLS as soon as a connection opens.
	ImplicitTLS Security = iota + 1
	// StartTLS accepts plaintext and offers the STARTTLS upgrade.
	StartTLS
	// PlaintextOnly never offers TLS, to prove the connector refuses to authenticate.
	PlaintextOnly
)

// listen opens a loopback listener with the requested security and returns it with its port.
func listen(t testing.TB, certificates *Certificates, security Security) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	if security == ImplicitTLS {
		return tls.NewListener(listener, certificates.serverTLSConfig()), port
	}
	return listener, port
}
