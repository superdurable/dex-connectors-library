// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lambda

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListFunctionsInput selects a bounded page of functions in the configured Region.
type ListFunctionsInput struct {
	// Marker resumes from a prior page's NextMarker; blank starts at the beginning.
	Marker string `json:"marker"`
	// MaxItems requests 1 to 50 functions; zero uses AWS's page size.
	MaxItems int `json:"maxItems"`
}

// FunctionPage contains one page of function identities and its continuation marker.
type FunctionPage struct {
	// Functions are the identities returned in this page.
	Functions []FunctionDetails `json:"functions"`
	// NextMarker resumes the next page; blank means the list is complete.
	NextMarker string `json:"nextMarker"`
}

// UpdateFunctionDescriptionInput sets the complete description of one function.
type UpdateFunctionDescriptionInput struct {
	// FunctionName identifies the unpublished function configuration.
	FunctionName string `json:"functionName"`
	// Description replaces the entire description; blank clears it.
	Description string `json:"description"`
}

// DeleteFunctionInput identifies a complete function to delete.
type DeleteFunctionInput struct {
	// FunctionName identifies the complete function and all its versions and aliases.
	FunctionName string `json:"functionName"`
}

// FunctionWriteResult confirms the function is absent after a deletion.
type FunctionWriteResult struct {
	// Applied confirms the function is absent.
	Applied bool `json:"applied"`
}

// ListFunctionsOperation lists a page of function identities.
type ListFunctionsOperation struct{ client *Client }

// UpdateFunctionDescriptionOperation sets a function's description.
type UpdateFunctionDescriptionOperation struct{ client *Client }

// DeleteFunctionOperation deletes a complete function.
type DeleteFunctionOperation struct{ client *Client }

// ListFunctions binds the Query to this client.
func (client *Client) ListFunctions() ListFunctionsOperation { return ListFunctionsOperation{client} }

// UpdateFunctionDescription binds the Mutation to this client.
func (client *Client) UpdateFunctionDescription() UpdateFunctionDescriptionOperation {
	return UpdateFunctionDescriptionOperation{client}
}

// DeleteFunction binds the Mutation to this client.
func (client *Client) DeleteFunction() DeleteFunctionOperation {
	return DeleteFunctionOperation{client}
}

// Definition returns the generated Query contract.
func (ListFunctionsOperation) Definition() sdkgo.QueryDefinition { return ListFunctionsDefinition }

// Definition returns the generated Mutation contract.
func (UpdateFunctionDescriptionOperation) Definition() sdkgo.MutationDefinition {
	return UpdateFunctionDescriptionDefinition
}

// Definition returns the generated Mutation contract.
func (DeleteFunctionOperation) Definition() sdkgo.MutationDefinition { return DeleteFunctionDefinition }

