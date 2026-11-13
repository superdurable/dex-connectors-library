// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zilliz

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListClustersInput requests the first page of clusters visible to the API Key.
type ListClustersInput struct{}

// ClusterSummary is the nonsecret metadata of one Zilliz Cloud cluster.
type ClusterSummary struct {
	// ClusterID identifies the cluster.
	ClusterID string `json:"clusterID"`
	// Name is the cluster's display name.
	Name string `json:"name"`
	// RegionID is the cloud region identifier.
	RegionID string `json:"regionID"`
	// Plan is the subscription plan.
	Plan string `json:"plan"`
	// Status is the provider's cluster lifecycle status.
	Status string `json:"status"`
}

// ListClustersOutput contains the total count and first 25 clusters.
type ListClustersOutput struct {
	// Total is the provider's count of visible clusters.
	Total int `json:"total"`
	// Clusters are the first page of visible cluster summaries.
	Clusters []ClusterSummary `json:"clusters"`
}

// ListClustersOperation reads Zilliz Cloud control-plane cluster metadata.
type ListClustersOperation struct{ client *Client }

// Definition returns the generated cluster-list contract.
func (ListClustersOperation) Definition() sdkgo.QueryDefinition { return ListClustersDefinition }

// Invoke lists clusters without exposing connection addresses or descriptions.
func (operation ListClustersOperation) Invoke(call sdkgo.Call, _ ListClustersInput) sdkgo.QueryAttempt[ListClustersOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return clustersBranch(ListClustersBranchDefect, sdkgo.FailureAuthentication, "Zilliz API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, clusterListEndpoint, nil)
	if err != nil {
		return clustersBranch(ListClustersBranchDefect, sdkgo.FailureValidation, "Zilliz cluster request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return clustersRetry(sdkgo.FailureAvailability, "Zilliz cluster request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return clustersRetry(sdkgo.FailureRateLimit, "Zilliz rate limited the cluster request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return clustersRetry(sdkgo.FailureAvailability, "Zilliz cluster service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyClustersHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return clustersBranch(ListClustersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Zilliz response exceeded the configured limit")
	}
	if err != nil {
		return clustersRetry(sdkgo.FailureAvailability, "Zilliz response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			Count    *int `json:"count"`
			Clusters []struct {
				ClusterID   string `json:"clusterId"`
				ClusterName string `json:"clusterName"`
				RegionID    string `json:"regionId"`
				Plan        string `json:"plan"`
				Status      string `json:"status"`
			} `json:"clusters"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return clustersBranch(ListClustersBranchInvalidResponse, sdkgo.FailureProtocol, "Zilliz returned invalid cluster data")
	}
	if *wire.Code != 0 {
		return clustersBranch(ListClustersBranchProviderRejected, sdkgo.FailureProviderRejection, "Zilliz rejected the cluster request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data.Count == nil || *wire.Data.Count < 0 || wire.Data.Clusters == nil || len(wire.Data.Clusters) > 25 {
		return clustersBranch(ListClustersBranchInvalidResponse, sdkgo.FailureProtocol, "Zilliz returned invalid cluster list")
	}
	clusters := make([]ClusterSummary, 0, len(wire.Data.Clusters))
	for _, item := range wire.Data.Clusters {
		if item.ClusterID == "" || len(item.ClusterID) > 128 || strings.TrimSpace(item.ClusterID) != item.ClusterID || len(item.ClusterName) > 200 || len(item.RegionID) > 100 || len(item.Plan) > 100 || len(item.Status) > 100 {
			return clustersBranch(ListClustersBranchInvalidResponse, sdkgo.FailureProtocol, "Zilliz returned invalid cluster summary")
		}
		clusters = append(clusters, ClusterSummary{ClusterID: item.ClusterID, Name: item.ClusterName, RegionID: item.RegionID, Plan: item.Plan, Status: item.Status})
	}
	return sdkgo.NewQueryBranch(ListClustersBranchFound, ListClustersOutput{Total: *wire.Data.Count, Clusters: clusters}, nil, sdkgo.Receipt{})
}
func classifyClustersHTTPStatus(status int) sdkgo.QueryAttempt[ListClustersOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return clustersBranch(ListClustersBranchProviderRejected, kind, "Zilliz rejected cluster request with HTTP "+strconv.Itoa(status))
}
func clustersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListClustersOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zilliz", Operation: "listClusters", Message: message}
	return sdkgo.NewQueryBranch(branch, ListClustersOutput{}, &failure, sdkgo.Receipt{})
}
func clustersRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListClustersOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zilliz", Operation: "listClusters", Message: message}
	return sdkgo.NewQueryRetry[ListClustersOutput](failure, delay)
}
