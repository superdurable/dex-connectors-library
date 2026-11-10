// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// maximumRequestBodyBytes limits every request body.
const maximumRequestBodyBytes = 4 << 20

// requestHostPolicy accepts only requests that name a loopback host or an
// allowed host, which stops DNS rebinding; a non-loopback IP literal is
// refused because it arrives only through forwarding the user did not intend.
type requestHostPolicy struct {
	allowedHosts map[string]bool
}

func newRequestHostPolicy(allowedHosts []string) requestHostPolicy {
	canonicalHosts := map[string]bool{}
	for _, host := range allowedHosts {
		if canonical := canonicalHost(host); canonical != "" {
			canonicalHosts[canonical] = true
		}
	}
	return requestHostPolicy{allowedHosts: canonicalHosts}
}

func (policy requestHostPolicy) allowsHost(hostHeader string) bool {
	host := canonicalHost(hostHeader)
	return host != "" && (isLoopbackHostName(host) || policy.allowedHosts[host])
}

// allowsOrigin accepts a request without Origin, or with an http origin whose
// host the Host check accepts. No CORS headers are ever sent.
func (policy requestHostPolicy) allowsOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" || parsed.User != nil {
		return false
	}
	return policy.allowsHost(parsed.Host)
}

// protect wraps next with the Host and Origin checks, Fetch-metadata
// cross-origin protection, and the body size limit.
func (policy requestHostPolicy) protect(next http.Handler) http.Handler {
	crossOriginProtected := http.NewCrossOriginProtection().Handler(next)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !policy.allowsHost(request.Host) {
			http.Error(response, "dex-connectors-mcp does not accept this Host", http.StatusForbidden)
			return
		}
		if !policy.allowsOrigin(request.Header.Get("Origin")) {
			http.Error(response, "dex-connectors-mcp does not accept this Origin", http.StatusForbidden)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maximumRequestBodyBytes)
		crossOriginProtected.ServeHTTP(response, request)
	})
}

// ValidateListenAddress returns an error unless address is HOST:PORT with a
// loopback IP literal or localhost as HOST.
func ValidateListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("--listen %q must be HOST:PORT: %w", address, err)
	}
	if !isLoopbackListenHost(host) {
		return fmt.Errorf("--listen %q must use a loopback address (127.0.0.0/8, ::1, or localhost)", address)
	}
	return nil
}

func isLoopbackListenHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// canonicalHost lowercases a host, drops its port, brackets, and trailing
// dot, and returns "" for no host.
func canonicalHost(value string) string {
	host := strings.TrimSpace(value)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func isLoopbackHostName(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
