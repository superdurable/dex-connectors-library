// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignJWTBearerAssertionProducesVerifiableRS256(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	issuedAt := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name           string
		assertion      JWTBearerAssertion
		expectedClaims map[string]any
	}{
		{
			name: "subject and scope",
			assertion: JWTBearerAssertion{Issuer: "service@example.test", Subject: "user@example.test",
				Audience: testTokenEndpointURL, Scope: "read send", IssuedAt: issuedAt, Lifetime: time.Hour},
			expectedClaims: map[string]any{"iss": "service@example.test", "sub": "user@example.test", "aud": testTokenEndpointURL,
				"scope": "read send", "iat": float64(issuedAt.Unix()), "exp": float64(issuedAt.Add(time.Hour).Unix())},
		},
		{
			name:      "omits empty subject and scope",
			assertion: JWTBearerAssertion{Issuer: "client-id", Audience: testTokenEndpointURL, IssuedAt: issuedAt, Lifetime: 3 * time.Minute},
			expectedClaims: map[string]any{"iss": "client-id", "aud": testTokenEndpointURL,
				"iat": float64(issuedAt.Unix()), "exp": float64(issuedAt.Add(3 * time.Minute).Unix())},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			signed, err := SignJWTBearerAssertion(test.assertion, privateKey)
			require.NoError(t, err)
			parts := strings.Split(signed.Reveal(), ".")
			require.Len(t, parts, 3)
			var header map[string]string
			require.NoError(t, json.Unmarshal(decodeSegment(t, parts[0]), &header))
			require.Equal(t, map[string]string{"alg": "RS256", "typ": "JWT"}, header)
			var claims map[string]any
			require.NoError(t, json.Unmarshal(decodeSegment(t, parts[1]), &claims))
			require.Equal(t, test.expectedClaims, claims)
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			require.NoError(t, rsa.VerifyPKCS1v15(&privateKey.PublicKey, crypto.SHA256, digest[:], decodeSegment(t, parts[2])))
		})
	}
}

func TestSignJWTBearerAssertionRejectsIncompleteInput(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	complete := JWTBearerAssertion{Issuer: "iss", Audience: "aud", IssuedAt: time.Unix(1, 0), Lifetime: time.Minute}
	for _, test := range []struct {
		name      string
		assertion func(JWTBearerAssertion) JWTBearerAssertion
		key       *rsa.PrivateKey
	}{
		{name: "missing key", assertion: func(assertion JWTBearerAssertion) JWTBearerAssertion { return assertion }},
		{name: "missing issuer", key: privateKey, assertion: func(assertion JWTBearerAssertion) JWTBearerAssertion { assertion.Issuer = ""; return assertion }},
		{name: "missing audience", key: privateKey, assertion: func(assertion JWTBearerAssertion) JWTBearerAssertion { assertion.Audience = ""; return assertion }},
		{name: "missing issued at", key: privateKey, assertion: func(assertion JWTBearerAssertion) JWTBearerAssertion {
			assertion.IssuedAt = time.Time{}
			return assertion
		}},
		{name: "non-positive lifetime", key: privateKey, assertion: func(assertion JWTBearerAssertion) JWTBearerAssertion { assertion.Lifetime = 0; return assertion }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := SignJWTBearerAssertion(test.assertion(complete), test.key)
			require.Error(t, err)
		})
	}
}

func TestParseRSAPrivateKeyPEM(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	require.NoError(t, err)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecPKCS8, err := x509.MarshalPKCS8PrivateKey(ecKey)
	require.NoError(t, err)

	parsed, err := ParseRSAPrivateKeyPEM(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})))
	require.NoError(t, err)
	require.True(t, rsaKey.Equal(parsed))
	parsed, err = ParseRSAPrivateKeyPEM(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})))
	require.NoError(t, err)
	require.True(t, rsaKey.Equal(parsed))

	_, err = ParseRSAPrivateKeyPEM(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecPKCS8})))
	require.EqualError(t, err, "private key must use RSA")
	_, err = ParseRSAPrivateKeyPEM("not a key")
	require.EqualError(t, err, "private key is not PEM encoded")
	_, err = ParseRSAPrivateKeyPEM(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage")})))
	require.EqualError(t, err, "private key is invalid")
}

func decodeSegment(t *testing.T, segment string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	require.NoError(t, err)
	return decoded
}
