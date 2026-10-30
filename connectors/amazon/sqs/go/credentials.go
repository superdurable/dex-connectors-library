// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	maxAccessKeyIDBytes     = 128
	maxSecretAccessKeyBytes = 256
	// maxSessionTokenBytes keeps x-amz-security-token well inside the request-header limits of AWS endpoints.
	maxSessionTokenBytes = 4096
)

// validateResolvedCredentials accepts values that fit the SigV4 Credential scope and request headers.
func validateResolvedCredentials(credentials Credentials) error {
	accessKeyID := credentials.AccessKeyID.Reveal()
	// A slash, comma, or equals sign would end the Credential=ID/date/region/sqs/aws4_request field early.
	if !providerhttp.IsHeaderSafeCredential(accessKeyID) || len(accessKeyID) > maxAccessKeyIDBytes || strings.ContainsAny(accessKeyID, "/,=") {
		return errors.New("SQS access key ID is required and must be printable ASCII without spaces, slashes, commas, or equals signs")
	}
	secretAccessKey := credentials.SecretAccessKey.Reveal()
	if secretAccessKey == "" || len(secretAccessKey) > maxSecretAccessKeyBytes || !utf8.ValidString(secretAccessKey) ||
		strings.ContainsFunc(secretAccessKey, unicode.IsControl) {
		return errors.New("SQS secret access key is required and cannot contain control characters")
	}
	if sessionToken := credentials.SessionToken.Reveal(); sessionToken != "" &&
		(!providerhttp.IsHeaderSafeCredential(sessionToken) || len(sessionToken) > maxSessionTokenBytes) {
		return errors.New("SQS session token must be printable ASCII without spaces")
	}
	return nil
}
