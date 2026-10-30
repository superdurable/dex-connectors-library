// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

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
	// maxSessionTokenBytes keeps x-amz-security-token inside S3's 8 KB request-header limit.
	maxSessionTokenBytes = 4096
)

// validateResolvedCredentials accepts values that fit the SigV4 Credential scope and request headers.
func validateResolvedCredentials(credentials Credentials) error {
	accessKeyID := credentials.AccessKeyID.Reveal()
	// A slash, comma, or equals sign would end the Credential=ID/date/region/s3/aws4_request field early.
	if !providerhttp.IsHeaderSafeCredential(accessKeyID) || len(accessKeyID) > maxAccessKeyIDBytes || strings.ContainsAny(accessKeyID, "/,=") {
		return errors.New("S3 access key ID is required and must be printable ASCII without spaces, slashes, commas, or equals signs")
	}
	secretAccessKey := credentials.SecretAccessKey.Reveal()
	if secretAccessKey == "" || len(secretAccessKey) > maxSecretAccessKeyBytes || !utf8.ValidString(secretAccessKey) ||
		strings.ContainsFunc(secretAccessKey, unicode.IsControl) {
		return errors.New("S3 secret access key is required and cannot contain control characters")
	}
	if sessionToken := credentials.SessionToken.Reveal(); sessionToken != "" &&
		(!providerhttp.IsHeaderSafeCredential(sessionToken) || len(sessionToken) > maxSessionTokenBytes) {
		return errors.New("S3 session token must be printable ASCII without spaces")
	}
	return nil
}
