// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const maximumHeaderValueBytes = 8192

var (
	headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

	// supportedMethods are the methods sendRequest accepts.
	supportedMethods = map[string]bool{
		http.MethodGet: true, http.MethodHead: true, http.MethodOptions: true,
		http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
	}

	// reservedRequestHeaders are lowercase names that the connection, the transport, or intermediaries own.
	reservedRequestHeaders = map[string]bool{
		"authorization": true, "proxy-authorization": true, "host": true, "content-length": true,
		"transfer-encoding": true, "connection": true, "keep-alive": true, "te": true, "trailer": true,
		"upgrade": true, "cookie": true,
	}
)

// plannedRequest is a validated SendRequestInput: everything one HTTP request needs except the credential.
type plannedRequest struct {
	method  string
	target  *url.URL
	headers http.Header
	body    string
	// idempotencyKeyHeader is the header that carries the Step's idempotency key, or empty.
	idempotencyKeyHeader string
	// isFencedWrite marks a single-dispatch POST, which the transport must never replay.
	isFencedWrite bool
}

// isUndeduplicatedPost reports a POST that names no idempotency header, which the API cannot deduplicate.
func (request plannedRequest) isUndeduplicatedPost() bool {
	return request.method == http.MethodPost && request.idempotencyKeyHeader == "" && !request.isFencedWrite
}

// planRequest validates input against the connection and returns the request to send, or an error whose
// text is safe to show: it names the operation, the field, and the rule, never a value.
func (client *Client) planRequest(operation string, input SendRequestInput) (plannedRequest, error) {
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !supportedMethods[method] {
		return plannedRequest{}, fmt.Errorf("%s method must be GET, HEAD, OPTIONS, POST, PUT, PATCH, or DELETE", operation)
	}
	target, err := client.resolveTarget(operation, input)
	if err != nil {
		return plannedRequest{}, err
	}
	headers, err := validateRequestHeaders(operation, input.Headers)
	if err != nil {
		return plannedRequest{}, err
	}
	if input.IdempotencyKeyHeader != "" {
		if err := validateHeaderName(operation, "idempotencyKeyHeader", input.IdempotencyKeyHeader); err != nil {
			return plannedRequest{}, err
		}
	}
	if input.Body != "" {
		switch {
		case method == http.MethodGet || method == http.MethodHead:
			return plannedRequest{}, fmt.Errorf("%s body is not allowed with GET or HEAD", operation)
		case int64(len(input.Body)) > client.maxBodyBytes:
			return plannedRequest{}, fmt.Errorf("%s body exceeds the connection's maxBodyBytes", operation)
		case headers.Get("Content-Type") == "":
			headers.Set("Content-Type", "application/json")
		}
	}
	return plannedRequest{
		method: method, target: target, headers: headers, body: input.Body, idempotencyKeyHeader: input.IdempotencyKeyHeader,
	}, nil
}

// resolveTarget joins a relative path to the API base URL, or accepts an absolute URL on the base URL's
// origin, then adds the query parameters. The access token goes only to that origin.
func (client *Client) resolveTarget(operation string, input SendRequestInput) (*url.URL, error) {
	var target *url.URL
	switch {
	case (input.Path == "") == (input.URL == ""):
		return nil, fmt.Errorf("%s needs exactly one of path and url", operation)
	case input.Path != "":
		var err error
		if target, err = client.joinPath(operation, input.Path); err != nil {
			return nil, err
		}
	default:
		parsed, err := parseHTTPURL(input.URL)
		if err != nil {
			return nil, fmt.Errorf("%s url: %w", operation, err)
		}
		if originOf(parsed) != originOf(client.apiBaseURL) {
			return nil, fmt.Errorf("%s url must have the scheme, host, and port of the connection's apiBaseUrl", operation)
		}
		target = parsed
	}
	if len(input.Query) > 0 {
		values := target.Query()
		for name, parameterValues := range input.Query {
			if name == "" {
				return nil, fmt.Errorf("%s query parameter names cannot be empty", operation)
			}
			for _, value := range parameterValues {
				values.Add(name, value)
			}
		}
		target.RawQuery = values.Encode()
	}
	return target, nil
}

// joinPath appends path, which starts with a single slash and carries no query, fragment, backslash, or
// dot-dot segment, to the base URL's path.
func (client *Client) joinPath(operation string, path string) (*url.URL, error) {
	for index := 0; index < len(path); index++ {
		if path[index] <= ' ' || path[index] > '~' {
			return nil, fmt.Errorf("%s path must be printable ASCII without spaces", operation)
		}
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, fmt.Errorf("%s path must start with a single slash", operation)
	}
	if strings.ContainsAny(path, "?#\\") {
		return nil, fmt.Errorf("%s path cannot contain ?, #, or a backslash; use query for parameters", operation)
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return nil, fmt.Errorf("%s path has invalid percent-encoding", operation)
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == ".." {
			return nil, fmt.Errorf("%s path cannot contain a .. segment", operation)
		}
	}
	target, err := url.Parse(client.apiBaseURL.String() + path)
	if err != nil || originOf(target) != originOf(client.apiBaseURL) {
		return nil, fmt.Errorf("%s path cannot be joined to the connection's apiBaseUrl", operation)
	}
	return target, nil
}

// originOf returns scheme://host:port in lowercase with the default port filled in, so two spellings of
// one origin compare equal.
func originOf(address *url.URL) string {
	scheme := strings.ToLower(address.Scheme)
	port := address.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[scheme]
	}
	return scheme + "://" + net.JoinHostPort(strings.ToLower(address.Hostname()), port)
}

// validateRequestHeaders copies headers after checking names and values. A connection-owned or
// transport-owned header, such as Authorization, selects defect instead of being dropped silently.
func validateRequestHeaders(operation string, headers map[string]string) (http.Header, error) {
	validated := make(http.Header, len(headers)+2)
	for name, value := range headers {
		if err := validateHeaderName(operation, "headers", name); err != nil {
			return nil, err
		}
		if len(value) > maximumHeaderValueBytes {
			return nil, fmt.Errorf("%s header values are limited to 8192 bytes", operation)
		}
		for index := 0; index < len(value); index++ {
			if (value[index] < ' ' && value[index] != '\t') || value[index] == 0x7f {
				return nil, fmt.Errorf("%s header values cannot contain control characters", operation)
			}
		}
		if _, isDuplicate := validated[http.CanonicalHeaderKey(name)]; isDuplicate {
			return nil, fmt.Errorf("%s header names must be unique ignoring case", operation)
		}
		validated.Set(name, value)
	}
	return validated, nil
}

func validateHeaderName(operation string, field string, name string) error {
	folded := strings.ToLower(name)
	switch {
	case !headerNamePattern.MatchString(name):
		return fmt.Errorf("%s %s names must be RFC 7230 tokens", operation, field)
	case reservedRequestHeaders[folded] || strings.HasPrefix(folded, "proxy-"):
		return fmt.Errorf("%s %s cannot name Authorization or a header the transport owns", operation, field)
	}
	return nil
}
