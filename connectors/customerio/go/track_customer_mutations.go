// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpsertCustomerInput identifies a customer and replaces supplied attributes.
type UpsertCustomerInput struct {
	// Identifier is a workspace ID, email, or prefixed cio_id.
	Identifier string `json:"identifier"`
	// Attributes are non-secret profile values to add or replace.
	Attributes map[string]any `json:"attributes"`
}

// DeleteCustomerInput identifies a customer to remove permanently.
type DeleteCustomerInput struct {
	// Identifier is a workspace ID, email, or prefixed cio_id.
	Identifier string `json:"identifier"`
}

// CustomerWriteOutput confirms the identifier submitted to Track API.
type CustomerWriteOutput struct {
	// Identifier is the submitted customer identifier.
	Identifier string `json:"identifier"`
}

// UpsertCustomerOperation applies profile attributes through Track API.
type UpsertCustomerOperation struct{ client *Client }

// DeleteCustomerOperation removes a customer through Track API.
type DeleteCustomerOperation struct{ client *Client }

// UpsertCustomer binds the mutation to this client.
func (client *Client) UpsertCustomer() UpsertCustomerOperation {
	return UpsertCustomerOperation{client}
}

// DeleteCustomer binds the mutation to this client.
func (client *Client) DeleteCustomer() DeleteCustomerOperation {
	return DeleteCustomerOperation{client}
}

// Definition returns the generated upsert contract.
func (UpsertCustomerOperation) Definition() sdkgo.MutationDefinition { return UpsertCustomerDefinition }

// Definition returns the generated delete contract.
func (DeleteCustomerOperation) Definition() sdkgo.MutationDefinition { return DeleteCustomerDefinition }

// IdempotencyKey identifies one absolute profile update; Track API does not consume it.
func (UpsertCustomerOperation) IdempotencyKey(id sdkgo.CallID, _ UpsertCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable deletion.
func (DeleteCustomerOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke adds a profile or changes only the supplied attributes.
func (operation UpsertCustomerOperation) Invoke(call sdkgo.Call, input UpsertCustomerInput) sdkgo.MutationAttempt[CustomerWriteOutput] {
	if !validCustomerIdentifier(input.Identifier) || !validCustomerAttributes(input.Attributes) {
		return trackWriteBranch("upsertCustomer", UpsertCustomerBranchDefect, sdkgo.FailureValidation, "invalid customer identifier or attributes")
	}
	body, err := json.Marshal(input.Attributes)
	if err != nil {
		return trackWriteBranch("upsertCustomer", UpsertCustomerBranchDefect, sdkgo.FailureValidation, "customer attributes cannot be encoded")
	}
	response := operation.client.doTrackRequest(call, http.MethodPut, "/api/v1/customers/"+url.PathEscape(input.Identifier), body)
	return classifyTrackWrite("upsertCustomer", input.Identifier, response, UpsertCustomerBranchUpserted, "", UpsertCustomerBranchProviderRejected, UpsertCustomerBranchDefect)
}

// Invoke permanently removes one profile and its Customer.io information.
func (operation DeleteCustomerOperation) Invoke(call sdkgo.Call, input DeleteCustomerInput) sdkgo.MutationAttempt[CustomerWriteOutput] {
	if !validCustomerIdentifier(input.Identifier) {
		return trackWriteBranch("deleteCustomer", DeleteCustomerBranchDefect, sdkgo.FailureValidation, "invalid customer identifier")
	}
	response := operation.client.doTrackRequest(call, http.MethodDelete, "/api/v1/customers/"+url.PathEscape(input.Identifier), nil)
	return classifyTrackWrite("deleteCustomer", input.Identifier, response, DeleteCustomerBranchDeleted, DeleteCustomerBranchNotFound, DeleteCustomerBranchProviderRejected, DeleteCustomerBranchDefect)
}

func (client *Client) doTrackRequest(call sdkgo.Call, method string, path string, body []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	siteID, apiKey := credentials.TrackingSiteID, credentials.TrackingAPIKey.Reveal()
	if siteID == "" || apiKey == "" || !providerhttp.IsHeaderSafeCredential(siteID) || !providerhttp.IsHeaderSafeCredential(apiKey) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.trackingBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.SetBasicAuth(siteID, apiKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func validCustomerIdentifier(identifier string) bool {
	return identifier != "" && len(identifier) <= 256 && utf8.ValidString(identifier) && strings.TrimSpace(identifier) == identifier && !strings.ContainsAny(identifier, "\r\n\t")
}

func validCustomerAttributes(attributes map[string]any) bool {
	if len(attributes) == 0 || len(attributes) > 100 {
		return false
	}
	for name := range attributes {
		if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.ContainsAny(name, "\r\n\t") {
			return false
		}
	}
	return true
}

func classifyTrackWrite(operation string, identifier string, response providerResponse, successBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[CustomerWriteOutput] {
	if errors.Is(response.err, errCredential) {
		return trackWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Customer.io Track credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[CustomerWriteOutput](trackWriteFailure(operation, sdkgo.FailureTransport, "Customer.io Track request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CustomerWriteOutput](trackWriteFailure(operation, sdkgo.FailureRateLimit, "Customer.io rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CustomerWriteOutput](trackWriteFailure(operation, sdkgo.FailureAvailability, "Customer.io Track write is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusUnauthorized {
		return trackWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Customer.io rejected Track credentials")
	}
	if response.status == http.StatusNotFound && notFoundBranch != "" {
		return trackWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Customer.io customer was not found")
	}
	if response.status != http.StatusOK {
		return trackWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Customer.io rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CustomerWriteOutput](trackWriteFailure(operation, sdkgo.FailureTransport, "Customer.io Track response failed"), 0)
	}
	return sdkgo.NewMutationBranch(successBranch, CustomerWriteOutput{Identifier: identifier}, nil, sdkgo.Receipt{})
}

func trackWriteFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "customerio", Operation: operation, Message: message}
}

func trackWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CustomerWriteOutput] {
	failure := trackWriteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, CustomerWriteOutput{}, &failure, sdkgo.Receipt{})
}
