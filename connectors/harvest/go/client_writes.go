// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateClientInput contains the required name and optional client details.
type CreateClientInput struct {
	// Name is the new client's nonempty display name.
	Name string `json:"name"`
	// IsActive sets the active state; nil lets Harvest use its default.
	IsActive *bool `json:"isActive,omitempty"`
	// Address is the optional physical or mailing address.
	Address string `json:"address,omitempty"`
	// Currency is an optional ISO 4217 currency code.
	Currency string `json:"currency,omitempty"`
}

// UpdateClientInput identifies a client and the absolute values to set.
type UpdateClientInput struct {
	// ID is the positive Harvest client ID.
	ID int64 `json:"id"`
	// Name replaces the display name when supplied.
	Name *string `json:"name,omitempty"`
	// IsActive sets the active or archived state when supplied.
	IsActive *bool `json:"isActive,omitempty"`
	// Address replaces the address when supplied; empty clears it.
	Address *string `json:"address,omitempty"`
	// Currency replaces the ISO 4217 currency when supplied.
	Currency *string `json:"currency,omitempty"`
}

// DeleteClientInput identifies the client to delete.
type DeleteClientInput struct {
	// ID is the positive Harvest client ID.
	ID int64 `json:"id"`
}

// DeletedClient identifies a client whose deletion Harvest confirmed.
type DeletedClient struct {
	// ID is the deleted client's Harvest ID.
	ID int64 `json:"id"`
}

// CreateClientOperation creates one client behind a bounded dispatch fence.
type CreateClientOperation struct{ client *Client }

// CreateClient binds the client creation Mutation to this client.
func (client *Client) CreateClient() CreateClientOperation { return CreateClientOperation{client} }

