// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package oauth2http connects Dex applications to any HTTP API that an OAuth 2.0 authorization server
// protects.
//
// The connection settings name the server's authorization and token endpoints, the scopes, and the API
// base URL; the credential holds the OAuth client ID, the client secret, and the tokens. Dex Web
// authorizes the connection with the authorization code flow and PKCE. The sendRequest Mutation then
// sends one HTTP request with the access token as a Bearer credential and returns the status code and
// body. CredentialRefreshDriver refreshes the access token before it expires and once after the API
// rejects it, and it follows a server that invalidates a refresh token when it is used.
//
// The token is sent only to the API base URL's origin, redirects are never followed, and the response
// body is bounded. Endpoints that the connection owner enters may use plain http, because the library
// cannot know where the server runs. The dialer then connects only when every address the host resolves
// to is loopback or private, and never to a link-local address such as the cloud metadata address, even
// over https. Tokens cross a private network unencrypted.
package oauth2http

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "OAuth 2.0 API"
	userAgent    = "superdurable-dex-oauth2-http-connector/0.1"
	// operationTimeout bounds every request and token refresh of one operation below the 30-second Execute timeout.
	operationTimeout = 25 * time.Second
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	// newHTTPClient builds the guarded client from the caller's; tests replace name resolution and dialing.
	newHTTPClient func(caller *http.Client, requestTimeout time.Duration) (*http.Client, error)
}

// WithHTTPClient overrides the default HTTP client. The caller retains ownership of the client and its
// transport, which must be nil or an *http.Transport; New fails for another transport, because the
// connector must own the dialer. The connector uses a copy that never follows a redirect, so a token is
// never replayed to another host, and that applies the connection's requestTimeout when the client sets
// none. The copy keeps the transport's TLS and pool settings, ignores its proxy settings, and connects
// only as the address policy allows: plain http to loopback and private addresses, https to any address
// except link-local ones, which are never allowed. The same client also carries token refresh requests.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock overrides the clock used for Receipts and token expiry. Tests use it; production
// connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client sends authenticated HTTP requests for one connection configuration. It is safe for
// concurrent use by several Steps.
type Client struct {
	credentials    CredentialSource
	refreshDriver  *CredentialRefreshDriver
	httpClient     *http.Client
	apiBaseURL     *url.URL
	maxBodyBytes   int64
	requestTimeout time.Duration
	now            func() time.Time
}

// New validates config and constructs a Client. Blank fields take their manifest defaults. Credentials
// are resolved before every request, so a replaced or refreshed token takes effect without a restart; the
// settings are startup configuration. Construction never contacts the API or the authorization server.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("OAuth 2.0 HTTP credential provider is required")
	}
	dependencies := clientOptions{now: time.Now, newHTTPClient: newPrivateNetworkHTTPClient}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("OAuth 2.0 HTTP connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("OAuth 2.0 HTTP connector clock is required")
	}
	if config.MaxBodyBytes < 1 || config.RequestTimeout < time.Second {
		return nil, errors.New("OAuth 2.0 HTTP maxBodyBytes must be positive and requestTimeout at least one second")
	}
	if _, err := parseHTTPURL(config.AuthorizationEndpoint); err != nil {
		return nil, fmt.Errorf("OAuth 2.0 HTTP authorizationEndpoint: %w", err)
	}
	apiBaseURL, err := parseAPIBaseURL(config.APIBaseURL)
	if err != nil {
		return nil, err
	}
	httpClient, err := dependencies.newHTTPClient(dependencies.httpClient, config.RequestTimeout)
	if err != nil {
		return nil, err
	}
	refreshDriver, err := NewCredentialRefreshDriver(config, httpClient)
	if err != nil {
		return nil, err
	}
	refreshDriver.now = dependencies.now
	return &Client{
		credentials: credentials, refreshDriver: refreshDriver, httpClient: httpClient, apiBaseURL: apiBaseURL,
		maxBodyBytes: config.MaxBodyBytes, requestTimeout: config.RequestTimeout, now: dependencies.now,
	}, nil
}

// SendRequest returns the sendRequest Mutation bound to this client.
func (client *Client) SendRequest() SendRequestOperation {
	return SendRequestOperation{client: client}
}

// parseAPIBaseURL accepts an absolute http or https URL without user information, a query, or a fragment.
// The scheme is the connection owner's choice, so plain http is accepted.
func parseAPIBaseURL(value string) (*url.URL, error) {
	parsed, err := parseHTTPURL(value)
	if err != nil {
		return nil, fmt.Errorf("OAuth 2.0 HTTP apiBaseUrl: %w", err)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, errors.New("OAuth 2.0 HTTP apiBaseUrl cannot contain a query")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return parsed, nil
}

// parseHTTPURL accepts printable ASCII without spaces that is an absolute http or https URL with a host,
// no user information, and no fragment. Error messages never repeat the value.
func parseHTTPURL(value string) (*url.URL, error) {
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return nil, errors.New("URL must be printable ASCII without spaces")
		}
	}
	if strings.Contains(value, "#") {
		return nil, errors.New("URL cannot contain a fragment")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return nil, errors.New("URL must be an absolute http or https URL with a host")
	}
	if parsed.User != nil {
		return nil, errors.New("URL cannot contain user information")
	}
	return parsed, nil
}

// newPrivateNetworkHTTPClient connects as providerhttp.PrivateNetworkHTTP allows: plain http only to loopback
// and private addresses, and never to link-local ones.
func newPrivateNetworkHTTPClient(caller *http.Client, requestTimeout time.Duration) (*http.Client, error) {
	return providerhttp.NewAddressPolicyClient(caller, requestTimeout, providerhttp.PrivateNetworkHTTP)
}

// describeRefusal returns the failure text for a refused connection of target, such as "apiBaseUrl".
func describeRefusal(target string, refusal *providerhttp.AddressRefusal) string {
	return fmt.Sprintf("the connection's %s resolves to an address the connector will not connect to: %s", target, refusal.Reason)
}

func sendRequestFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: SendRequestDefinition.Operation.OperationID, Message: message}
}
