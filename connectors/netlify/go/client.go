// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package netlify exposes typed Dex Steps for the Netlify REST API.
package netlify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies an HTTP transport without serializing it into a Dex Flow.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a client transport; New copies it and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable HTTP settings and resolves secrets per request.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Netlify.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Netlify credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Netlify base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

type requestSpec struct {
	method, path, contentType string
	query                     url.Values
	body                      []byte
}

// ListSitesInput requests the default Netlify sites page.
type ListSitesInput struct{}

// Site is a Netlify site summary.
type Site struct {
	// ID is the API site identifier, shown as Project ID in Netlify.
	ID string `json:"id"`
	// Name is the site name.
	Name string `json:"name"`
	// URL is the site's public URL.
	URL string `json:"url"`
	// AdminURL links to the Netlify management page.
	AdminURL string `json:"admin_url"`
}

// SitesPage contains the returned sites.
type SitesPage struct {
	// Sites contains one provider page, possibly empty.
	Sites []Site `json:"sites"`
}

// ListDeploysInput names the site whose deploys are listed.
type ListDeploysInput struct {
	// SiteID is the API site ID or accepted site domain.
	SiteID string `json:"siteId"`
}

// Deploy is a Netlify deploy summary.
type Deploy struct {
	// ID is the deployment identifier.
	ID string `json:"id"`
	// SiteID identifies its Netlify site.
	SiteID string `json:"site_id"`
	// State is the provider-reported deployment state.
	State string `json:"state"`
	// URL is the deployment URL.
	URL string `json:"url"`
}

// DeploysPage contains the returned deploys.
type DeploysPage struct {
	// Deploys contains one provider page, possibly empty.
	Deploys []Deploy `json:"deploys"`
}

// ListSitesOperation invokes listSites with one provider request.
type ListSitesOperation struct{ client *Client }

// ListSites binds the operation to this client.
func (client *Client) ListSites() ListSitesOperation { return ListSitesOperation{client} }

// Definition returns the generated listSites contract.
func (ListSitesOperation) Definition() sdkgo.QueryDefinition { return ListSitesDefinition }

// Invoke validates input, calls Netlify, and classifies the response.
func (operation ListSitesOperation) Invoke(call sdkgo.Call, input ListSitesInput) sdkgo.QueryAttempt[SitesPage] {
	spec, err := buildListSitesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListSitesBranchDefect, SitesPage{}, failure(sdkgo.FailureValidation, "listSites", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "listSites", spec, ListSitesBranchListed, "", ListSitesBranchProviderRejected, ListSitesBranchInvalidResponse, ListSitesBranchDefect, decodeListSites)
}
func buildListSitesRequest(input ListSitesInput) (requestSpec, error) {
	return requestSpec{method: http.MethodGet, path: "/sites"}, nil
}
func decodeListSites(body []byte) (SitesPage, error) {
	var items []Site
	if err := json.Unmarshal(body, &items); err != nil {
		return SitesPage{}, err
	}
	if items == nil {
		return SitesPage{}, fmt.Errorf("missing sites")
	}
	return SitesPage{Sites: items}, nil
}

// ListDeploysOperation invokes listDeploys with one provider request.
type ListDeploysOperation struct{ client *Client }

// ListDeploys binds the operation to this client.
func (client *Client) ListDeploys() ListDeploysOperation { return ListDeploysOperation{client} }

// Definition returns the generated listDeploys contract.
func (ListDeploysOperation) Definition() sdkgo.QueryDefinition { return ListDeploysDefinition }

// Invoke validates input, calls Netlify, and classifies the response.
func (operation ListDeploysOperation) Invoke(call sdkgo.Call, input ListDeploysInput) sdkgo.QueryAttempt[DeploysPage] {
	spec, err := buildListDeploysRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListDeploysBranchDefect, DeploysPage{}, failure(sdkgo.FailureValidation, "listDeploys", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "listDeploys", spec, ListDeploysBranchListed, ListDeploysBranchNotFound, ListDeploysBranchProviderRejected, ListDeploysBranchInvalidResponse, ListDeploysBranchDefect, decodeListDeploys)
}
func buildListDeploysRequest(input ListDeploysInput) (requestSpec, error) {
	if strings.TrimSpace(input.SiteID) == "" {
		return requestSpec{}, fmt.Errorf("siteId is required")
	}
	return requestSpec{method: http.MethodGet, path: "/sites/" + url.PathEscape(input.SiteID) + "/deploys"}, nil
}
func decodeListDeploys(body []byte) (DeploysPage, error) {
	var items []Deploy
	if err := json.Unmarshal(body, &items); err != nil {
		return DeploysPage{}, err
	}
	if items == nil {
		return DeploysPage{}, fmt.Errorf("missing deploys")
	}
	return DeploysPage{Deploys: items}, nil
}

func executeQuery[T any](client *Client, call sdkgo.Call, operation string, spec requestSpec, happy, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		localFailure.Operation = operation
		return sdkgo.NewQueryBranch(defect, zero, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "netlify", Operation: operation, Message: "Netlify request failed"}, 0)
	}
	// Closing the response cannot change its classified provider outcome.
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, operation)
		return sdkgo.NewQueryRetry[T](retry, after)
	}
	if response.StatusCode == http.StatusNotFound && notFound != "" {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewQueryBranch(notFound, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewQueryBranch(rejected, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return sdkgo.NewQueryBranch(invalid, zero, failure(sdkgo.FailureResponseTooLarge, operation, "response exceeded the configured limit"), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "netlify", Operation: operation, Message: "response was interrupted"}, 0)
	}
	value, err := decode(body)
	if err != nil {
		return sdkgo.NewQueryBranch(invalid, zero, failure(sdkgo.FailureProtocol, operation, "response was malformed"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(happy, value, nil, sdkgo.Receipt{})
}

func (client *Client) send(call sdkgo.Call, operation string, spec requestSpec) (*http.Response, *sdkgo.Failure, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, failure(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable"), nil
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return nil, failure(sdkgo.FailureAuthentication, operation, "connection credentials are invalid"), nil
	}
	endpoint := client.baseURL + spec.path
	if len(spec.query) != 0 {
		endpoint += "?" + spec.query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, spec.method, endpoint, bytes.NewReader(spec.body))
	if err != nil {
		return nil, failure(sdkgo.FailureLocalDefect, operation, "request could not be built"), nil
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	if spec.contentType != "" {
		request.Header.Set("Content-Type", spec.contentType)
	}
	response, err := client.httpClient.Do(request)
	return response, nil, err
}

func statusFailure(response *http.Response, operation string) sdkgo.Failure {
	kind := sdkgo.FailureProviderRejection
	switch response.StatusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		kind = sdkgo.FailureValidation
	}
	return sdkgo.Failure{Kind: kind, Provider: "netlify", Operation: operation, Message: fmt.Sprintf("Netlify returned HTTP %d", response.StatusCode)}
}

func shouldRetry(response *http.Response) bool {
	return response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError
}

func retryFailure(response *http.Response, operation string) (sdkgo.Failure, time.Duration) {
	kind := sdkgo.FailureAvailability
	if response.StatusCode == http.StatusTooManyRequests {
		kind = sdkgo.FailureRateLimit
	}
	return sdkgo.Failure{Kind: kind, Provider: "netlify", Operation: operation, Message: fmt.Sprintf("Netlify returned HTTP %d", response.StatusCode)}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
}

func failure(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "netlify", Operation: operation, Message: message}
}
