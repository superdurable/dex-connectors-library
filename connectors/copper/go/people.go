// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PersonMetadata contains the public identity and details of a Copper person.
type PersonMetadata struct {
	// ID is the stable numeric person ID.
	ID int64 `json:"id"`
	// Name is the person's display name.
	Name string `json:"name"`
	// Email is the first work email, when returned.
	Email string `json:"email,omitempty"`
	// Details are the optional notes.
	Details string `json:"details,omitempty"`
}

// CreatePersonInput supplies the new person's name and work email.
type CreatePersonInput struct {
	// Name is the required display name.
	Name string `json:"name"`
	// Email is a required work email used to check a previous send.
	Email string `json:"email"`
}

// GetPersonInput identifies a person by positive numeric ID.
type GetPersonInput struct {
	// PersonID is a positive decimal Copper person ID.
	PersonID string `json:"personId"`
}

// UpdatePersonInput changes the named fields of one person.
type UpdatePersonInput struct {
	// PersonID is a positive numeric ID.
	PersonID string `json:"personId"`
	// Name replaces the display name.
	Name string `json:"name"`
	// Details replaces the notes; blank clears them.
	Details string `json:"details"`
}

// DeletePersonInput identifies the person to remove permanently.
type DeletePersonInput struct {
	// PersonID is a positive decimal Copper person ID.
	PersonID string `json:"personId"`
}

// DeletePersonOutput confirms a deleted person ID.
type DeletePersonOutput struct {
	// ID is the deleted numeric ID.
	ID int64 `json:"id"`
	// Deleted is true when Copper confirms deletion.
	Deleted bool `json:"deleted"`
}

// CreatePersonOperation creates a person with a bounded send count.
type CreatePersonOperation struct{ client *Client }

// GetPersonOperation reads a person by ID.
type GetPersonOperation struct{ client *Client }

// UpdatePersonOperation changes only the name and details.
type UpdatePersonOperation struct{ client *Client }

// DeletePersonOperation removes a person permanently.
type DeletePersonOperation struct{ client *Client }

// CreatePerson binds the mutation to this client.
func (client *Client) CreatePerson() CreatePersonOperation { return CreatePersonOperation{client} }

// GetPerson binds the query to this client.
func (client *Client) GetPerson() GetPersonOperation { return GetPersonOperation{client} }

// UpdatePerson binds the mutation to this client.
func (client *Client) UpdatePerson() UpdatePersonOperation { return UpdatePersonOperation{client} }

// DeletePerson binds the mutation to this client.
func (client *Client) DeletePerson() DeletePersonOperation { return DeletePersonOperation{client} }

// Definition returns the generated mutation contract.
func (CreatePersonOperation) Definition() sdkgo.MutationDefinition { return CreatePersonDefinition }

// Definition returns the generated query contract.
func (GetPersonOperation) Definition() sdkgo.QueryDefinition { return GetPersonDefinition }

// Definition returns the generated mutation contract.
func (UpdatePersonOperation) Definition() sdkgo.MutationDefinition { return UpdatePersonDefinition }

// Definition returns the generated mutation contract.
func (DeletePersonOperation) Definition() sdkgo.MutationDefinition { return DeletePersonDefinition }

