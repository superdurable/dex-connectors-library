// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getresponse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateContactOperation queues a contact for one GetResponse list.
type CreateContactOperation struct{ client *Client }

// UpdateContactOperation replaces supplied contact fields.
type UpdateContactOperation struct{ client *Client }

// DeleteContactOperation removes a contact.
type DeleteContactOperation struct{ client *Client }

// CreateContact binds the create Mutation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// UpdateContact binds the update Mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// DeleteContact binds the delete Mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the manifest-generated create contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// Definition returns the manifest-generated update contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// Definition returns the manifest-generated delete contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey uses the stable Dex call ID for reconciliation.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable Dex call ID for an absolute update.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable Dex call ID for an idempotent deletion.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke queues a contact and reconciles a possibly accepted earlier attempt.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[CreateContactOutput] {
	output := CreateContactOutput{Email: input.Email, CampaignID: input.CampaignID}
	address, emailError := mail.ParseAddress(input.Email)
	if emailError != nil || address.Address != input.Email || !contactIDPattern.MatchString(input.CampaignID) || len(input.Name) > 128 {
		return createContactBranch(CreateContactBranchDefect, CreateContactOutput{}, sdkgo.FailureValidation, "email, list token, or name is invalid")
	}
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createContactBranch(CreateContactBranchDefect, CreateContactOutput{}, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createContactUncertain(output, "an earlier contact request may have been accepted")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		found, readBackError := operation.client.findContact(call, input)
		if readBackError != nil {
			return sdkgo.NewMutationRetry[CreateContactOutput](queryFailure("createContact", sdkgo.FailureAvailability, "contact reconciliation is unavailable"), 0)
		}
		if found {
			return sdkgo.NewMutationBranch(CreateContactBranchAccepted, output, nil, createContactReceipt(call))
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createContactUncertain(output, "an earlier contact request cannot be confirmed")
		}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return createContactUncertain(output, "an earlier contact request may have been accepted; credentials are unavailable")
		}
		if checkpointError := fence.RecordNotSent(); checkpointError != nil {
			return sdkgo.NewMutationRetry[CreateContactOutput](queryFailure("createContact", sdkgo.FailureAvailability, "credential checkpoint is unavailable"), 0)
		}
		return createContactBranch(CreateContactBranchDefect, CreateContactOutput{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	payload, err := json.Marshal(map[string]any{"email": input.Email, "name": input.Name, "campaign": map[string]string{"campaignId": input.CampaignID}})
	if err != nil {
		return createContactBranch(CreateContactBranchDefect, CreateContactOutput{}, sdkgo.FailureLocalDefect, "contact request could not be encoded")
	}
	dispatch := &createContactDispatch{client: operation.client, token: credentials.APIKey.Reveal(), payload: payload}
	err = fence.Dispatch(dispatch.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreateContactOutput](queryFailure("createContact", sdkgo.FailureAvailability, "contact request was not dispatched"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if checkpointError := fence.RecordCanResend(); checkpointError != nil {
			return sdkgo.NewMutationRetry[CreateContactOutput](queryFailure("createContact", sdkgo.FailureAvailability, "retry checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreateContactOutput](queryFailure("createContact", kind, "GetResponse asked to retry contact creation"), dispatch.retryAfter)
	}
	if err != nil || dispatch.status >= 500 || dispatch.status == http.StatusRequestTimeout {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[CreateContactOutput](queryFailure("createContact", sdkgo.FailureAvailability, "contact creation outcome is unknown"), dispatch.retryAfter)
		}
		return createContactUncertain(output, "contact creation outcome is unknown after the send limit")
	}
	if dispatch.status == http.StatusAccepted || dispatch.status == http.StatusCreated {
		return sdkgo.NewMutationBranch(CreateContactBranchAccepted, output, nil, createContactReceipt(call))
	}
	if decision.MaximumPriorSends > 0 {
		return createContactUncertain(output, "a previous contact request may have been accepted")
	}
	return createContactBranch(CreateContactBranchProviderRejected, CreateContactOutput{}, sdkgo.FailureProviderRejection, "GetResponse rejected HTTP "+strconv.Itoa(dispatch.status))
}

// Invoke applies an absolute contact update.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if !contactIDPattern.MatchString(input.ContactID) || (input.Email == "" && input.Name == "") || len(input.Name) > 128 {
		return contactWriteBranch(UpdateContactBranchDefect, ContactMetadata{}, "updateContact", sdkgo.FailureValidation, "contact ID and at least one valid field are required")
	}
	if input.Email != "" {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email {
			return contactWriteBranch(UpdateContactBranchDefect, ContactMetadata{}, "updateContact", sdkgo.FailureValidation, "email is invalid")
		}
	}
	payload := map[string]string{}
	if input.Email != "" {
		payload["email"] = input.Email
	}
	if input.Name != "" {
		payload["name"] = input.Name
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return contactWriteBranch(UpdateContactBranchDefect, ContactMetadata{}, "updateContact", sdkgo.FailureLocalDefect, "contact update could not be encoded")
	}
	response, status, retryAfter, err := operation.client.write(call, call.Context, http.MethodPost, "/contacts/"+input.ContactID, body)
	if errors.Is(err, errCredential) {
		return contactWriteBranch(UpdateContactBranchDefect, ContactMetadata{}, "updateContact", sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("updateContact", sdkgo.FailureTransport, "GetResponse request failed"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status == http.StatusServiceUnavailable || status >= 500 {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("updateContact", sdkgo.FailureAvailability, "GetResponse is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return contactWriteBranch(UpdateContactBranchNotFound, ContactMetadata{}, "updateContact", sdkgo.FailureNotFound, "contact was not found")
	}
	if status != http.StatusOK {
		return contactWriteBranch(UpdateContactBranchProviderRejected, ContactMetadata{}, "updateContact", sdkgo.FailureProviderRejection, "GetResponse rejected HTTP "+strconv.Itoa(status))
	}
	if err != nil {
		return contactWriteBranch(UpdateContactBranchInvalidResponse, ContactMetadata{}, "updateContact", sdkgo.FailureProtocol, "GetResponse returned an invalid response")
	}
	contact, err := decodeContact(response)
	if err != nil {
		return contactWriteBranch(UpdateContactBranchInvalidResponse, ContactMetadata{}, "updateContact", sdkgo.FailureProtocol, "GetResponse returned invalid contact JSON")
	}
	return sdkgo.NewMutationBranch(UpdateContactBranchUpdated, contact, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "getresponse", ProviderObjectID: contact.ContactID})
}

// Invoke deletes a contact by ID.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeleteContactOutput] {
	if !contactIDPattern.MatchString(input.ContactID) {
		return contactWriteBranch(DeleteContactBranchDefect, DeleteContactOutput{}, "deleteContact", sdkgo.FailureValidation, "contact ID is invalid")
	}
	_, status, retryAfter, err := operation.client.write(call, call.Context, http.MethodDelete, "/contacts/"+input.ContactID, nil)
	if errors.Is(err, errCredential) {
		return contactWriteBranch(DeleteContactBranchDefect, DeleteContactOutput{}, "deleteContact", sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[DeleteContactOutput](queryFailure("deleteContact", sdkgo.FailureTransport, "GetResponse request failed"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status == http.StatusServiceUnavailable || status >= 500 {
		return sdkgo.NewMutationRetry[DeleteContactOutput](queryFailure("deleteContact", sdkgo.FailureAvailability, "GetResponse is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return contactWriteBranch(DeleteContactBranchNotFound, DeleteContactOutput{}, "deleteContact", sdkgo.FailureNotFound, "contact was not found")
	}
	if status != http.StatusNoContent {
		return contactWriteBranch(DeleteContactBranchProviderRejected, DeleteContactOutput{}, "deleteContact", sdkgo.FailureProviderRejection, "GetResponse rejected HTTP "+strconv.Itoa(status))
	}
	return sdkgo.NewMutationBranch(DeleteContactBranchDeleted, DeleteContactOutput{ContactID: input.ContactID}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "getresponse", ProviderObjectID: input.ContactID})
}

type createContactDispatch struct {
	client     *Client
	token      string
	payload    []byte
	status     int
	retryAfter time.Duration
}

func (dispatch *createContactDispatch) send(ctx context.Context) error {
	var err error
	_, dispatch.status, dispatch.retryAfter, err = dispatch.client.writeWithToken(ctx, dispatch.token, http.MethodPost, "/contacts", dispatch.payload)
	return err
}

func (client *Client) findContact(call sdkgo.Call, input CreateContactInput) (bool, error) {
	query := url.Values{"query[email]": {input.Email}, "query[campaignId]": {input.CampaignID}}
	body, status, _, err := client.get(call, "/contacts?"+query.Encode())
	if err != nil || status != http.StatusOK {
		return false, errors.New("contact read-back failed")
	}
	var contacts []struct {
		Email    string `json:"email"`
		Campaign struct {
			CampaignID string `json:"campaignId"`
		} `json:"campaign"`
	}
	if err := json.Unmarshal(body, &contacts); err != nil {
		return false, err
	}
	for _, contact := range contacts {
		if strings.EqualFold(contact.Email, input.Email) && contact.Campaign.CampaignID == input.CampaignID {
			return true, nil
		}
	}
	return false, nil
}

func (client *Client) write(call sdkgo.Call, ctx context.Context, method, path string, payload []byte) ([]byte, int, time.Duration, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, 0, 0, errCredential
	}
	return client.writeWithToken(ctx, credentials.APIKey.Reveal(), method, path, payload)
}

func (client *Client) writeWithToken(ctx context.Context, token, method, path string, payload []byte) ([]byte, int, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, 0, errRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Auth-Token", "api-key "+token)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		if err := singledispatch.PrepareHTTPWrite(request); err != nil {
			return nil, 0, 0, errRequest
		}
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, 0, 0, errRequest
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return body, response.StatusCode, retryAfter, err
}

func createContactReceipt(call sdkgo.Call) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "getresponse"}
}

func createContactUncertain(output CreateContactOutput, message string) sdkgo.MutationAttempt[CreateContactOutput] {
	return sdkgo.NewMutationUncertain(output, queryFailure("createContact", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}

func createContactBranch(branch sdkgo.BranchID, output CreateContactOutput, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreateContactOutput] {
	failure := queryFailure("createContact", kind, message)
	return sdkgo.NewMutationBranch(branch, output, &failure, sdkgo.Receipt{})
}

func contactWriteBranch[T any](branch sdkgo.BranchID, output T, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, output, &failure, sdkgo.Receipt{})
}
