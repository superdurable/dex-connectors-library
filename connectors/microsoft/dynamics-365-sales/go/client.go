// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package dynamics365sales exposes a Dataverse contact Query to Dex Flows.
package dynamics365sales

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var identifierPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Option configures a client dependency at construction time.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport for provider tests or an application proxy.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient: httpClient} }

type tokenEndpointOption struct{ endpoint string }

func (option tokenEndpointOption) applyClientOption(client *Client) {
	client.tokenEndpoint = option.endpoint
}

// WithTokenEndpoint supplies a test token endpoint; production applications should use the Entra endpoint.
func WithTokenEndpoint(endpoint string) Option { return tokenEndpointOption{endpoint: endpoint} }

// Client holds immutable environment settings and resolves the client secret for each call.
type Client struct {
	environmentURL   string
	tenantID         string
	clientID         string
	tokenEndpoint    string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the environment and Entra identifiers without contacting Microsoft.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Dynamics 365 Sales credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !identifierPattern.MatchString(config.TenantID) || !identifierPattern.MatchString(config.ClientID) {
		return nil, fmt.Errorf("Dynamics 365 Sales tenant and client IDs must be UUIDs")
	}
	environmentURL, err := providerhttp.ValidateBaseURL(config.EnvironmentURL)
	if err != nil {
		return nil, fmt.Errorf("Dynamics 365 Sales environment URL: %w", err)
	}
	parsedEnvironment, err := url.Parse(environmentURL)
	if err != nil || parsedEnvironment.Path != "" {
		return nil, fmt.Errorf("Dynamics 365 Sales environment URL must be an origin")
	}
	if !strings.HasSuffix(strings.ToLower(parsedEnvironment.Hostname()), ".dynamics.com") && !isLoopbackEnvironment(parsedEnvironment.Hostname()) {
		return nil, fmt.Errorf("Dynamics 365 Sales environment URL must be a Dataverse host")
	}
	client := &Client{environmentURL: environmentURL, tenantID: config.TenantID, clientID: config.ClientID, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Dynamics 365 Sales client option is required")
		}
		option.applyClientOption(client)
	}
	if client.tokenEndpoint == "" {
		client.tokenEndpoint = "https://login.microsoftonline.com/" + config.TenantID + "/oauth2/v2.0/token"
	}
	tokenURL, err := url.Parse(client.tokenEndpoint)
	if err != nil || tokenURL.Hostname() == "" || tokenURL.User != nil || tokenURL.RawQuery != "" || tokenURL.Fragment != "" || (tokenURL.Scheme != "https" && !(tokenURL.Scheme == "http" && isLoopbackEnvironment(tokenURL.Hostname()))) {
		return nil, fmt.Errorf("Dynamics 365 Sales token endpoint is invalid")
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

func isLoopbackEnvironment(hostname string) bool {
	return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

var errCredential = errors.New("Dynamics 365 Sales client credential unavailable")
var errRequest = errors.New("Dynamics 365 Sales request failed")
var errInvalidToken = errors.New("Dynamics 365 Sales token response invalid")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) queryContacts(call sdkgo.Call, emailAddress string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	clientSecret := credentials.ClientSecret.Reveal()
	if !providerhttp.IsHeaderSafeCredential(clientSecret) {
		return providerResponse{err: errCredential}
	}
	form := url.Values{"client_id": {client.clientID}, "client_secret": {clientSecret}, "scope": {client.environmentURL + "/.default"}, "grant_type": {"client_credentials"}}
	tokenRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := client.send(tokenRequest)
	if tokenResponse.status != http.StatusOK || tokenResponse.err != nil {
		return tokenResponse
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if json.Unmarshal(tokenResponse.body, &token) != nil || token.TokenType != "Bearer" || !providerhttp.IsHeaderSafeCredential(token.AccessToken) {
		return providerResponse{status: http.StatusOK, err: errInvalidToken}
	}
	filter := "emailaddress1 eq '" + strings.ReplaceAll(emailAddress, "'", "''") + "'"
	query := url.Values{"$select": {"contactid,fullname,emailaddress1"}, "$filter": {filter}, "$top": {"2"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.environmentURL+"/api/data/v9.2/contacts?"+query.Encode(), nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("OData-Version", "4.0")
	request.Header.Set("OData-MaxVersion", "4.0")
	return client.send(request)
}

func (client *Client) send(request *http.Request) providerResponse {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
