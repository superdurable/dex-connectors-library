// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"encoding/json"
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

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential a trusted hosted broker returns,
// {"access_key_id": "...", "secret_access_key": "...", "session_token": "..."}, where session_token is
// optional. Unknown members are rejected, and errors never repeat a value.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	credentials, err := decodeLocalCredentials(contents)
	if err != nil {
		return Credentials{}, errors.New("S3 resolved credential is invalid")
	}
	return credentials, validateResolvedCredentials(credentials)
}

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
