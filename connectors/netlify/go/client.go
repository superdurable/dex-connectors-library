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

// GetSiteInput identifies one Netlify site by Project ID or accepted domain.
type GetSiteInput struct {
	// SiteID is the Project ID or accepted site domain.
	SiteID string `json:"siteId"`
}

// GetDeployInput identifies one deploy within a Netlify site.
type GetDeployInput struct {
	// SiteID is the Project ID or accepted site domain.
	SiteID string `json:"siteId"`
	// DeployID is the deploy identifier from a deploy listing.
	DeployID string `json:"deployId"`
}

// GetSiteOperation reads one site.
type GetSiteOperation struct{ client *Client }

// GetSite binds a site read to this client.
func (client *Client) GetSite() GetSiteOperation { return GetSiteOperation{client} }

// Definition returns the generated getSite contract.
func (GetSiteOperation) Definition() sdkgo.QueryDefinition { return GetSiteDefinition }

// Invoke reads the requested site and classifies the response.
func (operation GetSiteOperation) Invoke(call sdkgo.Call, input GetSiteInput) sdkgo.QueryAttempt[Site] {
	if strings.TrimSpace(input.SiteID) == "" {
		return sdkgo.NewQueryBranch(GetSiteBranchDefect, Site{}, failure(sdkgo.FailureValidation, "getSite", "siteId is required"), sdkgo.Receipt{})
	}
	spec := requestSpec{method: http.MethodGet, path: "/sites/" + url.PathEscape(input.SiteID)}
	return executeQuery(operation.client, call, "getSite", spec, GetSiteBranchFound, GetSiteBranchNotFound, GetSiteBranchProviderRejected, GetSiteBranchInvalidResponse, GetSiteBranchDefect, decodeSite)
}

func decodeSite(body []byte) (Site, error) {
	var site Site
	if err := json.Unmarshal(body, &site); err != nil {
		return Site{}, err
	}
	if site.ID == "" {
		return Site{}, fmt.Errorf("missing site ID")
	}
	return site, nil
}

// GetDeployOperation reads one deploy.
type GetDeployOperation struct{ client *Client }

// GetDeploy binds a deploy read to this client.
func (client *Client) GetDeploy() GetDeployOperation { return GetDeployOperation{client} }

// Definition returns the generated getDeploy contract.
func (GetDeployOperation) Definition() sdkgo.QueryDefinition { return GetDeployDefinition }

// Invoke reads the requested deploy and classifies the response.
func (operation GetDeployOperation) Invoke(call sdkgo.Call, input GetDeployInput) sdkgo.QueryAttempt[Deploy] {
	if strings.TrimSpace(input.SiteID) == "" || strings.TrimSpace(input.DeployID) == "" {
		return sdkgo.NewQueryBranch(GetDeployBranchDefect, Deploy{}, failure(sdkgo.FailureValidation, "getDeploy", "siteId and deployId are required"), sdkgo.Receipt{})
	}
	spec := requestSpec{method: http.MethodGet, path: "/sites/" + url.PathEscape(input.SiteID) + "/deploys/" + url.PathEscape(input.DeployID)}
	return executeQuery(operation.client, call, "getDeploy", spec, GetDeployBranchFound, GetDeployBranchNotFound, GetDeployBranchProviderRejected, GetDeployBranchInvalidResponse, GetDeployBranchDefect, decodeDeploy)
}

func decodeDeploy(body []byte) (Deploy, error) {
	var deploy Deploy
	if err := json.Unmarshal(body, &deploy); err != nil {
		return Deploy{}, err
	}
	if deploy.ID == "" {
		return Deploy{}, fmt.Errorf("missing deploy ID")
	}
	return deploy, nil
}

// DeleteSiteInput identifies the site to delete.
type DeleteSiteInput struct {
	// SiteID is the Netlify Project ID.
	SiteID string `json:"siteId"`
}

// DeletedSite identifies the site removed by Netlify.
type DeletedSite struct {
	// SiteID is the Netlify Project ID.
	SiteID string `json:"siteId"`
}

// DeleteSiteOperation deletes one site.
type DeleteSiteOperation struct{ client *Client }

