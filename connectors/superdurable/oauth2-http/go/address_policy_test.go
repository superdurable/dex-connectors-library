// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// namedNetwork stands in for DNS and the network: every host name resolves to the addresses a test sets,
// and every connection really goes to target, so the test sees which address the dialer chose to connect to.
type namedNetwork struct {
	target    string
	mu        sync.Mutex
	addresses map[string][]netip.Addr
	dialed    []string
}

func newNamedNetwork(target string) *namedNetwork {
	return &namedNetwork{target: target, addresses: map[string][]netip.Addr{}}
}

func (network *namedNetwork) resolveTo(host string, addresses ...string) {
	network.mu.Lock()
	defer network.mu.Unlock()
	network.addresses[host] = nil
	for _, address := range addresses {
		network.addresses[host] = append(network.addresses[host], netip.MustParseAddr(address))
	}
}

func (network *namedNetwork) option() oauth2http.Option {
	return oauth2http.WithAddressResolutionForTest(
		func(_ context.Context, host string) ([]netip.Addr, error) {
			network.mu.Lock()
			defer network.mu.Unlock()
			return append([]netip.Addr(nil), network.addresses[host]...), nil
		},
		func(ctx context.Context, networkName string, address string) (net.Conn, error) {
			network.mu.Lock()
			network.dialed = append(network.dialed, address)
			network.mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, networkName, network.target)
		})
}

func (network *namedNetwork) dialedAddresses() []string {
	network.mu.Lock()
	defer network.mu.Unlock()
	return append([]string(nil), network.dialed...)
}

func serverPort(t *testing.T, server *httptest.Server) string {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	return parsed.Port()
}

// namedAPISession opens a client whose apiBaseUrl is a host name that network resolves.
func namedAPISession(t *testing.T, server *httptest.Server, scheme string, host string, network *namedNetwork) *oauth2http.Client {
	t.Helper()
	expiresAt := testStart.Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentials(testCredentials("access-token-1", "refresh-token-1"), &expiresAt, func() time.Time { return testStart })
	client, err := oauth2http.New(oauth2http.Config{
		AuthorizationEndpoint: scheme + "://" + host + "/authorize", TokenEndpoint: scheme + "://" + host + ":" + serverPort(t, server) + "/token",
		Scopes: []string{"openid"}, APIBaseURL: scheme + "://" + host + ":" + serverPort(t, server),
	}, credentials, oauth2http.WithClock(func() time.Time { return testStart }), oauth2http.WithHTTPClient(server.Client()), network.option())
	require.NoError(t, err)
	return client
}

func TestPlainHTTPConnectsOnlyWhenEveryResolvedAddressIsLoopbackOrPrivate(t *testing.T) {
	for _, test := range []struct {
		name        string
		addresses   []string
		isPermitted bool
	}{
		{"IPv4 loopback", []string{"127.0.0.1"}, true},
		{"IPv6 loopback", []string{"::1"}, true},
		{"10.0.0.0/8", []string{"10.1.2.3"}, true},
		{"172.16.0.0/12", []string{"172.16.0.5"}, true},
		{"192.168.0.0/16", []string{"192.168.1.1"}, true},
		{"IPv6 unique local", []string{"fd12:3456::1"}, true},
		{"several private addresses", []string{"10.0.0.1", "192.168.0.2", "::1"}, true},
		{"public IPv4", []string{"8.8.8.8"}, false},
		{"public IPv6", []string{"2001:4860:4860::8888"}, false},
		{"carrier-grade NAT is not private", []string{"100.64.0.1"}, false},
		{"one public address among private ones", []string{"10.0.0.1", "8.8.8.8"}, false},
		{"cloud metadata address", []string{"169.254.169.254"}, false},
		{"another link-local IPv4 address", []string{"169.254.0.1"}, false},
		{"IPv6 link-local", []string{"fe80::1"}, false},
		{"IPv4-mapped metadata address", []string{"::ffff:169.254.169.254"}, false},
		{"IPv6 metadata address", []string{"fd00:ec2::254"}, false},
		{"loopback together with the metadata address", []string{"127.0.0.1", "169.254.169.254"}, false},
		{"unspecified address", []string{"0.0.0.0"}, false},
		{"multicast address", []string{"224.0.0.1"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newFakeAuthorizationServer(t)
			network := newNamedNetwork(server.Listener.Addr().String())
			network.resolveTo("api.internal.test", test.addresses...)
			client := namedAPISession(t, server.Server, "http", "api.internal.test", network)
			server.mu.Lock()
			server.validAccessTokens["access-token-1"] = true
			server.mu.Unlock()

			result, err := sendRequest(t, client, "named", oauth2http.SendRequestInput{Path: "/me"})

			require.NoError(t, err)
			if test.isPermitted {
				require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch, "failure: %+v", result.Failure)
				require.Len(t, server.requests(), 1)
				dialed := network.dialedAddresses()
				require.Len(t, dialed, 1)
				require.Equal(t, net.JoinHostPort(netip.MustParseAddr(test.addresses[0]).Unmap().String(), serverPort(t, server.Server)), dialed[0],
					"the dialer connects to the address it checked, not to the name")
				return
			}
			require.Equal(t, oauth2http.SendRequestBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, "apiBaseUrl")
			require.Contains(t, result.Failure.Message, "will not connect")
			require.Empty(t, network.dialedAddresses(), "a refused address is never dialed")
			require.Empty(t, server.requests())
		})
	}
}

