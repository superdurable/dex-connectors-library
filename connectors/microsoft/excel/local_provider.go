// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// localProviderTransport sends Microsoft Graph and identity requests to one loopback fake and refuses every other host.
type localProviderTransport struct {
	target *url.URL
	base   http.RoundTripper
}

// newLocalProviderHTTPClient copies caller and routes its Microsoft requests to the loopback baseURL.
func newLocalProviderHTTPClient(caller *http.Client, baseURL string) (*http.Client, error) {
	validated, err := providerhttp.ValidateBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("Microsoft Excel local provider URL: %w", err)
	}
	target, err := url.Parse(validated)
	if err != nil {
		return nil, errors.New("Microsoft Excel local provider URL cannot be parsed")
	}
	if !isLoopbackHostname(target.Hostname()) {
		return nil, errors.New("Microsoft Excel local provider URL must use a loopback host, because it is for local verification only")
	}
	if target.Path != "" || target.RawQuery != "" || target.Fragment != "" || target.User != nil {
		return nil, errors.New("Microsoft Excel local provider URL cannot contain a path, query, fragment, or user information")
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

// RoundTrip rewrites only the scheme and host of a request for a Microsoft host.
func (transport localProviderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != graphHost && request.URL.Host != loginHost {
		return nil, errors.New("Microsoft Excel local provider transport refuses a host other than Microsoft Graph and Microsoft identity")
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