// IdempotencyKey identifies attempts; Copper does not deduplicate it.
func (CreatePersonOperation) IdempotencyKey(id sdkgo.CallID, _ CreatePersonInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of an absolute field update.
func (UpdatePersonOperation) IdempotencyKey(id sdkgo.CallID, _ UpdatePersonInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of a deletion.
func (DeletePersonOperation) IdempotencyKey(id sdkgo.CallID, _ DeletePersonInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke reads one person without custom fields.
func (operation GetPersonOperation) Invoke(call sdkgo.Call, input GetPersonInput) sdkgo.QueryAttempt[PersonMetadata] {
	branches := queryBranches{found: GetPersonBranchFound, notFound: GetPersonBranchNotFound, rejected: GetPersonBranchProviderRejected, invalid: GetPersonBranchInvalidResponse, defect: GetPersonBranchDefect}
	if !validPersonID(input.PersonID) {
		return queryBranch("getPerson", branches.defect, PersonMetadata{}, sdkgo.FailureValidation, "person ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getPerson", "/people/"+input.PersonID, branches, decodePerson)
}

// Invoke replaces one person's name and notes, leaving other provider fields intact.
func (operation UpdatePersonOperation) Invoke(call sdkgo.Call, input UpdatePersonInput) sdkgo.MutationAttempt[PersonMetadata] {
	if !validPersonID(input.PersonID) || strings.TrimSpace(input.Name) == "" {
		return personFailure[PersonMetadata]("updatePerson", UpdatePersonBranchDefect, sdkgo.FailureValidation, "person ID and name are required")
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Details string `json:"details"`
	}{input.Name, input.Details})
	response := operation.client.personRequest(call.Context, call, http.MethodPut, "/people/"+input.PersonID, body)
	if retry, failure := personResponseFailure("updatePerson", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[PersonMetadata](*failure, response.retryAfter)
		}
		branch := UpdatePersonBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdatePersonBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = UpdatePersonBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, PersonMetadata{}, failure, sdkgo.Receipt{})
	}
	person, err := decodePerson(response.body)
	if response.status != http.StatusOK || err != nil {
		return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("updatePerson", sdkgo.FailureProtocol, "Copper update response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdatePersonBranchUpdated, person, nil, sdkgo.Receipt{})
}

// Invoke permanently removes one person.
func (operation DeletePersonOperation) Invoke(call sdkgo.Call, input DeletePersonInput) sdkgo.MutationAttempt[DeletePersonOutput] {
	if !validPersonID(input.PersonID) {
		return personFailure[DeletePersonOutput]("deletePerson", DeletePersonBranchDefect, sdkgo.FailureValidation, "person ID must be positive")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodDelete, "/people/"+input.PersonID, nil)
	if retry, failure := personResponseFailure("deletePerson", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[DeletePersonOutput](*failure, response.retryAfter)
		}
		branch := DeletePersonBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeletePersonBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = DeletePersonBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeletePersonOutput{}, failure, sdkgo.Receipt{})
	}
	var wire struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &wire) != nil || wire.ID <= 0 || !wire.Deleted {
		return sdkgo.NewMutationRetry[DeletePersonOutput](queryFailure("deletePerson", sdkgo.FailureProtocol, "Copper deletion response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeletePersonBranchDeleted, DeletePersonOutput{wire.ID, true}, nil, sdkgo.Receipt{})
}

// Invoke creates a person and checks email search before any possible resend.
func (operation CreatePersonOperation) Invoke(call sdkgo.Call, input CreatePersonInput) sdkgo.MutationAttempt[PersonMetadata] {
	if strings.TrimSpace(input.Name) == "" || !strings.Contains(input.Email, "@") || strings.ContainsAny(input.Email, "\r\n") {
		return personFailure[PersonMetadata]("createPerson", CreatePersonBranchDefect, sdkgo.FailureValidation, "name and work email are required")
	}
	fence, err := singledispatch.NewFence(call, CreatePersonDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return personFailure[PersonMetadata]("createPerson", CreatePersonBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return personUncertain("previous person creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		found, isKnown, retry := operation.client.findPersonByEmail(call, input.Email, input.Name)
		if retry {
			return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("createPerson", sdkgo.FailureAvailability, "Copper email search unavailable"), 0)
		}
		if found != nil {
			return sdkgo.NewMutationBranch(CreatePersonBranchCreated, *found, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return personUncertain("previous person creation cannot be confirmed")
		}
	}
	body, _ := json.Marshal(struct {
		Name   string `json:"name"`
		Emails []struct {
			Email    string `json:"email"`
			Category string `json:"category"`
		} `json:"emails"`
	}{Name: input.Name, Emails: []struct {
		Email    string `json:"email"`
		Category string `json:"category"`
	}{{input.Email, "work"}}})
	var response personResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.personRequest(requestContext, call, http.MethodPost, "/people", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("createPerson", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("createPerson", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("createPerson", kind, "Copper did not create the person"), response.retryAfter)
	}
	if response.err != nil || response.status >= 500 {
		return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("createPerson", sdkgo.FailureTransport, "person creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return personUncertain("an earlier person creation may have succeeded")
		}
		return personFailure[PersonMetadata]("createPerson", CreatePersonBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	person, decodeErr := decodePerson(response.body)
	if response.status != http.StatusOK || decodeErr != nil {
		return sdkgo.NewMutationRetry[PersonMetadata](queryFailure("createPerson", sdkgo.FailureProtocol, "person creation response is invalid"), 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(person.ID, 10))
	return sdkgo.NewMutationBranch(CreatePersonBranchCreated, person, nil, sdkgo.Receipt{})
}

type personResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) personRequest(requestContext context.Context, call sdkgo.Call, method, path string, body []byte) personResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return personResponse{err: errCredential}
	}
	apiKey, email := credentials.APIKey.Reveal(), credentials.UserEmail.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) || !providerhttp.IsHeaderSafeCredential(email) || !strings.Contains(email, "@") {
		return personResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return personResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-PW-AccessToken", apiKey)
	request.Header.Set("X-PW-Application", "developer_api")
	request.Header.Set("X-PW-UserEmail", email)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return personResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return personResponse{responseBody, response.StatusCode, retryAfter, err}
}

func (client *Client) findPersonByEmail(call sdkgo.Call, email, name string) (*PersonMetadata, bool, bool) {
	body, _ := json.Marshal(map[string]string{"email": email})
	response := client.personRequest(call.Context, call, http.MethodPost, "/people/search", body)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var people []json.RawMessage
	if json.Unmarshal(response.body, &people) != nil {
		return nil, false, true
	}
	for _, item := range people {
		person, err := decodePerson(item)
		if err != nil {
			return nil, false, true
		}
		if strings.EqualFold(person.Email, email) && person.Name == name {
			return &person, true, false
		}
	}
	return nil, true, false
}

func decodePerson(body []byte) (PersonMetadata, error) {
	var wire struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Details string `json:"details"`
		Emails  []struct {
			Email    string `json:"email"`
			Category string `json:"category"`
		} `json:"emails"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PersonMetadata{}, err
	}
	if wire.ID <= 0 || strings.TrimSpace(wire.Name) == "" {
		return PersonMetadata{}, fmt.Errorf("person identity missing")
	}
	person := PersonMetadata{ID: wire.ID, Name: wire.Name, Details: wire.Details}
	for _, address := range wire.Emails {
		if address.Category == "work" {
			person.Email = address.Email
			break
		}
	}
	return person, nil
}

func validPersonID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 63)
	return err == nil && value > 0 && strings.TrimSpace(id) == id
}

func personResponseFailure(operation string, response personResponse) (bool, *sdkgo.Failure) {
	var kind sdkgo.FailureKind
	switch {
	case errors.Is(response.err, errCredential):
		kind = sdkgo.FailureAuthentication
	case response.err != nil:
		kind = sdkgo.FailureTransport
	case response.status == http.StatusTooManyRequests:
		kind = sdkgo.FailureRateLimit
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		kind = sdkgo.FailureAvailability
	case response.status >= 400:
		kind = sdkgo.FailureProviderRejection
	default:
		return false, nil
	}
	failure := queryFailure(operation, kind, "Copper person request failed")
	return kind == sdkgo.FailureTransport || kind == sdkgo.FailureRateLimit || kind == sdkgo.FailureAvailability, &failure
}

func personFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func personUncertain(message string) sdkgo.MutationAttempt[PersonMetadata] {
	return sdkgo.NewMutationUncertain(PersonMetadata{}, queryFailure("createPerson", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
