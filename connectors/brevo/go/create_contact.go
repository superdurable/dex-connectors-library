// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateContactInput supplies a new email-identified contact.
type CreateContactInput struct {
	// Email is the new contact's plain email address.
	Email string `json:"email"`
	// Attributes sets existing uppercase Brevo contact attributes.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// CreateContactOperation creates a contact without updating an existing one.
type CreateContactOperation struct{ client *Client }

// CreateContact binds strict contact creation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated strict contact creation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey preserves the call identity across bounded sends.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a new contact, first checking by email after unknown outcomes.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactWriteResult] {
	const name = "createContact"
	if !validEmail(input.Email) {
		return brevoMutationBranch(CreateContactBranchDefect, ContactWriteResult{}, name, sdkgo.FailureValidation, "plain contact email is required")
	}
	for key := range input.Attributes {
		if key == "" || strings.ToUpper(key) != key {
			return brevoMutationBranch(CreateContactBranchDefect, ContactWriteResult{}, name, sdkgo.FailureValidation, "contact attribute names must be uppercase")
		}
	}
	payload, err := json.Marshal(struct {
		Email         string            `json:"email"`
		Attributes    map[string]string `json:"attributes,omitempty"`
		UpdateEnabled bool              `json:"updateEnabled"`
	}{input.Email, input.Attributes, false})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(CreateContactBranchDefect, ContactWriteResult{}, name, sdkgo.FailureValidation, "contact input is invalid or too large")
	}
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return brevoMutationBranch(CreateContactBranchDefect, ContactWriteResult{}, name, sdkgo.FailureLocalDefect, "contact dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if contactID := operation.findCreatedContact(call, input.Email); contactID > 0 {
			return sdkgo.NewMutationBranch(CreateContactBranchCreated, ContactWriteResult{ID: contactID, Email: input.Email, Created: true}, nil, sdkgo.Receipt{})
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ContactWriteResult{}, *brevoFailure(name, sdkgo.FailureTransport, "an earlier contact may have been created"), sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodPost, "/contacts", payload, name)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateContactBranchDefect, ContactWriteResult{}, failure, sdkgo.Receipt{})
	}
	var response *http.Response
	err = fence.Dispatch(func(ctx context.Context) error {
		response, err = operation.client.httpClient.Do(request.WithContext(ctx))
		return err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureAvailability, "contact dispatch was not recorded or sent"), 0)
	}
	if response != nil {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			if checkpointError := fence.RecordCanResend(); checkpointError != nil {
				return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureAvailability, "contact retry checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureRateLimit, "Brevo asked to retry contact creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		}
	}
	if err != nil || response == nil || isRetryable(response.StatusCode) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(ContactWriteResult{}, *brevoFailure(name, sdkgo.FailureTransport, "contact outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureTransport, "contact outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		return brevoMutationBranch(CreateContactBranchProviderRejected, ContactWriteResult{}, name, failureKind(response.StatusCode), "Brevo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, readError := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	var created struct {
		ID int64 `json:"id"`
	}
	if readError != nil || json.Unmarshal(body, &created) != nil || created.ID <= 0 {
		return sdkgo.NewMutationUncertain(ContactWriteResult{}, *brevoFailure(name, sdkgo.FailureProtocol, "Brevo created a contact but its ID could not be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, ContactWriteResult{ID: created.ID, Email: input.Email, Created: true}, nil, sdkgo.Receipt{})
}

func (operation CreateContactOperation) findCreatedContact(call sdkgo.Call, email string) int64 {
	request, failure := operation.client.newRequest(call, http.MethodGet, "/contacts/"+url.PathEscape(email), nil, "createContact")
	if failure != nil {
		return 0
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return 0
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return 0
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return 0
	}
	var found Contact
	if json.Unmarshal(body, &found) != nil || found.Email != email || found.ID <= 0 {
		return 0
	}
	return found.ID
}
