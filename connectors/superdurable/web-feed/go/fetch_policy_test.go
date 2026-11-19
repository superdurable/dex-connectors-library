// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// The pre-send literal check must refuse exactly what the PublicHTTPS dialer refuses, with the same reason.
func TestRefusedAddressLiteralsMatchThePublicHTTPSPolicy(t *testing.T) {
	for _, literal := range []string{
		"127.0.0.1", "127.8.9.10", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1", "100.127.255.254",
		"169.254.169.254", "169.254.0.1", "0.0.0.0", "224.0.0.1", "255.255.255.255", "::1", "::", "fe80::1", "fd00:ec2::254",
		"fd12:3456::1", "fc00::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.0.0.5", "::ffff:169.254.169.254",
		"203.0.113.10", "8.8.8.8", "172.32.0.1", "100.128.0.1", "2001:db8::1", "2606:4700::1111", "::ffff:8.8.8.8",
	} {
		t.Run(literal, func(t *testing.T) {
			address := netip.MustParseAddr(literal)
			client, err := providerhttp.NewAddressPolicyClientForTest(nil, time.Second, providerhttp.PublicHTTPS,
				func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{address}, nil },
				func(context.Context, string, string) (net.Conn, error) {
					return nil, errors.New("allowed address dialed")
				})
			require.NoError(t, err)
			_, err = client.Get("https://policy.example.com/")
			var refusal *providerhttp.AddressRefusal
			if errors.As(err, &refusal) {
				require.Equal(t, refusal.Reason, refusedAddressLiteralReason(literal))
			} else {
				require.ErrorContains(t, err, "allowed address dialed")
				require.Empty(t, refusedAddressLiteralReason(literal))
			}
		})
	}
	require.Empty(t, refusedAddressLiteralReason("blog.example.com"), "named hosts are checked when dialed")
}

func TestReadFeedRefusesAnAddressLiteralBeforeConnecting(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://blog.example.com/to-literal", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://[::ffff:169.254.169.254]/latest", http.StatusFound)
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://10.0.0.5/feed.xml"})
	requireBranch(t, result, err, ReadFeedBranchUnreachable, "VALIDATION")
	require.Equal(t, "the URL resolves to an address the connector will not connect to: only public addresses are allowed", result.Failure.Message)
	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/to-literal"})
	requireBranch(t, result, err, ReadFeedBranchUnreachable, "VALIDATION")
	require.Contains(t, result.Failure.Message, "link-local")
	require.Len(t, site.recordedRequests(), 1, "only the public first hop was requested")
	require.Equal(t, []string{"203.0.113.10:443"}, site.dialedAddresses(), "a refused literal is never dialed")
}
