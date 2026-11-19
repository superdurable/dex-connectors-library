// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteResponseRequest identifies a stored response to remove.
type DeleteResponseRequest struct {
	// ResponseID is the ID shown on the response in the OpenAI Platform or returned by createResponse.
	ResponseID string `json:"responseId"`
}

// DeletedResponse confirms removal of a stored response.
type DeletedResponse struct {
	// ID is the deleted response ID.
	ID string `json:"id"`
	// Deleted is true when the provider confirms removal.
	Deleted bool `json:"deleted"`
}

// DeleteResponseOperation removes a stored response.
type DeleteResponseOperation struct{ client *Client }

// DeleteResponse returns the typed removal Mutation bound to this client.
func (client *Client) DeleteResponse() DeleteResponseOperation {
	return DeleteResponseOperation{client: client}
}

// Definition returns the manifest-generated delete contract.
func (DeleteResponseOperation) Definition() sdkgo.MutationDefinition { return DeleteResponseDefinition }

// IdempotencyKey returns the stable connector call ID.
func (DeleteResponseOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteResponseRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the stored response and retries transient or unknown outcomes safely.
func (operation DeleteResponseOperation) Invoke(call sdkgo.Call, input DeleteResponseRequest) sdkgo.MutationAttempt[DeletedResponse] {
	if !isSafeResponseID(input.ResponseID) {
		failure := openAIFailure(sdkgo.FailureValidation, "deleteResponse", "response ID is invalid")
		return sdkgo.NewMutationBranch(DeleteResponseBranchDefect, DeletedResponse{}, &failure, sdkgo.Receipt{})
	}
	request, requestFailure := operation.client.newRequest(call, http.MethodDelete, "/responses/"+url.PathEscape(input.ResponseID), "", nil)
	if requestFailure != nil {
		return sdkgo.NewMutationBranch(DeleteResponseBranchDefect, DeletedResponse{}, &requestFailure.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedResponse](openAIFailure(sdkgo.FailureTransport, "deleteResponse", "provider outcome could not be confirmed"), 0)
	}
	defer response.Body.Close()
	receipt := responseReceipt(call, response.Header.Get("X-Request-Id"), response.Header, input.ResponseID)
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode >= 500 {
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[DeletedResponse](openAIFailure(kind, "deleteResponse", "provider returned HTTP "+strconv.Itoa(response.StatusCode)), retryAfterHeader(response.Header))
	}
	if response.StatusCode == http.StatusNotFound {
		failure := openAIFailure(sdkgo.FailureNotFound, "deleteResponse", "stored response was not found")
		return sdkgo.NewMutationBranch(DeleteResponseBranchNotFound, DeletedResponse{}, &failure, receipt)
	}
	if response.StatusCode != http.StatusOK {
		failure := openAIFailure(sdkgo.FailureProviderRejection, "deleteResponse", "provider returned HTTP "+strconv.Itoa(response.StatusCode))
		return sdkgo.NewMutationBranch(DeleteResponseBranchProviderRejected, DeletedResponse{}, &failure, receipt)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, operation.client.maxResponseBytes+1))
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedResponse](openAIFailure(sdkgo.FailureTransport, "deleteResponse", "deletion receipt could not be read"), 0)
	}
	if int64(len(body)) > operation.client.maxResponseBytes {
		failure := openAIFailure(sdkgo.FailureResponseTooLarge, "deleteResponse", "deletion receipt exceeds the configured limit")
		return sdkgo.NewMutationBranch(DeleteResponseBranchInvalidResponse, DeletedResponse{}, &failure, receipt)
	}
	var result DeletedResponse
	if json.Unmarshal(body, &result) != nil || result.ID != input.ResponseID || !result.Deleted {
		failure := openAIFailure(sdkgo.FailureProtocol, "deleteResponse", "provider returned an invalid deletion receipt")
		return sdkgo.NewMutationBranch(DeleteResponseBranchInvalidResponse, DeletedResponse{}, &failure, receipt)
	}
	receipt.ObservedAt = time.Now().UTC()
	return sdkgo.NewMutationBranch(DeleteResponseBranchDeleted, result, nil, receipt)
}
