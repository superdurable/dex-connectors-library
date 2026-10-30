// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// WithAddressResolutionForTest replaces DNS resolution and the TCP dial of the address policy, so a test
// chooses the addresses a host resolves to and where the connection really goes.
func WithAddressResolutionForTest(
	resolve func(ctx context.Context, host string) ([]netip.Addr, error),
	dial func(ctx context.Context, network string, address string) (net.Conn, error),
) Option {
	return func(options *clientOptions) {
		options.newHTTPClient = func(caller *http.Client, requestTimeout time.Duration) (*http.Client, error) {
			return providerhttp.NewAddressPolicyClientForTest(caller, requestTimeout, providerhttp.PrivateNetworkHTTP, resolve, dial)
		}
	}
}

// NewCredentialRefreshDriverThroughTransportForTest builds the refresh driver for config but sends its token
// requests through transport instead of the address policy, which can dial only a network address.
func NewCredentialRefreshDriverThroughTransportForTest(config Config, transport http.RoundTripper) (*CredentialRefreshDriver, error) {
	driver, err := NewCredentialRefreshDriver(config, nil)
	if err != nil {
		return nil, err
	}
	driver.httpClient = providerhttp.NewProviderHTTPClient(&http.Client{Transport: transport}, withConfigDefaults(config).RequestTimeout)
	return driver, nil
}
