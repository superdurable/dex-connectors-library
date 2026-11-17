// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateContactInput records email consent and who supplied a new contact.
type CreateContactInput struct {
	// EmailAddress is the unique email address, without a display name.
	EmailAddress string `json:"emailAddress"`
	// FirstName is the optional given name, up to 50 characters.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the optional family name, up to 50 characters.
	LastName string `json:"lastName,omitempty"`
	// EmailPermission is explicit, implicit, or not_set; use only the permission actually obtained.
	EmailPermission string `json:"emailPermission"`
	// CreateSource is Account or Contact, according to who added the contact.
	CreateSource string `json:"createSource"`
}

// CreateContactOperation creates one email contact with a bounded send count.
type CreateContactOperation struct{ client *Client }

// CreateContact binds the Mutation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey correlates attempts; Constant Contact does not accept this key on creation.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a contact and checks the unique email before any possible resend.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	address, addressError := mail.ParseAddress(input.EmailAddress)
	if addressError != nil || address.Address != input.EmailAddress || len(input.EmailAddress) > 80 ||
		len(input.FirstName) > 50 || len(input.LastName) > 50 || strings.ContainsAny(input.FirstName+input.LastName, "\r\n") ||
		(input.EmailPermission != "explicit" && input.EmailPermission != "implicit" && input.EmailPermission != "not_set") ||
		(input.CreateSource != "Account" && input.CreateSource != "Contact") {
		return listMutationBranch[ContactMetadata]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "invalid contact email, name, permission, or source")
	}
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return listMutationBranch[ContactMetadata]("createContact", CreateContactBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createContactUncertain("previous contact creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		contact, isKnown, shouldRetry := operation.client.findContactByEmail(call, input)
		if shouldRetry {
			return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", sdkgo.FailureAvailability, "contact read-back unavailable"), 0)
		}
		if contact != nil {
			return sdkgo.NewMutationBranch(CreateContactBranchCreated, *contact, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return createContactUncertain("previous contact creation cannot be confirmed")
		}
	}
	payload, err := json.Marshal(struct {
		EmailAddress struct {
			Address          string `json:"address"`
			PermissionToSend string `json:"permission_to_send"`
		} `json:"email_address"`
		FirstName    string `json:"first_name,omitempty"`
		LastName     string `json:"last_name,omitempty"`
		CreateSource string `json:"create_source"`
	}{
		EmailAddress: struct {
			Address          string `json:"address"`
			PermissionToSend string `json:"permission_to_send"`
		}{input.EmailAddress, input.EmailPermission},
		FirstName: input.FirstName, LastName: input.LastName, CreateSource: input.CreateSource,
	})
	if err != nil {
		return listMutationBranch[ContactMetadata]("createContact", CreateContactBranchDefect, sdkgo.FailureLocalDefect, "contact request could not be encoded")
	}
	credentials, credentialResponse := operation.client.resolveCredentials(call)
	if credentialResponse.err != nil {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		if errors.Is(credentialResponse.err, errCredential) {
			return listMutationBranch[ContactMetadata]("createContact", CreateContactBranchDefect, sdkgo.FailureAuthentication, "Constant Contact credentials are unavailable or invalid")
		}
		return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", sdkgo.FailureAvailability, "Constant Contact credentials could not be refreshed"), 0)
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.sendAuthorized(requestContext, credentials, http.MethodPost, "/contacts", payload)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", kind, "Constant Contact did not create the contact"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", sdkgo.FailureTransport, "contact creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return createContactUncertain("an earlier contact creation may have succeeded")
		}
		branch := CreateContactBranchProviderRejected
		if response.status == http.StatusConflict {
			branch = CreateContactBranchAlreadyExists
		}
		return listMutationBranch[ContactMetadata]("createContact", branch, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	contact, decodeError := decodeContact(response.body)
	if response.status != http.StatusCreated && response.status != http.StatusOK || decodeError != nil {
		return sdkgo.NewMutationRetry[ContactMetadata](listFailure("createContact", sdkgo.FailureProtocol, "Constant Contact returned an invalid contact"), 0)
	}
	_ = fence.RecordProviderID(contact.ContactID)
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
}

func (client *Client) findContactByEmail(call sdkgo.Call, input CreateContactInput) (*ContactMetadata, bool, bool) {
	response := client.get(call, "/contacts?email="+url.QueryEscape(input.EmailAddress))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var envelope struct {
		Contacts []json.RawMessage `json:"contacts"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Contacts == nil {
		return nil, false, true
	}
	for _, item := range envelope.Contacts {
		contact, err := decodeContact(item)
		if err != nil {
			return nil, false, true
		}
		if strings.EqualFold(contact.EmailAddress, input.EmailAddress) && contact.FirstName == input.FirstName && contact.LastName == input.LastName && contact.EmailPermission == input.EmailPermission {
			return &contact, true, false
		}
	}
	return nil, true, false
}

func createContactUncertain(message string) sdkgo.MutationAttempt[ContactMetadata] {
	return sdkgo.NewMutationUncertain(ContactMetadata{}, listFailure("createContact", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
