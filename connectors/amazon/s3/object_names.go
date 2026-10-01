// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxObjectKeyBytes is the Amazon S3 object key limit, measured in UTF-8 bytes.
	maxObjectKeyBytes = 1024
	// maxContinuationTokenBytes bounds the opaque token S3 returns for the next listObjects page.
	maxContinuationTokenBytes = 4096
)

// bucketNamePattern is the general purpose bucket rule: 3 to 63 lowercase letters, digits, dots, hyphens.
var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// validateBucketName applies the general purpose rules; legacy us-east-1 names are rejected.
func validateBucketName(bucket string) error {
	if !bucketNamePattern.MatchString(bucket) || strings.Contains(bucket, "..") || net.ParseIP(bucket) != nil {
		return errors.New("bucket must be a 3 to 63 character S3 bucket name of lowercase letters, digits, dots, and hyphens")
	}
	return nil
}

// validateObjectKey rejects dot path segments, which HTTP clients and proxies may normalize away.
func validateObjectKey(key string) error {
	if key == "" || len(key) > maxObjectKeyBytes {
		return errors.New("key must be 1 to 1024 bytes")
	}
	if err := validateKeyText(key, "key"); err != nil {
		return err
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." || segment == ".." {
			return errors.New("key cannot contain . or .. path segments")
		}
	}
	return nil
}

// validateKeyText accepts UTF-8 text without control characters, the form keys, prefixes, and delimiters take.
func validateKeyText(value string, field string) error {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return errors.New(field + " must be UTF-8 text without control characters")
	}
	return nil
}

// validateKeyPrefix accepts a blank or bounded key prefix, delimiter, or start-after key.
func validateKeyPrefix(value string, field string) error {
	if len(value) > maxObjectKeyBytes {
		return errors.New(field + " must be at most 1024 bytes")
	}
	return validateKeyText(value, field)
}

// validateContinuationToken accepts a blank token or the printable ASCII token an earlier page returned.
func validateContinuationToken(token string) error {
	if len(token) > maxContinuationTokenBytes {
		return errors.New("continuationToken must be at most 4096 bytes")
	}
	for index := 0; index < len(token); index++ {
		if token[index] < 0x21 || token[index] > 0x7E {
			return errors.New("continuationToken must be the printable token an earlier page returned")
		}
	}
	return nil
}
