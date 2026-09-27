// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package providerhttp holds provider-neutral HTTP safety helpers for connector
// operations: a hardened client copy, base-URL and credential checks, bounded
// body reads, Retry-After parsing, error-token extraction that never returns
// provider message text, and a bounded server-sent event reader.
//
// The package contains no provider host, model ID, error code, or credential.
// A connector passes those in from its own manifest-backed configuration.
//
// A connector typically hardens its client and validates its base URL once in
// its constructor, then reads each response within its own limits:
//
//	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
//	if err != nil {
//		return nil, fmt.Errorf("endpoint: %w", err)
//	}
//	httpClient := providerhttp.NewProviderHTTPClient(callerClient, 870*time.Second)
//	// ... per call:
//	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
//	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
//		// select an invalid-response branch
//	}
//	delay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
//	tokens := providerhttp.ReadErrorTokens(errorBody, []string{"/error/type", "/error/code"})
package providerhttp

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewProviderHTTPClient returns a copy of caller that is safe for credentialed
// provider calls. The copy never follows a redirect, so a credential header is
// never replayed to another host; the caller receives the 3xx response instead.
// When caller sets no Timeout, the copy uses requestTimeout; a zero
// requestTimeout then leaves the copy without a timeout. A nil caller starts
// from the zero http.Client, which uses http.DefaultTransport.
//
// The copy shares caller's Transport and Jar. The caller keeps ownership of the
// original client and its transport, and later changes to caller do not affect
// the copy.
func NewProviderHTTPClient(caller *http.Client, requestTimeout time.Duration) *http.Client {
	hardened := &http.Client{}
	if caller != nil {
		copied := *caller
		hardened = &copied
	}
	if hardened.Timeout == 0 {
		hardened.Timeout = requestTimeout
	}
	hardened.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return hardened
}

// ValidateBaseURL checks a provider base URL and returns it without trailing
// slashes, ready for joining with a path that starts with "/".
//
// The value must be printable ASCII and an absolute URL with a host. It must use
// HTTPS, except that plain HTTP is accepted for a loopback host (localhost,
// 127.0.0.0/8, or ::1) so tests can use a local server. User information, a
// query, and a fragment are rejected, because each can carry a secret or change
// where a joined path points. Error messages never repeat the value.
func ValidateBaseURL(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("base URL is required")
	}
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return "", fmt.Errorf("base URL must be printable ASCII without spaces")
		}
	}
	if strings.ContainsAny(value, "?#") {
		return "", fmt.Errorf("base URL cannot contain a query or a fragment")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("base URL must be an absolute URL with a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("base URL cannot contain user information")
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return "", fmt.Errorf("base URL must use HTTPS unless its host is loopback")
		}
	default:
		return "", fmt.Errorf("base URL must use HTTPS")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// IsHeaderSafeCredential reports whether value can travel in one HTTP header
// without being altered or splitting the header: it is non-empty and every
// byte is printable ASCII from 0x21 through 0x7E. Spaces, control characters,
// and non-ASCII bytes are rejected; punctuation such as "|" is accepted.
func IsHeaderSafeCredential(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7E {
			return false
		}
	}
	return true
}

func isLoopbackHost(host string) bool {
	if strings.ToLower(host) == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
