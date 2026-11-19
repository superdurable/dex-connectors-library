// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "web-feed", Name: "web-feed-public"}
	testClockTime  = time.Date(2026, time.November, 10, 10, 0, 0, 0, time.UTC)
	// publicTestAddress is a documentation address that the public address policy treats as public.
	publicTestAddress = netip.MustParseAddr("203.0.113.10")
)

// testHostAddresses decides what each test host resolves to; every other host resolves to publicTestAddress.
var testHostAddresses = map[string]netip.Addr{
	"internal.example.com":    netip.MustParseAddr("10.0.0.5"),
	"loopback.example.com":    netip.MustParseAddr("127.0.0.1"),
	"metadata.example.com":    netip.MustParseAddr("169.254.169.254"),
	"shared.example.com":      netip.MustParseAddr("100.64.0.9"),
	"ipv6meta.example.com":    netip.MustParseAddr("fd00:ec2::254"),
	"uniquelocal.example.com": netip.MustParseAddr("fd12:3456::1"),
}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "web-feed-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type clockTestOption struct{ clock func() time.Time }

func (option clockTestOption) applyClientOption(options *clientOptions) { options.clock = option.clock }

type addressResolutionTestOption struct {
	resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	dial    func(ctx context.Context, network string, address string) (net.Conn, error)
}

func (option addressResolutionTestOption) applyClientOption(options *clientOptions) {
	options.newPolicyClient = func(caller *http.Client, requestTimeout time.Duration) (*http.Client, error) {
		return providerhttp.NewAddressPolicyClientForTest(caller, requestTimeout, providerhttp.PublicHTTPS, option.resolve, option.dial)
	}
}

// recordedRequest is the part of one request a test asserts.
type recordedRequest struct {
	host   string
	path   string
	header http.Header
}

// testSite is one TLS server that answers for every *.example.com host, routed by host and path.
type testSite struct {
	server   *httptest.Server
	mutex    sync.Mutex
	routes   map[string]http.HandlerFunc
	requests []recordedRequest
	dials    []string
}

func newTestSite(t *testing.T) *testSite {
	t.Helper()
	site := &testSite{routes: map[string]http.HandlerFunc{}}
	site.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mutex.Lock()
		site.requests = append(site.requests, recordedRequest{host: r.Host, path: r.URL.RequestURI(), header: r.Header.Clone()})
		handler, exists := site.routes[r.Host+r.URL.Path]
		site.mutex.Unlock()
		if !exists {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	}))
	// Certificate and dropped-connection tests make the server log expected handshake errors.
	site.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	site.server.StartTLS()
	t.Cleanup(site.server.Close)
	return site
}

// handle registers a handler for an https URL such as https://blog.example.com/feed.xml.
func (site *testSite) handle(rawURL string, handler http.HandlerFunc) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.routes[strings.TrimPrefix(rawURL, "https://")] = handler
}

// serve registers a fixed answer with the given content type.
func (site *testSite) serve(rawURL, contentType, body string) {
	site.handle(rawURL, func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write([]byte(body))
	})
}

func (site *testSite) recordedRequests() []recordedRequest {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	return append([]recordedRequest(nil), site.requests...)
}

func (site *testSite) dialedAddresses() []string {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	return append([]string(nil), site.dials...)
}

func (site *testSite) resolve(_ context.Context, host string) ([]netip.Addr, error) {
	if host == "missing.example.com" {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	if address, exists := testHostAddresses[host]; exists {
		return []netip.Addr{address}, nil
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address}, nil
	}
	return []netip.Addr{publicTestAddress}, nil
}

// dial records the checked address and connects to the test server instead.
func (site *testSite) dial(ctx context.Context, network string, address string) (net.Conn, error) {
	site.mutex.Lock()
	site.dials = append(site.dials, address)
	site.mutex.Unlock()
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, site.server.Listener.Addr().String())
}

func (site *testSite) newClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := New(config, nil,
		WithHTTPClient(site.server.Client()),
		addressResolutionTestOption{resolve: site.resolve, dial: site.dial},
		clockTestOption{clock: func() time.Time { return testClockTime }},
	)
	require.NoError(t, err)
	return client
}

func runQuery[IN, OUT any](t *testing.T, operation sdkgo.Query[IN, OUT], input IN) (sdkgo.QueryResult[OUT], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, operation, testConnection, input)
}

func requireBranch[OUT any](t *testing.T, result sdkgo.QueryResult[OUT], err error, branch sdkgo.BranchID, kind sdkgo.FailureKind) {
	t.Helper()
	require.NoError(t, err)
	require.Equal(t, branch, result.Branch)
	if kind == "" {
		require.Nil(t, result.Failure)
		return
	}
	require.NotNil(t, result.Failure)
	require.Equal(t, kind, result.Failure.Kind)
	require.Equal(t, "web-feed", result.Failure.Provider)
}

func timestamp(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return &parsed
}