// Definition returns the generated client creation contract.
func (CreateClientOperation) Definition() sdkgo.MutationDefinition { return CreateClientDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate it.
func (CreateClientOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateClientInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a client, retrying unknown outcomes only within the send cap.
func (operation CreateClientOperation) Invoke(call sdkgo.Call, input CreateClientInput) sdkgo.MutationAttempt[ClientRecord] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || len(input.Address) > 10000 || !validHarvestCurrency(input.Currency) {
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchDefect, sdkgo.FailureValidation, "client name, address, or currency is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateClientDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return uncertainClientMutation("createClient", sdkgo.FailureTransport, "the prior client creation could not be confirmed")
	}
	// Client names are not unique in Harvest, so a name-only read cannot identify this creation.
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return uncertainClientMutation("createClient", sdkgo.FailureAuthentication, "credentials are unavailable after an unconfirmed creation")
		}
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchDefect, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	payload, err := json.Marshal(struct {
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active,omitempty"`
		Address  string `json:"address,omitempty"`
		Currency string `json:"currency,omitempty"`
	}{Name: input.Name, IsActive: input.IsActive, Address: input.Address, Currency: input.Currency})
	if err != nil {
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchDefect, sdkgo.FailureValidation, "client input cannot be encoded")
	}
	request, err := operation.client.newHarvestRequest(call.Context, credentials, http.MethodPost, "/clients", payload)
	if err != nil {
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchDefect, sdkgo.FailureLocalDefect, "client request cannot be built")
	}
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchDefect, sdkgo.FailureLocalDefect, "client request is invalid")
	}
	var response *http.Response
	var didGetConnection atomic.Bool
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		response, err = operation.client.httpClient.Do(request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
		return err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return retryClientMutation[ClientRecord]("createClient", sdkgo.FailureAvailability, "client dispatch did not start", 0)
	}
	if err != nil {
		if !didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return retryClientMutation[ClientRecord]("createClient", sdkgo.FailureTransport, "client request was not sent", 0)
			}
		}
		return operation.retryOrUncertainCreate(call, decision, sdkgo.FailureTransport, "client creation outcome is unknown", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return retryClientMutation[ClientRecord]("createClient", sdkgo.FailureAvailability, "client retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return retryClientMutation[ClientRecord]("createClient", kind, "Harvest deferred client creation", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return operation.retryOrUncertainCreate(call, decision, sdkgo.FailureAvailability, "client creation outcome is unknown", 0)
	}
	if response.StatusCode >= 400 {
		if decision.MaximumPriorSends > 0 {
			return uncertainClientMutation("createClient", sdkgo.FailureProviderRejection, "a prior client creation remains unconfirmed")
		}
		return failedClientMutation[ClientRecord]("createClient", CreateClientBranchProviderRejected, sdkgo.FailureProviderRejection, "Harvest rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusCreated {
		return operation.retryOrUncertainCreate(call, decision, sdkgo.FailureProtocol, "Harvest returned an unexpected create status", 0)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return operation.retryOrUncertainCreate(call, decision, sdkgo.FailureTransport, "client creation response was interrupted", 0)
	}
	created, err := decodeClientRecord(body)
	if err != nil {
		return operation.retryOrUncertainCreate(call, decision, sdkgo.FailureProtocol, "client creation response was invalid", 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(created.ID, 10))
	return sdkgo.NewMutationBranch(CreateClientBranchCreated, created, nil, sdkgo.Receipt{})
}

func (operation CreateClientOperation) retryOrUncertainCreate(call sdkgo.Call, decision singledispatch.Decision, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[ClientRecord] {
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return retryClientMutation[ClientRecord]("createClient", kind, message, after)
	}
	return uncertainClientMutation("createClient", kind, message)
}

// UpdateClientOperation sets absolute values on one existing client.
type UpdateClientOperation struct{ client *Client }

// UpdateClient binds the client update Mutation to this client.
func (client *Client) UpdateClient() UpdateClientOperation { return UpdateClientOperation{client} }

// Definition returns the generated client update contract.
func (UpdateClientOperation) Definition() sdkgo.MutationDefinition { return UpdateClientDefinition }

// IdempotencyKey records the stable Dex call identity for this update.
func (UpdateClientOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateClientInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an absolute PATCH; unknown outcomes can safely retry.
func (operation UpdateClientOperation) Invoke(call sdkgo.Call, input UpdateClientInput) sdkgo.MutationAttempt[ClientRecord] {
	if input.ID <= 0 || (input.Name == nil && input.IsActive == nil && input.Address == nil && input.Currency == nil) ||
		(input.Name != nil && (strings.TrimSpace(*input.Name) == "" || len(*input.Name) > 255)) ||
		(input.Address != nil && len(*input.Address) > 10000) ||
		(input.Currency != nil && !validHarvestCurrency(*input.Currency)) {
		return failedClientMutation[ClientRecord]("updateClient", UpdateClientBranchDefect, sdkgo.FailureValidation, "client ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name     *string `json:"name,omitempty"`
		IsActive *bool   `json:"is_active,omitempty"`
		Address  *string `json:"address,omitempty"`
		Currency *string `json:"currency,omitempty"`
	}{Name: input.Name, IsActive: input.IsActive, Address: input.Address, Currency: input.Currency})
	if err != nil {
		return failedClientMutation[ClientRecord]("updateClient", UpdateClientBranchDefect, sdkgo.FailureValidation, "client update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateClient", http.MethodPatch, "/clients/"+strconv.FormatInt(input.ID, 10), payload, UpdateClientBranchUpdated, UpdateClientBranchNotFound, UpdateClientBranchProviderRejected, decodeClientRecord)
}

// DeleteClientOperation deletes a client with no related records.
type DeleteClientOperation struct{ client *Client }

// DeleteClient binds the client deletion Mutation to this client.
func (client *Client) DeleteClient() DeleteClientOperation { return DeleteClientOperation{client} }

// Definition returns the generated client deletion contract.
func (DeleteClientOperation) Definition() sdkgo.MutationDefinition { return DeleteClientDefinition }

// IdempotencyKey records the stable Dex call identity for this deletion.
func (DeleteClientOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteClientInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the client; a missing client is a definite outcome.
func (operation DeleteClientOperation) Invoke(call sdkgo.Call, input DeleteClientInput) sdkgo.MutationAttempt[DeletedClient] {
	if input.ID <= 0 {
		return failedClientMutation[DeletedClient]("deleteClient", DeleteClientBranchDefect, sdkgo.FailureValidation, "client ID must be positive")
	}
	decode := func(_ []byte) (DeletedClient, error) { return DeletedClient{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteClient", http.MethodDelete, "/clients/"+strconv.FormatInt(input.ID, 10), nil, DeleteClientBranchDeleted, DeleteClientBranchNotFound, DeleteClientBranchProviderRejected, decode)
}

func (client *Client) newHarvestRequest(ctx context.Context, credentials Credentials, method, path string, body []byte) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Harvest-Account-Id", client.accountID)
	request.Header.Set("User-Agent", "superdurable-harvest-connector")
	return request, nil
}

func invokeSafeHarvestWrite[T any](client *Client, call sdkgo.Call, operationID, method, path string, payload []byte, successBranch, notFoundBranch, rejectedBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedClientMutation[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := client.newHarvestRequest(call.Context, credentials, method, path, payload)
	if err != nil {
		return failedClientMutation[T](operationID, "defect", sdkgo.FailureLocalDefect, "client request cannot be built")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryClientMutation[T](operationID, sdkgo.FailureTransport, "provider request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return retryClientMutation[T](operationID, kind, "provider deferred client write", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict {
		return retryClientMutation[T](operationID, sdkgo.FailureAvailability, "client write outcome is unknown", 0)
	}
	if response.StatusCode == http.StatusNotFound {
		return failedClientMutation[T](operationID, notFoundBranch, sdkgo.FailureNotFound, "client was not found")
	}
	if response.StatusCode >= 400 {
		return failedClientMutation[T](operationID, rejectedBranch, sdkgo.FailureProviderRejection, "Harvest rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return retryClientMutation[T](operationID, sdkgo.FailureProtocol, "unexpected client write response", 0)
	}
	if method == http.MethodDelete {
		value, _ := decode(nil)
		return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return retryClientMutation[T](operationID, sdkgo.FailureTransport, "client response was interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return retryClientMutation[T](operationID, sdkgo.FailureProtocol, "client response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func validHarvestCurrency(currency string) bool {
	if currency == "" {
		return true
	}
	if len(currency) != 3 {
		return false
	}
	for _, letter := range currency {
		if letter < 'A' || letter > 'Z' {
			return false
		}
	}
	return true
}

func failedClientMutation[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func retryClientMutation[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewMutationRetry[T](failure, after)
}

func uncertainClientMutation(operationID string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ClientRecord] {
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewMutationUncertain(ClientRecord{}, failure, sdkgo.Receipt{})
}
