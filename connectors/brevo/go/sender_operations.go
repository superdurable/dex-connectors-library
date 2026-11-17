// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateSenderInput supplies a sender address and display name.
type CreateSenderInput struct {
	// Email is an address the owner can verify through Brevo.
	Email string `json:"email"`
	// Name appears as the sender display name.
	Name string `json:"name"`
}

// UpdateSenderInput sets a sender's absolute display name.
type UpdateSenderInput struct {
	// ID is Brevo's numeric sender ID.
	ID int64 `json:"id"`
	// Name replaces the current sender display name.
	Name string `json:"name"`
}

// DeleteSenderInput identifies the sender to delete.
type DeleteSenderInput struct {
	// ID is Brevo's numeric sender ID.
	ID int64 `json:"id"`
}

// SenderChangeResult identifies the created, updated, or deleted sender.
type SenderChangeResult struct {
	// ID is Brevo's numeric sender ID.
	ID int64 `json:"id"`
}

// CreateSenderOperation creates a sender with bounded repeats.
type CreateSenderOperation struct{ client *Client }

// UpdateSenderOperation replaces a sender's display name.
type UpdateSenderOperation struct{ client *Client }

// DeleteSenderOperation deletes a sender.
type DeleteSenderOperation struct{ client *Client }

// CreateSender binds sender creation to this client.
func (client *Client) CreateSender() CreateSenderOperation { return CreateSenderOperation{client} }

// UpdateSender binds sender updates to this client.
func (client *Client) UpdateSender() UpdateSenderOperation { return UpdateSenderOperation{client} }

// DeleteSender binds sender deletion to this client.
func (client *Client) DeleteSender() DeleteSenderOperation { return DeleteSenderOperation{client} }

// Definition returns the generated sender creation contract.
func (CreateSenderOperation) Definition() sdkgo.MutationDefinition { return CreateSenderDefinition }

// Definition returns the generated sender update contract.
func (UpdateSenderOperation) Definition() sdkgo.MutationDefinition { return UpdateSenderDefinition }

// Definition returns the generated sender deletion contract.
func (DeleteSenderOperation) Definition() sdkgo.MutationDefinition { return DeleteSenderDefinition }

// IdempotencyKey preserves the call identity across bounded create sends.
func (CreateSenderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSenderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across absolute sender updates.
func (UpdateSenderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSenderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across sender deletions.
func (DeleteSenderOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSenderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a sender, bounding possible duplicate sends after unknown outcomes.
func (operation CreateSenderOperation) Invoke(call sdkgo.Call, input CreateSenderInput) sdkgo.MutationAttempt[SenderChangeResult] {
	const name = "createSender"
	if !validEmail(input.Email) || strings.TrimSpace(input.Name) == "" {
		return brevoMutationBranch(CreateSenderBranchDefect, SenderChangeResult{}, name, sdkgo.FailureValidation, "sender email and name are required")
	}
	payload, err := json.Marshal(input)
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(CreateSenderBranchDefect, SenderChangeResult{}, name, sdkgo.FailureValidation, "sender input is invalid or too large")
	}
	fence, err := singledispatch.NewFence(call, CreateSenderDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return brevoMutationBranch(CreateSenderBranchDefect, SenderChangeResult{}, name, sdkgo.FailureLocalDefect, "sender dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SenderChangeResult{}, *brevoFailure(name, sdkgo.FailureTransport, "an earlier sender may have been created"), sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodPost, "/senders", payload, name)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateSenderBranchDefect, SenderChangeResult{}, failure, sdkgo.Receipt{})
	}
	var response *http.Response
	err = fence.Dispatch(func(ctx context.Context) error {
		response, err = operation.client.httpClient.Do(request.WithContext(ctx))
		return err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SenderChangeResult](*brevoFailure(name, sdkgo.FailureAvailability, "sender dispatch was not recorded or sent"), 0)
	}
	if response != nil {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			if checkpointError := fence.RecordCanResend(); checkpointError != nil {
				return sdkgo.NewMutationRetry[SenderChangeResult](*brevoFailure(name, sdkgo.FailureAvailability, "sender retry checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[SenderChangeResult](*brevoFailure(name, sdkgo.FailureRateLimit, "Brevo asked to retry sender creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		}
	}
	if err != nil || response == nil || isRetryable(response.StatusCode) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(SenderChangeResult{}, *brevoFailure(name, sdkgo.FailureTransport, "sender outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[SenderChangeResult](*brevoFailure(name, sdkgo.FailureTransport, "sender outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		return brevoMutationBranch(CreateSenderBranchProviderRejected, SenderChangeResult{}, name, failureKind(response.StatusCode), "Brevo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, readError := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	var result SenderChangeResult
	if readError != nil || json.Unmarshal(body, &result) != nil || result.ID <= 0 {
		return sdkgo.NewMutationUncertain(SenderChangeResult{}, *brevoFailure(name, sdkgo.FailureProtocol, "Brevo created a sender but its ID could not be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateSenderBranchCreated, result, nil, sdkgo.Receipt{})
}

// Invoke sets the sender's display name to an absolute value.
func (operation UpdateSenderOperation) Invoke(call sdkgo.Call, input UpdateSenderInput) sdkgo.MutationAttempt[SenderChangeResult] {
	const name = "updateSender"
	if input.ID <= 0 || strings.TrimSpace(input.Name) == "" {
		return brevoMutationBranch(UpdateSenderBranchDefect, SenderChangeResult{}, name, sdkgo.FailureValidation, "sender ID and name are required")
	}
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
	}{input.Name})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(UpdateSenderBranchDefect, SenderChangeResult{}, name, sdkgo.FailureValidation, "sender input is invalid or too large")
	}
	return invokeBrevoNoContentMutation(call, operation.client, name, http.MethodPut, "/senders/"+strconv.FormatInt(input.ID, 10), payload, SenderChangeResult{ID: input.ID}, UpdateSenderBranchUpdated, UpdateSenderBranchNotFound, UpdateSenderBranchProviderRejected, UpdateSenderBranchDefect)
}

// Invoke removes the sender by its numeric ID.
func (operation DeleteSenderOperation) Invoke(call sdkgo.Call, input DeleteSenderInput) sdkgo.MutationAttempt[SenderChangeResult] {
	const name = "deleteSender"
	if input.ID <= 0 {
		return brevoMutationBranch(DeleteSenderBranchDefect, SenderChangeResult{}, name, sdkgo.FailureValidation, "positive sender ID is required")
	}
	return invokeBrevoNoContentMutation(call, operation.client, name, http.MethodDelete, "/senders/"+strconv.FormatInt(input.ID, 10), nil, SenderChangeResult{ID: input.ID}, DeleteSenderBranchDeleted, DeleteSenderBranchNotFound, DeleteSenderBranchProviderRejected, DeleteSenderBranchDefect)
}
