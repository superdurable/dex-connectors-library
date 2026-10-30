// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package testcertificates issues a throwaway certificate authority and server certificate for TLS tests.
package testcertificates

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
	"time"
)

// Authority is a self-signed CA and one server certificate it issued.
type Authority struct {
	// CertificatePEM is the CA certificate a client trusts.
	CertificatePEM []byte
	// ServerTLS serves the issued certificate.
	ServerTLS *tls.Config
}

// Issue creates a CA and a server certificate valid for dnsNames and ipAddresses for one day.
func Issue(dnsNames []string, ipAddresses []net.IP) (Authority, error) {
	authorityKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Authority{}, err
	}
	now := time.Now()
	authorityTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "scripted MySQL test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	authorityDER, err := x509.CreateCertificate(rand.Reader, authorityTemplate, authorityTemplate, &authorityKey.PublicKey, authorityKey)
	if err != nil {
		return Authority{}, err
	}
	authorityCertificate, err := x509.ParseCertificate(authorityDER)
	if err != nil {
		return Authority{}, err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Authority{}, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "scripted MySQL server"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		DNSNames: dnsNames, IPAddresses: ipAddresses,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, authorityCertificate, &serverKey.PublicKey, authorityKey)
	if err != nil {
		return Authority{}, err
	}
	return Authority{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: authorityDER}),
		ServerTLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}},
		},
	}, nil
}
