// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lambda

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var functionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// GetFunctionInput identifies one Lambda function in the configured Region.
type GetFunctionInput struct {
	FunctionName string `json:"functionName"`
}

// FunctionDetails returns the stable identity of a Lambda function.
type FunctionDetails struct {
	Name string `json:"name"`
	ARN  string `json:"arn"`
}

// GetFunctionOperation reads one function's configuration.
type GetFunctionOperation struct{ client *Client }

// GetFunction binds the Query to this client.
func (client *Client) GetFunction() GetFunctionOperation { return GetFunctionOperation{client} }

// Definition returns the generated Query contract.
func (GetFunctionOperation) Definition() sdkgo.QueryDefinition { return GetFunctionDefinition }

// Invoke executes one signed GetFunction request and classifies its outcome.
func (operation GetFunctionOperation) Invoke(call sdkgo.Call, input GetFunctionInput) sdkgo.QueryAttempt[FunctionDetails] {
	if !functionNamePattern.MatchString(input.FunctionName) {
		return functionBranch(GetFunctionBranchDefect, sdkgo.FailureValidation, "Lambda function name is invalid")
	}
	response := operation.client.getFunction(call, input.FunctionName)
	if errors.Is(response.err, errCredential) {
		return functionBranch(GetFunctionBranchDefect, sdkgo.FailureAuthentication, "Lambda credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[FunctionDetails](functionFailure(sdkgo.FailureTransport, "Lambda request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[FunctionDetails](functionFailure(sdkgo.FailureRateLimit, "Lambda rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[FunctionDetails](functionFailure(sdkgo.FailureAvailability, "Lambda is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetFunctionBranchMissing, FunctionDetails{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return functionBranch(GetFunctionBranchProviderRejected, sdkgo.FailureProviderRejection, "Lambda rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return functionBranch(GetFunctionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lambda response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[FunctionDetails](functionFailure(sdkgo.FailureTransport, "Lambda response was interrupted"), 0)
	}
	var wire struct {
		Configuration struct {
			FunctionName string `json:"FunctionName"`
			FunctionARN  string `json:"FunctionArn"`
		} `json:"Configuration"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Configuration.FunctionName != input.FunctionName || wire.Configuration.FunctionARN == "" {
		return functionBranch(GetFunctionBranchInvalidResponse, sdkgo.FailureProtocol, "Lambda returned invalid function JSON")
	}
	return sdkgo.NewQueryBranch(GetFunctionBranchFound, FunctionDetails{Name: wire.Configuration.FunctionName, ARN: wire.Configuration.FunctionARN}, nil, sdkgo.Receipt{})
}
func functionFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-lambda", Operation: "getFunction", Message: message}
}
func functionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[FunctionDetails] {
	failure := functionFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, FunctionDetails{}, &failure, sdkgo.Receipt{})
}
