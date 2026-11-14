// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upyun

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetServiceUsageInput has no parameters; the connection selects the storage service.
type GetServiceUsageInput struct{}

// ServiceUsage is the number of bytes stored in the configured service.
type ServiceUsage struct {
	// Bytes is the nonnegative storage usage reported by UPYUN.
	Bytes int64 `json:"bytes"`
}

// GetServiceUsageOperation reads the storage service usage.
type GetServiceUsageOperation struct{ client *Client }

// GetServiceUsage binds the Query to this client.
func (client *Client) GetServiceUsage() GetServiceUsageOperation {
	return GetServiceUsageOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetServiceUsageOperation) Definition() sdkgo.QueryDefinition { return GetServiceUsageDefinition }

// Invoke requests usage and classifies provider responses.
func (operation GetServiceUsageOperation) Invoke(call sdkgo.Call, _ GetServiceUsageInput) sdkgo.QueryAttempt[ServiceUsage] {
	response := operation.client.getUsage(call)
	if errors.Is(response.err, errCredential) {
		return usageBranch(GetServiceUsageBranchDefect, sdkgo.FailureAuthentication, "UPYUN operator credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ServiceUsage](usageFailure(sdkgo.FailureTransport, "UPYUN request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ServiceUsage](usageFailure(sdkgo.FailureRateLimit, "UPYUN rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ServiceUsage](usageFailure(sdkgo.FailureAvailability, "UPYUN is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return usageBranch(GetServiceUsageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "UPYUN usage response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ServiceUsage](usageFailure(sdkgo.FailureTransport, "UPYUN response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return usageBranch(GetServiceUsageBranchProviderRejected, sdkgo.FailureProviderRejection, "UPYUN rejected HTTP "+strconv.Itoa(response.status))
	}
	text := strings.TrimSpace(string(response.body))
	if text == "" {
		return usageBranch(GetServiceUsageBranchInvalidResponse, sdkgo.FailureProtocol, "UPYUN usage response is empty")
	}
	bytes, err := strconv.ParseInt(text, 10, 64)
	if err != nil || bytes < 0 {
		return usageBranch(GetServiceUsageBranchInvalidResponse, sdkgo.FailureProtocol, "UPYUN returned invalid usage bytes")
	}
	return sdkgo.NewQueryBranch(GetServiceUsageBranchRead, ServiceUsage{Bytes: bytes}, nil, sdkgo.Receipt{})
}

func usageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "upyun", Operation: "getServiceUsage", Message: message}
}
func usageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ServiceUsage] {
	failure := usageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ServiceUsage{}, &failure, sdkgo.Receipt{})
}
