// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// JWTBearerAssertion holds the claims of an RS256 JWT-bearer grant assertion
// (RFC 7523 section 3), such as a Google service-account assertion.
type JWTBearerAssertion struct {
	// Issuer is the iss claim, usually the service account or client ID.
	Issuer string
	// Subject is the sub claim, usually the delegated user. Empty omits it.
	Subject string
	// Audience is the aud claim, usually the token endpoint URL.
	Audience string
	// Scope is the space-separated scope claim some providers require. Empty omits it.
	Scope string
	// IssuedAt is the iat claim.
	IssuedAt time.Time
	// Lifetime sets the exp claim relative to IssuedAt. It must be positive.
	Lifetime time.Duration
}

// SignJWTBearerAssertion signs assertion with privateKey using RS256 and
// returns the compact JWT for ExchangeJWTBearerAssertion.
func SignJWTBearerAssertion(assertion JWTBearerAssertion, privateKey *rsa.PrivateKey) (sdkgo.SecretString, error) {
	if privateKey == nil {
		return sdkgo.SecretString{}, errors.New("JWT-bearer assertion private key is missing")
	}
	if assertion.Issuer == "" || assertion.Audience == "" || assertion.IssuedAt.IsZero() || assertion.Lifetime <= 0 {
		return sdkgo.SecretString{}, errors.New("JWT-bearer assertion claims are incomplete")
	}
	claims := map[string]any{
		"aud": assertion.Audience,
		"exp": assertion.IssuedAt.Add(assertion.Lifetime).Unix(),
		"iat": assertion.IssuedAt.Unix(),
		"iss": assertion.Issuer,
	}
	if assertion.Subject != "" {
		claims["sub"] = assertion.Subject
	}
	if assertion.Scope != "" {
		claims["scope"] = assertion.Scope
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return sdkgo.SecretString{}, errors.New("JWT-bearer assertion header could not be encoded")
	}
	encodedClaims, err := json.Marshal(claims)
	if err != nil {
		return sdkgo.SecretString{}, errors.New("JWT-bearer assertion claims could not be encoded")
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(encodedClaims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return sdkgo.SecretString{}, errors.New("JWT-bearer assertion could not be signed")
	}
	return sdkgo.NewSecretString(unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)), nil
}

// ParseRSAPrivateKeyPEM parses the first PEM block of pemText as a PKCS #8 or
// PKCS #1 RSA private key. Error messages never repeat key material.
func ParseRSAPrivateKeyPEM(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		privateKey, isRSA := parsedKey.(*rsa.PrivateKey)
		if !isRSA {
			return nil, errors.New("private key must use RSA")
		}
		return privateKey, nil
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("private key is invalid")
	}
	return privateKey, nil
}
