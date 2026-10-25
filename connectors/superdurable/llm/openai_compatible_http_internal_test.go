// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenAICompatibleAddressPolicy checks the resolved addresses before any credentialed dial.
func TestOpenAICompatibleAddressPolicy(t *testing.T) {
	for _, testCase := range []struct {
		address      string
		loopbackHost bool
		allowed      bool
	}{
		{"8.8.8.8", false, true},
		{"127.0.0.1", false, false},
		{"127.0.0.1", true, true},
		{"10.0.0.1", false, false},
		{"192.168.1.1", false, false},
		{"169.254.169.254", false, false},
		{"100.64.0.1", false, false},
		{"fd00:ec2::254", false, false},
		{"fe80::1", false, false},
		{"::1", true, true},
		{"::ffff:169.254.169.254", false, false},
	} {
		t.Run(testCase.address, func(t *testing.T) {
			address := netip.MustParseAddr(testCase.address).Unmap()
			require.Equal(t, testCase.allowed, isAllowedOpenAICompatibleAddress(address, testCase.loopbackHost))
		})
	}
}

// TestOpenAICompatibleClientSpeaksHTTP2 keeps gateways that negotiate HTTP/2 through ALPN reachable.
func TestOpenAICompatibleClientSpeaksHTTP2(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	client, err := newOpenAICompatibleHTTPClient(server.Client(), true)
	require.NoError(t, err)
	response, err := client.Get(server.URL + "/v1/models")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, 2, response.ProtoMajor)
}
