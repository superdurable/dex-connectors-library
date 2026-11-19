// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateLeadInput replaces selected fields on one lead; omitted pointers preserve existing values.
type UpdateLeadInput struct {
	// LeadID identifies the Close lead to change.
	LeadID string `json:"leadId"`
	// Name replaces the lead's name when present.
	Name *string `json:"name,omitempty"`
	// Description replaces the lead's description when present; empty clears it.
	Description *string `json:"description,omitempty"`
}

// DeleteLeadInput identifies one lead to remove.
type DeleteLeadInput struct {
	// LeadID identifies the Close lead and its child records.
	LeadID string `json:"leadId"`
}

// DeletedLead confirms that a lead is absent after deletion.
type DeletedLead struct {
	// LeadID identifies the absent lead.
	LeadID string `json:"leadId"`
}

// UpdateLeadOperation replaces selected lead fields through Close.
type UpdateLeadOperation struct{ client *Client }

// UpdateLead binds the mutation to this client.
func (client *Client) UpdateLead() UpdateLeadOperation { return UpdateLeadOperation{client} }

// Definition returns the generated updateLead contract.
func (UpdateLeadOperation) Definition() sdkgo.MutationDefinition { return UpdateLeadDefinition }

// IdempotencyKey correlates attempts; the absolute field replacement is repeat safe.
func (UpdateLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates the replacement and classifies the Close response.
func (operation UpdateLeadOperation) Invoke(call sdkgo.Call, input UpdateLeadInput) sdkgo.MutationAttempt[Lead] {
	if !validLeadID(input.LeadID) || (input.Name == nil && input.Description == nil) ||
		(input.Name != nil && (strings.TrimSpace(*input.Name) == "" || len(*input.Name) > 255)) ||
		(input.Description != nil && len(*input.Description) > 10000) {
		return closeMutationBranch[Lead]("updateLead", UpdateLeadBranchDefect, sdkgo.FailureValidation, "leadId and at least one valid replacement field are required")
	}
	requestBody, err := json.Marshal(struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}{Name: input.Name, Description: input.Description})
	if err != nil {
		return closeMutationBranch[Lead]("updateLead", UpdateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateLead", http.MethodPut, "/lead/"+input.LeadID+"/", requestBody, UpdateLeadBranchUpdated, UpdateLeadBranchNotFound, decodeGetLead)
}

// DeleteLeadOperation removes a lead and its child records.
type DeleteLeadOperation struct{ client *Client }

// DeleteLead binds the mutation to this client.
func (client *Client) DeleteLead() DeleteLeadOperation { return DeleteLeadOperation{client} }

// Definition returns the generated deleteLead contract.
func (DeleteLeadOperation) Definition() sdkgo.MutationDefinition { return DeleteLeadDefinition }

// IdempotencyKey correlates attempts; removing an already absent lead is repeat safe.
func (DeleteLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates the lead ID and classifies absence as a completed deletion.
func (operation DeleteLeadOperation) Invoke(call sdkgo.Call, input DeleteLeadInput) sdkgo.MutationAttempt[DeletedLead] {
	if !validLeadID(input.LeadID) {
		return closeMutationBranch[DeletedLead]("deleteLead", DeleteLeadBranchDefect, sdkgo.FailureValidation, "leadId must be a Close lead ID")
	}
	return invokeCloseWrite(operation.client, call, "deleteLead", http.MethodDelete, "/lead/"+input.LeadID+"/", nil, DeleteLeadBranchDeleted, DeleteLeadBranchDeleted, func(body []byte) (DeletedLead, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedLead{}, errors.New("invalid deletion response")
		}
		return DeletedLead{LeadID: input.LeadID}, nil
	})
}

func validLeadID(leadID string) bool {
	return strings.HasPrefix(leadID, "lead_") && safeKey.MatchString(leadID)
}

func invokeCloseWrite[T any](client *Client, call sdkgo.Call, operation, method, path string, body []byte, successBranch, notFoundBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return closeMutationBranch[T](operation, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return closeMutationBranch[T](operation, "defect", sdkgo.FailureLocalDefect, "request cannot be built")
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.SetBasicAuth(credentials.APIKey.Reveal(), "")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return closeMutationRetry[T](operation, sdkgo.FailureTransport, "provider request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return closeMutationRetry[T](operation, sdkgo.FailureRateLimit, "provider rate limit", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= 500 {
		return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "provider temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusNotFound && notFoundBranch != "" {
		if notFoundBranch == successBranch {
			value, err := decode([]byte("{}"))
			if err != nil {
				return closeMutationBranch[T](operation, "defect", sdkgo.FailureLocalDefect, "deletion result could not be built")
			}
			return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{})
		}
		return closeMutationBranch[T](operation, notFoundBranch, sdkgo.FailureNotFound, "provider resource was not found")
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return closeMutationBranch[T](operation, "providerRejected", kind, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return closeMutationBranch[T](operation, "invalidResponse", sdkgo.FailureResponseTooLarge, "provider response exceeded configured limit")
	}
	if err != nil {
		return closeMutationRetry[T](operation, sdkgo.FailureTransport, "provider response was interrupted", 0)
	}
	value, err := decode(responseBody)
	if err != nil {
		return closeMutationBranch[T](operation, "invalidResponse", sdkgo.FailureProtocol, "provider returned unusable JSON")
	}
	return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func closeMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "close", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func closeMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "close", Operation: operation, Message: message}
	return sdkgo.NewMutationRetry[T](failure, after)
}