func TestHTTPSAcceptsPublicAddressesButNeverLinkLocalOnes(t *testing.T) {
	for _, test := range []struct {
		name        string
		addresses   []string
		isPermitted bool
	}{
		{"public IPv4", []string{"8.8.8.8"}, true},
		{"private IPv4", []string{"10.1.2.3"}, true},
		{"loopback", []string{"127.0.0.1"}, true},
		{"cloud metadata address", []string{"169.254.169.254"}, false},
		{"IPv6 link-local", []string{"fe80::1"}, false},
		{"IPv4-mapped metadata address", []string{"::ffff:169.254.169.254"}, false},
		{"IPv6 metadata address", []string{"fd00:ec2::254"}, false},
		{"public address together with the metadata address", []string{"8.8.8.8", "169.254.169.254"}, false},
		{"unspecified address", []string{"::"}, false},
	} {
		expectedReason := "link-local"
		if test.name == "unspecified address" {
			expectedReason = "only unicast"
		}
		t.Run(test.name, func(t *testing.T) {
			// The test certificate of httptest is valid for example.com.
			server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"sub":"user-1"}`))
			}))
			t.Cleanup(server.Close)
			network := newNamedNetwork(server.Listener.Addr().String())
			network.resolveTo("example.com", test.addresses...)
			client := namedAPISession(t, server, "https", "example.com", network)

			result, err := sendRequest(t, client, "secure", oauth2http.SendRequestInput{Path: "/me"})

			require.NoError(t, err)
			if test.isPermitted {
				require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch, "failure: %+v", result.Failure)
				require.Len(t, network.dialedAddresses(), 1)
				return
			}
			require.Equal(t, oauth2http.SendRequestBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, expectedReason)
			require.Empty(t, network.dialedAddresses())
		})
	}
}

func TestTheTokenEndpointFollowsTheSameAddressPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		address     string
		isPermitted bool
	}{
		{"private address", "10.0.0.7", true},
		{"public address", "8.8.8.8", false},
		{"cloud metadata address", "169.254.169.254", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newFakeAuthorizationServer(t)
			access, refresh := server.issueTokens()
			network := newNamedNetwork(server.Listener.Addr().String())
			network.resolveTo("auth.internal.test", test.address)
			network.resolveTo("api.internal.test", "127.0.0.1")
			expiresAt := testStart.Add(-time.Minute) // The stored token has expired, so the call refreshes first.
			credentials := testsupport.NewRefreshingCredentials(testCredentials(access, refresh), &expiresAt, func() time.Time { return testStart })
			client, err := oauth2http.New(oauth2http.Config{
				AuthorizationEndpoint: "http://auth.internal.test/authorize", TokenEndpoint: "http://auth.internal.test:" + serverPort(t, server.Server) + "/token",
				Scopes: []string{"openid"}, APIBaseURL: "http://api.internal.test:" + serverPort(t, server.Server),
			}, credentials, oauth2http.WithClock(func() time.Time { return testStart }), network.option())
			require.NoError(t, err)

			result, err := sendRequest(t, client, "refresh", oauth2http.SendRequestInput{Path: "/me"})

			require.NoError(t, err)
			if test.isPermitted {
				require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch, "failure: %+v", result.Failure)
				require.Len(t, server.refreshRequests(), 1)
				return
			}
			require.Equal(t, oauth2http.SendRequestBranchDefect, result.Branch, "a refused token endpoint is not retried")
			require.Contains(t, result.Failure.Message, "tokenEndpoint")
			require.Empty(t, server.refreshRequests())
			require.Empty(t, server.requests(), "no API request is sent without a token")
		})
	}
}

func TestDefaultPolicyRefusesLiteralAddressesBeforeAnyConnection(t *testing.T) {
	for _, endpoint := range []string{
		"http://8.8.8.8/token", "https://169.254.169.254/latest/api/token", "http://169.254.169.254/latest/api/token",
		"http://[fe80::1]/token", "https://[::ffff:169.254.169.254]/token", "http://0.0.0.0:8080/token",
	} {
		t.Run(endpoint, func(t *testing.T) {
			driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: endpoint, RequestTimeout: time.Second}, nil)
			require.NoError(t, err)

			_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[oauth2http.Credentials]{
				Credentials: testCredentials("old", "refresh"), Now: testStart,
			})

			require.Error(t, err)
			require.False(t, sdkgo.IsReauthorizationRequired(err))
			require.ErrorContains(t, err, "address not allowed")
		})
	}
}

func TestWithHTTPClientKeepsTLSSettingsAndIgnoresProxies(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"seen":"` + request.Header.Get("Authorization")[:len("Bearer ")] + `"}`))
	}))
	t.Cleanup(server.Close)
	proxyWasUsed := false
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { proxyWasUsed = true }))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	transport := server.Client().Transport.(*http.Transport)
	transport.Proxy = http.ProxyURL(proxyURL)
	expiresAt := testStart.Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentials(testCredentials("access-token-1", "refresh-token-1"), &expiresAt, func() time.Time { return testStart })
	client, err := oauth2http.New(oauth2http.Config{
		AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token", Scopes: []string{"openid"}, APIBaseURL: server.URL,
	}, credentials, oauth2http.WithClock(func() time.Time { return testStart }), oauth2http.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	result, err := sendRequest(t, client, "tls", oauth2http.SendRequestInput{Path: "/me"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch, "the caller's root certificates still verify the server; failure: %+v", result.Failure)
	require.False(t, proxyWasUsed, "a proxy would receive the connection instead of the address the dialer checked")
}

func TestNewRejectsAnHTTPClientTransportItCannotGuard(t *testing.T) {
	_, err := oauth2http.New(validConfig(), staticCredentials(), oauth2http.WithHTTPClient(&http.Client{Transport: roundTripperFunc(nil)}))
	require.ErrorContains(t, err, "*http.Transport")
	_, err = oauth2http.NewCredentialRefreshDriver(validConfig(), &http.Client{Transport: roundTripperFunc(nil)})
	require.ErrorContains(t, err, "*http.Transport")
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
