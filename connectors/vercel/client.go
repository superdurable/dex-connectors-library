// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package vercel exposes typed Dex Steps for the Vercel REST API.
package vercel

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

// New validates configuration without contacting Vercel.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Vercel credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Vercel base URL: %w", err)
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

// ListDeploymentsInput optionally selects a Vercel team ID.
type ListDeploymentsInput struct {
	// TeamID is the team ID; blank uses the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// Deployment is a Vercel deployment summary.
type Deployment struct {
	// UID is the deployment identifier returned by the list endpoint.
	UID string `json:"uid"`
	// ID is the identifier returned by some deployment detail responses.
	ID string `json:"id"`
	// Name is the Vercel project name.
	Name string `json:"name"`
	// URL is the deployment hostname without a scheme.
	URL string `json:"url"`
	// State is the list endpoint's deployment state.
	State string `json:"state"`
	// ReadyState is the detail endpoint's deployment state.
	ReadyState string `json:"readyState"`
	// Created is the creation timestamp in Unix milliseconds.
	Created int64 `json:"created"`
}

// DeploymentsPage contains Vercel's deployments and pagination cursor.
type DeploymentsPage struct {
	// Deployments holds the listed deployments.
	Deployments []Deployment `json:"deployments"`
	// Pagination contains the next cursor when more results exist.
	Pagination struct {
		Next *int64 `json:"next"`
	} `json:"pagination"`
}

// GetDeploymentInput identifies one deployment and an optional team.
type GetDeploymentInput struct {
	// IDOrURL is a deployment ID or hostname.
	IDOrURL string `json:"idOrUrl"`
	// TeamID is the team ID; blank uses the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// ListDeploymentsOperation invokes listDeployments with one provider request.
type ListDeploymentsOperation struct{ client *Client }

// ListDeployments binds the operation to this client.
func (client *Client) ListDeployments() ListDeploymentsOperation {
	return ListDeploymentsOperation{client}
}

// Definition returns the generated listDeployments contract.
func (ListDeploymentsOperation) Definition() sdkgo.QueryDefinition { return ListDeploymentsDefinition }

// Invoke validates input, calls Vercel, and classifies the response.
func (operation ListDeploymentsOperation) Invoke(call sdkgo.Call, input ListDeploymentsInput) sdkgo.QueryAttempt[DeploymentsPage] {
	spec, err := buildListDeploymentsRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListDeploymentsBranchDefect, DeploymentsPage{}, failure(sdkgo.FailureValidation, "listDeployments", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "listDeployments", spec, ListDeploymentsBranchListed, "", ListDeploymentsBranchProviderRejected, ListDeploymentsBranchInvalidResponse, ListDeploymentsBranchDefect, decodeListDeployments)
}
func buildListDeploymentsRequest(input ListDeploymentsInput) (requestSpec, error) {
	query := url.Values{}
	if input.TeamID != "" {
		query.Set("teamId", input.TeamID)
	}
	return requestSpec{method: http.MethodGet, path: "/v6/deployments", query: query}, nil
}
func decodeListDeployments(body []byte) (DeploymentsPage, error) {
	var value DeploymentsPage
	if err := json.Unmarshal(body, &value); err != nil {
		return DeploymentsPage{}, err
	}
	if value.Deployments == nil {
		return DeploymentsPage{}, fmt.Errorf("missing deployments")
	}
	return value, nil
}

// GetDeploymentOperation invokes getDeployment with one provider request.
type GetDeploymentOperation struct{ client *Client }

// GetDeployment binds the operation to this client.
func (client *Client) GetDeployment() GetDeploymentOperation { return GetDeploymentOperation{client} }

// Definition returns the generated getDeployment contract.
func (GetDeploymentOperation) Definition() sdkgo.QueryDefinition { return GetDeploymentDefinition }

// Invoke validates input, calls Vercel, and classifies the response.
func (operation GetDeploymentOperation) Invoke(call sdkgo.Call, input GetDeploymentInput) sdkgo.QueryAttempt[Deployment] {
	spec, err := buildGetDeploymentRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetDeploymentBranchDefect, Deployment{}, failure(sdkgo.FailureValidation, "getDeployment", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "getDeployment", spec, GetDeploymentBranchFound, GetDeploymentBranchNotFound, GetDeploymentBranchProviderRejected, GetDeploymentBranchInvalidResponse, GetDeploymentBranchDefect, decodeGetDeployment)
}
func buildGetDeploymentRequest(input GetDeploymentInput) (requestSpec, error) {
	if strings.TrimSpace(input.IDOrURL) == "" {
		return requestSpec{}, fmt.Errorf("idOrUrl is required")
	}
	query := url.Values{}
	if input.TeamID != "" {
		query.Set("teamId", input.TeamID)
	}
	return requestSpec{method: http.MethodGet, path: "/v13/deployments/" + url.PathEscape(input.IDOrURL), query: query}, nil
}
func decodeGetDeployment(body []byte) (Deployment, error) {
	var value Deployment
	if err := json.Unmarshal(body, &value); err != nil {
		return Deployment{}, err
	}
	if value.UID == "" && value.ID == "" {
		return Deployment{}, fmt.Errorf("missing deployment identifier")
	}
	return value, nil
}

func executeQuery[T any](client *Client, call sdkgo.Call, operation string, spec requestSpec, happy, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		localFailure.Operation = operation
		return sdkgo.NewQueryBranch(defect, zero, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "vercel", Operation: operation, Message: "Vercel request failed"}, 0)
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
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "vercel", Operation: operation, Message: "response was interrupted"}, 0)
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
	return sdkgo.Failure{Kind: kind, Provider: "vercel", Operation: operation, Message: fmt.Sprintf("Vercel returned HTTP %d", response.StatusCode)}
}

func shouldRetry(response *http.Response) bool {
	return response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError
}

func retryFailure(response *http.Response, operation string) (sdkgo.Failure, time.Duration) {
	kind := sdkgo.FailureAvailability
	if response.StatusCode == http.StatusTooManyRequests {
		kind = sdkgo.FailureRateLimit
	}
	return sdkgo.Failure{Kind: kind, Provider: "vercel", Operation: operation, Message: fmt.Sprintf("Vercel returned HTTP %d", response.StatusCode)}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
}

func failure(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "vercel", Operation: operation, Message: message}
}
