// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

var (
	sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")
	awsMetadataIPv6    = netip.MustParseAddr("fd00:ec2::254")
)

// newOpenAICompatibleHTTPClient checks DNS answers at connection time and dials only checked addresses.
func newOpenAICompatibleHTTPClient(caller *http.Client, isLoopback bool) (*http.Client, error) {
	client := &http.Client{}
	if caller != nil {
		copied := *caller
		client = &copied
	}
	var transport *http.Transport
	switch configured := client.Transport.(type) {
	case nil:
		transport = http.DefaultTransport.(*http.Transport).Clone()
	case *http.Transport:
		transport = configured.Clone()
	default:
		return nil, fmt.Errorf("transport must be nil or an *http.Transport to enforce the address policy")
	}
	transport.Proxy = nil
	transport.Dial, transport.DialTLS, transport.DialTLSContext = nil, nil, nil
	// A cloned caller HTTP/2 handler would send requests through the caller's
	// connection pool, past the address check, so HTTP/2 runs on this transport.
	transport.TLSNextProto = nil
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.Protocols.SetHTTP2(true)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid provider address")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("provider address resolution failed")
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("provider address resolution returned no addresses")
		}
		for _, candidate := range addresses {
			if !isAllowedOpenAICompatibleAddress(candidate.Unmap(), isLoopback) {
				return nil, fmt.Errorf("provider address is not allowed")
			}
		}
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].Unmap().String(), port))
	}
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client, nil
}

func isAllowedOpenAICompatibleAddress(address netip.Addr, isLoopback bool) bool {
	if isLoopback {
		return address.IsLoopback()
	}
	return address.IsGlobalUnicast() && !address.IsLoopback() && !address.IsPrivate() &&
		!address.IsLinkLocalUnicast() && !sharedAddressSpace.Contains(address) && address != awsMetadataIPv6
}
