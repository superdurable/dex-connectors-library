// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// Standard Webhooks header names and secret prefix, from https://www.standardwebhooks.com.
const (
	standardWebhooksIDHeader        = "webhook-id"
	standardWebhooksTimestampHeader = "webhook-timestamp"
	standardWebhooksSignatureHeader = "webhook-signature"
	standardWebhooksSecretPrefix    = "whsec_"
	standardWebhooksSignatureScheme = "v1"
)

// headerNamePattern is the RFC 9110 token grammar for header field names.
var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// requestVerifier authenticates incoming requests with the connection's configured scheme.
type requestVerifier struct {
	scheme             VerificationScheme
	signatureHeader    string
	signaturePrefix    string
	signatureEncoding  SignatureEncoding
	timestampTolerance time.Duration
	tokenHeader        string
}

func newRequestVerifier(config *Config) (requestVerifier, error) {
	signatureHeader, err := canonicalHeaderName("signatureHeader", config.SignatureHeader)
	if err != nil {
		return requestVerifier{}, err
	}
	tokenHeader, err := canonicalHeaderName("tokenHeader", config.TokenHeader)
	if err != nil {
		return requestVerifier{}, err
	}
	if strings.ContainsFunc(config.SignaturePrefix, isControlCharacter) {
		return requestVerifier{}, fmt.Errorf("webhook signaturePrefix cannot contain control characters")
	}
	return requestVerifier{
		scheme: config.VerificationScheme, signatureHeader: signatureHeader, signaturePrefix: config.SignaturePrefix,
		signatureEncoding: config.SignatureEncoding, tokenHeader: tokenHeader,
		timestampTolerance: time.Duration(config.TimestampToleranceSeconds) * time.Second,
	}, nil
}

// verifyRequest authenticates one request. A missing secret wraps ErrVerificationUnavailable, so the
// endpoint answers 503 and the sender retries after the connection is fixed.
func (verifier requestVerifier) verifyRequest(request webhooktrigger.Request, credentials Credentials) error {
	secret := credentials.SigningSecret.Reveal()
	if secret == "" {
		return fmt.Errorf("webhook signing secret is not configured: %w", webhooktrigger.ErrVerificationUnavailable)
	}
	switch verifier.scheme {
	case VerificationSchemeStandardWebhooks:
		return verifier.verifyStandardWebhooksSignature(request, secret)
	case VerificationSchemeSharedToken:
		return verifier.verifySharedToken(request.Header, secret)
	default:
		return verifier.verifyHMACSignature(request, secret)
	}
}

func (verifier requestVerifier) verifyHMACSignature(request webhooktrigger.Request, secret string) error {
	headerValue := strings.TrimSpace(request.Header.Get(verifier.signatureHeader))
	if headerValue == "" {
		return errors.New("webhook signature header is missing")
	}
	encodedSignature, hasPrefix := strings.CutPrefix(headerValue, verifier.signaturePrefix)
	if !hasPrefix {
		return errors.New("webhook signature prefix is missing")
	}
	var signature []byte
	var err error
	if verifier.signatureEncoding == SignatureEncodingBase64 {
		signature, err = base64.StdEncoding.DecodeString(encodedSignature)
	} else {
		signature, err = hex.DecodeString(encodedSignature)
	}
	if err != nil {
		return errors.New("webhook signature is not encoded as configured")
	}
	if !hmac.Equal(signature, computeHMACSHA256([]byte(secret), request.Body)) {
		return errors.New("webhook signature does not match")
	}
	return nil
}

func (verifier requestVerifier) verifyStandardWebhooksSignature(request webhooktrigger.Request, secret string) error {
	key, err := standardWebhooksSigningKey(secret)
	if err != nil {
		return fmt.Errorf("%w: %w", err, webhooktrigger.ErrVerificationUnavailable)
	}
	webhookID := request.Header.Get(standardWebhooksIDHeader)
	timestampText := request.Header.Get(standardWebhooksTimestampHeader)
	signatureList := request.Header.Get(standardWebhooksSignatureHeader)
	if webhookID == "" || timestampText == "" || signatureList == "" {
		return errors.New("Standard Webhooks headers are incomplete")
	}
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		return errors.New("Standard Webhooks timestamp is not an integer")
	}
	offset := request.ReceivedAt.Sub(time.Unix(timestamp, 0))
	if offset > verifier.timestampTolerance || offset < -verifier.timestampTolerance {
		return errors.New("Standard Webhooks timestamp is outside the tolerance")
	}
	expected := standardWebhooksSignature(key, webhookID, timestampText, request.Body)
	for _, entry := range strings.Fields(signatureList) {
		version, encodedSignature, isVersioned := strings.Cut(entry, ",")
		if !isVersioned || version != standardWebhooksSignatureScheme {
			continue
		}
		signature, err := base64.StdEncoding.DecodeString(encodedSignature)
		if err == nil && hmac.Equal(signature, expected) {
			return nil
		}
	}
	return errors.New("no Standard Webhooks signature matches")
}

// verifySharedToken compares digests, so the comparison takes the same time for every token length.
func (verifier requestVerifier) verifySharedToken(header http.Header, secret string) error {
	token := header.Get(verifier.tokenHeader)
	if token == "" {
		return errors.New("webhook token header is missing")
	}
	tokenDigest, secretDigest := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(secret))
	if !hmac.Equal(tokenDigest[:], secretDigest[:]) {
		return errors.New("webhook token does not match")
	}
	return nil
}

// standardWebhooksSigningKey base64-decodes a whsec_ secret and uses any other secret's bytes unchanged.
func standardWebhooksSigningKey(secret string) ([]byte, error) {
	encodedKey, isStandardSecret := strings.CutPrefix(secret, standardWebhooksSecretPrefix)
	if !isStandardSecret {
		return []byte(secret), nil
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) == 0 {
		return nil, errors.New("webhook signing_secret starts with whsec_ but is not valid base64")
	}
	return key, nil
}

// standardWebhooksSignature signs "id.timestamp.body" and returns the raw HMAC-SHA256 digest.
func standardWebhooksSignature(key []byte, webhookID string, timestampText string, body []byte) []byte {
	signedContent := make([]byte, 0, len(webhookID)+len(timestampText)+len(body)+2)
	signedContent = append(signedContent, webhookID...)
	signedContent = append(signedContent, '.')
	signedContent = append(signedContent, timestampText...)
	signedContent = append(signedContent, '.')
	signedContent = append(signedContent, body...)
	return computeHMACSHA256(key, signedContent)
}

func computeHMACSHA256(key []byte, contents []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(contents) // hash.Hash writes never fail.
	return mac.Sum(nil)
}

// canonicalHeaderName validates a configured header name and returns its canonical form.
func canonicalHeaderName(field string, name string) (string, error) {
	trimmedName := strings.TrimSpace(name)
	if !headerNamePattern.MatchString(trimmedName) {
		return "", fmt.Errorf("webhook %s %q is not a valid HTTP header name", field, name)
	}
	return textproto.CanonicalMIMEHeaderKey(trimmedName), nil
}

func isControlCharacter(character rune) bool {
	return character < 0x20 || character == 0x7f
}
