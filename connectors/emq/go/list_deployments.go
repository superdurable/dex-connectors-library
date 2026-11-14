// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package emq

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ListDeploymentsInput has no operation parameters; the key's project scope selects deployments.
type ListDeploymentsInput struct{}

// Deployment is a bounded view of one EMQX Cloud deployment.
type Deployment struct {
	// DeploymentID is the provider's stable deployment identifier.
	DeploymentID string `json:"deploymentId"`
	// DeploymentName is the console display name.
	DeploymentName string `json:"deploymentName"`
	// DeploymentType identifies the serverless or dedicated plan.
	DeploymentType string `json:"deploymentType"`
	// ProjectName is the containing project name.
	ProjectName string `json:"projectName"`
	// Region is the provider's region label.
	Region string `json:"region"`
	// Status is the provider-reported lifecycle state.
	Status string `json:"status"`
	// Version is the EMQX major version label.
	Version string `json:"version"`
	// Platform is the cloud provider label.
	Platform string `json:"platform"`
	// Connections is the deployment's connection capacity.
	Connections int64 `json:"connections"`
	// TPS is the deployment's transactions-per-second capacity.
	TPS int64 `json:"tps"`
	// CreatedAt is the provider's creation time string.
	CreatedAt string `json:"createdAt"`
}

// ListDeploymentsOutput contains zero or more scoped deployments.
type ListDeploymentsOutput struct {
	// Deployments is empty when the key can access no deployments.
	Deployments []Deployment `json:"deployments"`
}

// ListDeploymentsOperation reads scoped deployment metadata.
type ListDeploymentsOperation struct{ client *Client }

// Definition returns the generated deployment-list contract.
func (ListDeploymentsOperation) Definition() sdkgo.QueryDefinition { return ListDeploymentsDefinition }

// Invoke makes one read-only Platform API call and classifies its outcome.
func (operation ListDeploymentsOperation) Invoke(call sdkgo.Call, _ ListDeploymentsInput) sdkgo.QueryAttempt[ListDeploymentsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidBasicCredential(credentials.PlatformAPIKey.Reveal()) || !isValidBasicCredential(credentials.PlatformAPISecret.Reveal()) || strings.Contains(credentials.PlatformAPIKey.Reveal(), ":") {
		return deploymentBranch(ListDeploymentsBranchDefect, sdkgo.FailureAuthentication, "Platform API credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.endpoint+"/deployments", nil)
	if err != nil {
		return deploymentBranch(ListDeploymentsBranchDefect, sdkgo.FailureValidation, "deployment-list request is invalid")
	}
	request.SetBasicAuth(credentials.PlatformAPIKey.Reveal(), credentials.PlatformAPISecret.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return deploymentRetry(sdkgo.FailureAvailability, "EMQX Cloud request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return deploymentRetry(sdkgo.FailureRateLimit, "EMQX Cloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return deploymentRetry(sdkgo.FailureAvailability, "EMQX Cloud is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return deploymentBranch(ListDeploymentsBranchProviderRejected, kind, "EMQX Cloud rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return deploymentBranch(ListDeploymentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "EMQX Cloud response exceeded the configured limit")
	}
	if err != nil {
		return deploymentRetry(sdkgo.FailureAvailability, "EMQX Cloud response was interrupted", 0)
	}
	output, err := decodeDeployments(body)
	if err != nil {
		return deploymentBranch(ListDeploymentsBranchInvalidResponse, sdkgo.FailureProtocol, "EMQX Cloud returned an invalid deployment list")
	}
	return sdkgo.NewQueryBranch(ListDeploymentsBranchListed, output, nil, sdkgo.Receipt{})
}
func decodeDeployments(body []byte) (ListDeploymentsOutput, error) {
	var wire []struct {
		Connections    *int64 `json:"connections"`
		TPS            *int64 `json:"tps"`
		CreatedAt      string `json:"createAt"`
		DeploymentID   string `json:"deploymentID"`
		DeploymentName string `json:"deploymentName"`
		DeploymentType string `json:"deploymentType"`
		Platform       string `json:"platform"`
		ProjectName    string `json:"projectName"`
		Region         string `json:"region"`
		Status         string `json:"status"`
		Version        string `json:"version"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire == nil {
		return ListDeploymentsOutput{}, errors.New("invalid deployment list")
	}
	output := ListDeploymentsOutput{Deployments: make([]Deployment, 0, len(wire))}
	for _, item := range wire {
		if item.DeploymentID == "" || item.DeploymentName == "" || item.DeploymentType == "" || item.Status == "" || item.Connections == nil || item.TPS == nil || *item.Connections < 0 || *item.TPS < 0 {
			return ListDeploymentsOutput{}, errors.New("invalid deployment")
		}
		output.Deployments = append(output.Deployments, Deployment{DeploymentID: item.DeploymentID, DeploymentName: item.DeploymentName, DeploymentType: item.DeploymentType, ProjectName: item.ProjectName, Region: item.Region, Status: item.Status, Version: item.Version, Platform: item.Platform, Connections: *item.Connections, TPS: *item.TPS, CreatedAt: item.CreatedAt})
	}
	return output, nil
}
func deploymentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListDeploymentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "emq", Operation: "listDeployments", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDeploymentsOutput{Deployments: []Deployment{}}, &failure, sdkgo.Receipt{})
}
func deploymentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDeploymentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "emq", Operation: "listDeployments", Message: message}
	return sdkgo.NewQueryRetry[ListDeploymentsOutput](failure, delay)
}