// IdempotencyKey records the Dex call identity for this absolute update.
func (UpdateFunctionDescriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateFunctionDescriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the Dex call identity for idempotent deletion.
func (DeleteFunctionOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFunctionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded function page.
func (operation ListFunctionsOperation) Invoke(call sdkgo.Call, input ListFunctionsInput) sdkgo.QueryAttempt[FunctionPage] {
	if len(input.Marker) > 2048 || strings.ContainsAny(input.Marker, "\r\n") || input.MaxItems < 0 || input.MaxItems > 50 {
		return functionPageBranch(ListFunctionsBranchDefect, sdkgo.FailureValidation, "Lambda page input is invalid")
	}
	parameters := url.Values{}
	if input.Marker != "" {
		parameters.Set("Marker", input.Marker)
	}
	if input.MaxItems > 0 {
		parameters.Set("MaxItems", strconv.Itoa(input.MaxItems))
	}
	path := "/2015-03-31/functions"
	if len(parameters) > 0 {
		path += "?" + parameters.Encode()
	}
	response := operation.client.send(call, http.MethodGet, path, nil)
	if errors.Is(response.err, errCredential) {
		return functionPageBranch(ListFunctionsBranchDefect, sdkgo.FailureAuthentication, "Lambda credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[FunctionPage](functionPageFailure(sdkgo.FailureTransport, "Lambda request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[FunctionPage](functionPageFailure(sdkgo.FailureRateLimit, "Lambda rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[FunctionPage](functionPageFailure(sdkgo.FailureAvailability, "Lambda is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return functionPageBranch(ListFunctionsBranchProviderRejected, sdkgo.FailureProviderRejection, "Lambda rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return functionPageBranch(ListFunctionsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lambda response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[FunctionPage](functionPageFailure(sdkgo.FailureTransport, "Lambda response was interrupted"), 0)
	}
	var wire struct {
		Functions []struct {
			Name string `json:"FunctionName"`
			ARN  string `json:"FunctionArn"`
		} `json:"Functions"`
		NextMarker string `json:"NextMarker"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Functions == nil || len(wire.Functions) > 50 || len(wire.NextMarker) > 2048 {
		return functionPageBranch(ListFunctionsBranchInvalidResponse, sdkgo.FailureProtocol, "Lambda returned invalid function page JSON")
	}
	page := FunctionPage{Functions: make([]FunctionDetails, 0, len(wire.Functions)), NextMarker: wire.NextMarker}
	for _, item := range wire.Functions {
		if item.Name == "" || item.ARN == "" {
			return functionPageBranch(ListFunctionsBranchInvalidResponse, sdkgo.FailureProtocol, "Lambda returned invalid function identity")
		}
		page.Functions = append(page.Functions, FunctionDetails{Name: item.Name, ARN: item.ARN})
	}
	return sdkgo.NewQueryBranch(ListFunctionsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke sets an absolute function description.
func (operation UpdateFunctionDescriptionOperation) Invoke(call sdkgo.Call, input UpdateFunctionDescriptionInput) sdkgo.MutationAttempt[FunctionDetails] {
	if !functionNamePattern.MatchString(input.FunctionName) || len(input.Description) > 256 {
		return functionUpdateBranch(UpdateFunctionDescriptionBranchDefect, sdkgo.FailureValidation, "Lambda function name or description is invalid")
	}
	body, err := json.Marshal(struct {
		Description string `json:"Description"`
	}{input.Description})
	if err != nil {
		return functionUpdateBranch(UpdateFunctionDescriptionBranchDefect, sdkgo.FailureValidation, "Lambda description could not be encoded")
	}
	response := operation.client.send(call, http.MethodPut, "/2015-03-31/functions/"+input.FunctionName+"/configuration", body)
	if errors.Is(response.err, errCredential) {
		return functionUpdateBranch(UpdateFunctionDescriptionBranchDefect, sdkgo.FailureAuthentication, "Lambda credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[FunctionDetails](functionUpdateFailure(sdkgo.FailureTransport, "Lambda request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusConflict {
		return sdkgo.NewMutationRetry[FunctionDetails](functionUpdateFailure(sdkgo.FailureRateLimit, "Lambda cannot update the function yet"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[FunctionDetails](functionUpdateFailure(sdkgo.FailureAvailability, "Lambda is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(UpdateFunctionDescriptionBranchMissing, FunctionDetails{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return functionUpdateBranch(UpdateFunctionDescriptionBranchProviderRejected, sdkgo.FailureProviderRejection, "Lambda rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return functionUpdateBranch(UpdateFunctionDescriptionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lambda response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[FunctionDetails](functionUpdateFailure(sdkgo.FailureTransport, "Lambda response was interrupted"), 0)
	}
	var wire struct {
		Name string `json:"FunctionName"`
		ARN  string `json:"FunctionArn"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Name != input.FunctionName || wire.ARN == "" {
		return functionUpdateBranch(UpdateFunctionDescriptionBranchInvalidResponse, sdkgo.FailureProtocol, "Lambda returned invalid function JSON")
	}
	return sdkgo.NewMutationBranch(UpdateFunctionDescriptionBranchUpdated, FunctionDetails{Name: wire.Name, ARN: wire.ARN}, nil, sdkgo.Receipt{})
}

// Invoke deletes a function and treats an already absent function as deleted.
func (operation DeleteFunctionOperation) Invoke(call sdkgo.Call, input DeleteFunctionInput) sdkgo.MutationAttempt[FunctionWriteResult] {
	if !functionNamePattern.MatchString(input.FunctionName) {
		return functionDeleteBranch(DeleteFunctionBranchDefect, sdkgo.FailureValidation, "Lambda function name is invalid")
	}
	response := operation.client.send(call, http.MethodDelete, "/2015-03-31/functions/"+input.FunctionName, nil)
	if errors.Is(response.err, errCredential) {
		return functionDeleteBranch(DeleteFunctionBranchDefect, sdkgo.FailureAuthentication, "Lambda credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[FunctionWriteResult](functionDeleteFailure(sdkgo.FailureTransport, "Lambda request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[FunctionWriteResult](functionDeleteFailure(sdkgo.FailureRateLimit, "Lambda rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[FunctionWriteResult](functionDeleteFailure(sdkgo.FailureAvailability, "Lambda is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusNoContent && response.status != http.StatusNotFound {
		return functionDeleteBranch(DeleteFunctionBranchProviderRejected, sdkgo.FailureProviderRejection, "Lambda rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(DeleteFunctionBranchDeleted, FunctionWriteResult{Applied: true}, nil, sdkgo.Receipt{})
}

func functionPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-lambda", Operation: "listFunctions", Message: message}
}
func functionPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[FunctionPage] {
	failure := functionPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, FunctionPage{}, &failure, sdkgo.Receipt{})
}
func functionUpdateFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-lambda", Operation: "updateFunctionDescription", Message: message}
}
func functionUpdateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[FunctionDetails] {
	failure := functionUpdateFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, FunctionDetails{}, &failure, sdkgo.Receipt{})
}
func functionDeleteFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-lambda", Operation: "deleteFunction", Message: message}
}
func functionDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[FunctionWriteResult] {
	failure := functionDeleteFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, FunctionWriteResult{}, &failure, sdkgo.Receipt{})
}
