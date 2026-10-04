// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// localProviderTransport sends the QuickBooks API and Intuit token hosts to one loopback fake and refuses every other host.
type localProviderTransport struct {
	target *url.URL
	base   http.RoundTripper
}

// newLocalProviderHTTPClient copies caller and routes its QuickBooks and Intuit requests to the loopback baseURL.
func newLocalProviderHTTPClient(caller *http.Client, baseURL string) (*http.Client, error) {
	validated, err := providerhttp.ValidateBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("QuickBooks local provider URL: %w", err)
	}
	target, err := url.Parse(validated)
	if err != nil {
		return nil, errors.New("QuickBooks local provider URL cannot be parsed")
	}
	if !isLoopbackHostname(target.Hostname()) {
		return nil, errors.New("QuickBooks local provider URL must use a loopback host, because it is for local verification only")
	}
	if target.Path != "" {
		return nil, errors.New("QuickBooks local provider URL cannot contain a path; request paths are kept")
	}
	routed := &http.Client{}
	if caller != nil {
		copied := *caller
		routed = &copied
	}
	base := routed.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	routed.Transport = localProviderTransport{target: target, base: base}
	return routed, nil
}

// RoundTrip rewrites only the scheme and host of a request for a QuickBooks or Intuit token host.
func (transport localProviderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.URL.Host {
	case productionAPIHost, sandboxAPIHost, tokenHost:
	default:
		return nil, errors.New("QuickBooks local provider transport refuses a host other than QuickBooks or Intuit's token host")
	}
	rewritten := request.Clone(request.Context())
	rewritten.URL.Scheme = transport.target.Scheme
	rewritten.URL.Host = transport.target.Host
	rewritten.Host = transport.target.Host
	return transport.base.RoundTrip(rewritten)
}

func isLoopbackHostname(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	address := net.ParseIP(hostname)
	return address != nil && address.IsLoopback()
}