// DeleteSite binds a site deletion to this client.
func (client *Client) DeleteSite() DeleteSiteOperation { return DeleteSiteOperation{client} }

// Definition returns the generated deleteSite contract.
func (DeleteSiteOperation) Definition() sdkgo.MutationDefinition { return DeleteSiteDefinition }

// IdempotencyKey derives a stable identity for the repeated deletion.
func (DeleteSiteOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSiteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the site and classifies the response.
func (operation DeleteSiteOperation) Invoke(call sdkgo.Call, input DeleteSiteInput) sdkgo.MutationAttempt[DeletedSite] {
	if strings.TrimSpace(input.SiteID) == "" {
		return sdkgo.NewMutationBranch(DeleteSiteBranchDefect, DeletedSite{}, failure(sdkgo.FailureValidation, "deleteSite", "siteId is required"), sdkgo.Receipt{})
	}
	response, localFailure, err := operation.client.send(call, "deleteSite", requestSpec{method: http.MethodDelete, path: "/sites/" + url.PathEscape(input.SiteID)})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(DeleteSiteBranchDefect, DeletedSite{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedSite](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "netlify", Operation: "deleteSite", Message: "Netlify request failed"}, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, "deleteSite")
		return sdkgo.NewMutationRetry[DeletedSite](retry, after)
	}
	if response.StatusCode == http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeleteSiteBranchDeleted, DeletedSite{SiteID: input.SiteID}, nil, sdkgo.Receipt{})
	}
	providerFailure := statusFailure(response, "deleteSite")
	if response.StatusCode == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteSiteBranchNotFound, DeletedSite{}, &providerFailure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteSiteBranchProviderRejected, DeletedSite{}, &providerFailure, sdkgo.Receipt{})
}

// CancelDeployInput identifies the deploy to cancel.
type CancelDeployInput struct {
	// DeployID is the deploy identifier from a deploy listing.
	DeployID string `json:"deployId"`
}

// CancelDeployOperation cancels one deploy.
type CancelDeployOperation struct{ client *Client }

// CancelDeploy binds a deploy cancellation to this client.
func (client *Client) CancelDeploy() CancelDeployOperation { return CancelDeployOperation{client} }

// Definition returns the generated cancelDeploy contract.
func (CancelDeployOperation) Definition() sdkgo.MutationDefinition { return CancelDeployDefinition }

// IdempotencyKey derives a stable identity for the repeated cancellation.
func (CancelDeployOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelDeployInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels the deploy and classifies the response.
func (operation CancelDeployOperation) Invoke(call sdkgo.Call, input CancelDeployInput) sdkgo.MutationAttempt[Deploy] {
	if strings.TrimSpace(input.DeployID) == "" {
		return sdkgo.NewMutationBranch(CancelDeployBranchDefect, Deploy{}, failure(sdkgo.FailureValidation, "cancelDeploy", "deployId is required"), sdkgo.Receipt{})
	}
	response, localFailure, err := operation.client.send(call, "cancelDeploy", requestSpec{method: http.MethodPost, path: "/deploys/" + url.PathEscape(input.DeployID) + "/cancel"})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(CancelDeployBranchDefect, Deploy{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Deploy](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "netlify", Operation: "cancelDeploy", Message: "Netlify request failed"}, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, "cancelDeploy")
		return sdkgo.NewMutationRetry[Deploy](retry, after)
	}
	if response.StatusCode != http.StatusCreated {
		providerFailure := statusFailure(response, "cancelDeploy")
		if response.StatusCode == http.StatusNotFound {
			return sdkgo.NewMutationBranch(CancelDeployBranchNotFound, Deploy{}, &providerFailure, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CancelDeployBranchProviderRejected, Deploy{}, &providerFailure, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(CancelDeployBranchInvalidResponse, Deploy{}, failure(sdkgo.FailureProtocol, "cancelDeploy", "response was malformed or oversized"), sdkgo.Receipt{})
	}
	deploy, err := decodeDeploy(body)
	if err != nil {
		return sdkgo.NewMutationBranch(CancelDeployBranchInvalidResponse, Deploy{}, failure(sdkgo.FailureProtocol, "cancelDeploy", "response was malformed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CancelDeployBranchCancelled, deploy, nil, sdkgo.Receipt{})
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
