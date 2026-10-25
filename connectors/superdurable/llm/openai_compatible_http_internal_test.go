// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
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

// TestOpenAICompatibleClientSpeaksHTTP1 keeps gateways that offer only HTTP/1.1 over TLS reachable.
func TestOpenAICompatibleClientSpeaksHTTP1(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	client, err := newOpenAICompatibleHTTPClient(server.Client(), true)
	require.NoError(t, err)
	response, err := client.Get(server.URL + "/v1/models")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, 1, response.ProtoMajor)
}

// TestOpenAICompatibleClientOwnsHTTP2WithCallerTransport keeps a caller transport's
// HTTP/2 handler and pool out of the guarded client, and still speaks HTTP/2.
func TestOpenAICompatibleClientOwnsHTTP2WithCallerTransport(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	get := func(t *testing.T, caller *http.Transport) *http.Response {
		t.Helper()
		client, err := newOpenAICompatibleHTTPClient(&http.Client{Transport: caller}, true)
		require.NoError(t, err)
		response, err := client.Get(server.URL + "/v1/models")
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		return response
	}

	t.Run("inherited handler", func(t *testing.T) {
		var inherited atomic.Bool
		caller := server.Client().Transport.(*http.Transport).Clone()
		caller.TLSClientConfig.NextProtos = []string{"h2", "http/1.1"}
		caller.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{
			"h2": func(string, *tls.Conn) http.RoundTripper {
				inherited.Store(true)
				return http.DefaultTransport
			},
		}
		response := get(t, caller)
		require.False(t, inherited.Load(), "the caller's HTTP/2 handler and pool were used")
		require.Equal(t, 2, response.ProtoMajor)
	})

	t.Run("used caller without forced HTTP/2", func(t *testing.T) {
		caller := server.Client().Transport.(*http.Transport)
		response, err := (&http.Client{Transport: caller}).Get(server.URL)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		// Its TLS configuration now advertises h2, which a clone copies.
		caller.ForceAttemptHTTP2 = false
		require.Equal(t, 2, get(t, caller).ProtoMajor)
	})
}
