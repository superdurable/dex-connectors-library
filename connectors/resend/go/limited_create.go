// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateContactInput creates a global contact under its unique email address.
type CreateContactInput struct {
	// Email is the contact's unique email address.
	Email string `json:"email"`
	// FirstName is the optional given name.
	FirstName string `json:"firstName"`
	// LastName is the optional family name.
	LastName string `json:"lastName"`
	// Unsubscribed is the desired global subscription state.
	Unsubscribed bool `json:"unsubscribed"`
}

// CreateDomainInput creates one sending domain.
type CreateDomainInput struct {
	// Name is the DNS domain name to register.
	Name string `json:"name"`
}

// DomainReference identifies a newly created domain.
type DomainReference struct {
	// ID is the stable Resend domain ID.
	ID string `json:"id"`
}

// CreateContactOperation creates a global contact with bounded resend.
type CreateContactOperation struct{ client *Client }

// CreateDomainOperation creates a sending domain with bounded resend.
type CreateDomainOperation struct{ client *Client }

// CreateContact binds global contact creation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// CreateDomain binds sending-domain creation to this client.
func (client *Client) CreateDomain() CreateDomainOperation { return CreateDomainOperation{client} }

// Definition returns the generated createContact contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// Definition returns the generated createDomain contract.
func (CreateDomainOperation) Definition() sdkgo.MutationDefinition { return CreateDomainDefinition }

// IdempotencyKey correlates attempts; Resend does not document deduplication for contact creation.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts; Resend does not document deduplication for domain creation.
func (CreateDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a contact identified by Email.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactReference] {
	if strings.TrimSpace(input.Email) == "" || !strings.Contains(input.Email, "@") {
		return mutationFailure[ContactReference]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "valid contact email is required")
	}
	body, err := json.Marshal(struct {
		Email        string `json:"email"`
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		Unsubscribed bool   `json:"unsubscribed"`
	}{input.Email, input.FirstName, input.LastName, input.Unsubscribed})
	if err != nil {
		return mutationFailure[ContactReference]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "contact cannot be encoded")
	}
	request := resendLimitedCreate[ContactReference]{client: operation.client, call: call, operation: "createContact", path: "/contacts", body: body, timeout: CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateContactBranchCreated, rejected: CreateContactBranchProviderRejected, defect: CreateContactBranchDefect, decode: decodeContactReference, readBack: func(call sdkgo.Call) (ContactReference, readBackState) { return operation.readBackContact(call, input) }}
	return request.invoke()
}

// Invoke creates a domain identified by Name.
func (operation CreateDomainOperation) Invoke(call sdkgo.Call, input CreateDomainInput) sdkgo.MutationAttempt[DomainReference] {
	if strings.TrimSpace(input.Name) == "" || strings.ContainsAny(input.Name, " /\t\n") {
		return mutationFailure[DomainReference]("createDomain", CreateDomainBranchDefect, sdkgo.FailureValidation, "DNS domain name is required")
	}
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{input.Name})
	if err != nil {
		return mutationFailure[DomainReference]("createDomain", CreateDomainBranchDefect, sdkgo.FailureValidation, "domain cannot be encoded")
	}
	request := resendLimitedCreate[DomainReference]{client: operation.client, call: call, operation: "createDomain", path: "/domains", body: body, timeout: CreateDomainDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateDomainBranchCreated, rejected: CreateDomainBranchProviderRejected, defect: CreateDomainBranchDefect, decode: decodeDomainReference, readBack: func(call sdkgo.Call) (DomainReference, readBackState) { return operation.readBackDomain(call, input) }}
	return request.invoke()
}

type readBackState uint8

const (
	readBackRetry readBackState = iota
	readBackAbsent
	readBackMatched
	readBackConflicted
)

type resendLimitedCreate[T any] struct {
	client    *Client
	call      sdkgo.Call
	operation string
	path      string
	body      []byte
	timeout   time.Duration
	created   sdkgo.BranchID
	rejected  sdkgo.BranchID
	defect    sdkgo.BranchID
	decode    func([]byte) (T, error)
	readBack  func(sdkgo.Call) (T, readBackState)
}

func (request resendLimitedCreate[T]) invoke() sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(request.call, request.timeout, singledispatch.WithMaxSends(request.call.MaxSends()))
	if err != nil {
		return mutationFailure[T](request.operation, request.defect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return request.uncertain("create checkpoint cannot be trusted")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		_, state := request.readBack(request.call)
		switch state {
		case readBackMatched:
			return request.uncertain("resource fields do not identify this create")
		case readBackRetry:
			return request.retry(sdkgo.FailureAvailability, "provider read-back is temporarily unavailable", 0)
		case readBackConflicted:
			return request.uncertain("create outcome conflicts with the provider resource")
		case readBackAbsent:
			if decision.Kind == singledispatch.ReadBackRequired {
				return request.uncertain("send cap reached without confirming creation")
			}
		}
	}
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	canResendUnknown := decision.MaximumPriorSends+1 < request.call.MaxSends()
	httpRequest, err := request.client.newRequest(request.call, http.MethodPost, request.path, request.body)
	if err != nil {
		if hasPossibleEarlierSend {
			return request.uncertain("connection unavailable after a possible create")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "no-send checkpoint unavailable", 0)
		}
		return mutationFailure[T](request.operation, request.defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	if err := singledispatch.PrepareHTTPWrite(httpRequest); err != nil {
		return mutationFailure[T](request.operation, request.defect, sdkgo.FailureLocalDefect, "create request cannot be prepared")
	}
	dispatch := resendCreateDispatch{client: request.client, request: httpRequest}
	err = fence.Dispatch(dispatch.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return request.retry(sdkgo.FailureAvailability, "create dispatch did not start", 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if dispatch.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return request.retry(kind, "provider did not process create", dispatch.retryAfter)
	}
	if err != nil || dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		if canResendUnknown {
			return request.retry(sdkgo.FailureAvailability, "create outcome is unknown", dispatch.retryAfter)
		}
		return request.uncertain("send cap reached with unknown create outcome")
	}
	if dispatch.status == http.StatusConflict {
		if !hasPossibleEarlierSend {
			return mutationFailure[T](request.operation, request.rejected, sdkgo.FailureProviderRejection, "provider reports the resource already exists")
		}
		_, state := request.readBack(request.call)
		if state == readBackMatched {
			return request.uncertain("resource fields do not identify this create")
		}
		if state == readBackRetry {
			return request.retry(sdkgo.FailureAvailability, "provider read-back is temporarily unavailable", 0)
		}
		if hasPossibleEarlierSend {
			return request.uncertain("earlier create may have succeeded")
		}
		return mutationFailure[T](request.operation, request.rejected, sdkgo.FailureProviderRejection, "provider reports the resource already exists")
	}
	if dispatch.status != http.StatusOK && dispatch.status != http.StatusCreated && dispatch.status != http.StatusAccepted {
		if hasPossibleEarlierSend {
			return request.uncertain("earlier create may have succeeded")
		}
		return mutationFailure[T](request.operation, request.rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(dispatch.status))
	}
	value, err := request.decode(dispatch.body)
	if err != nil {
		_, state := request.readBack(request.call)
		if state == readBackMatched {
			return request.uncertain("resource fields do not identify this create")
		}
		if state == readBackRetry || (state == readBackAbsent && canResendUnknown) {
			return request.retry(sdkgo.FailureAvailability, "accepted create could not be confirmed", 0)
		}
		return request.uncertain("provider accepted create without a confirmable ID")
	}
	return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
}

func (request resendLimitedCreate[T]) retry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](newFailure(request.operation, kind, message), retryAfter)
}
func (request resendLimitedCreate[T]) uncertain(message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationUncertain(*new(T), newFailure(request.operation, sdkgo.FailureTransport, message), sdkgo.Receipt{})
}

type resendCreateDispatch struct {
	client     *Client
	request    *http.Request
	status     int
	body       []byte
	retryAfter time.Duration
}

func (dispatch *resendCreateDispatch) send(ctx context.Context) error {
	response, err := dispatch.client.httpClient.Do(dispatch.request.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	dispatch.status = response.StatusCode
	dispatch.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	dispatch.body, err = providerhttp.ReadBoundedBody(response.Body, dispatch.client.maxResponseBytes)
	return err
}

func (operation CreateContactOperation) readBackContact(call sdkgo.Call, input CreateContactInput) (ContactReference, readBackState) {
	body, state := operation.client.readBackResource(call, "/contacts/"+url.PathEscape(input.Email))
	if state != readBackMatched {
		return ContactReference{}, state
	}
	contact, err := decodeContact(body)
	if err != nil {
		return ContactReference{}, readBackRetry
	}
	if !strings.EqualFold(contact.Email, input.Email) || contact.FirstName != input.FirstName || contact.LastName != input.LastName || contact.Unsubscribed != input.Unsubscribed {
		return ContactReference{}, readBackConflicted
	}
	return ContactReference{ID: contact.ID}, readBackMatched
}

func (operation CreateDomainOperation) readBackDomain(call sdkgo.Call, input CreateDomainInput) (DomainReference, readBackState) {
	body, state := operation.client.readBackResource(call, "/domains")
	if state != readBackMatched {
		return DomainReference{}, state
	}
	var page DomainPage
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return DomainReference{}, readBackRetry
	}
	for _, domain := range page.Data {
		if strings.EqualFold(domain.Name, input.Name) && domain.ID != "" {
			return DomainReference{ID: domain.ID}, readBackMatched
		}
	}
	if page.HasMore {
		return DomainReference{}, readBackRetry
	}
	return DomainReference{}, readBackAbsent
}

func (client *Client) readBackResource(call sdkgo.Call, path string) ([]byte, readBackState) {
	request, err := client.newRequest(call, http.MethodGet, path, nil)
	if err != nil {
		return nil, readBackRetry
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, readBackRetry
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil, readBackAbsent
	}
	if response.StatusCode != http.StatusOK {
		return nil, readBackRetry
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return nil, readBackRetry
	}
	return body, readBackMatched
}

func decodeDomainReference(body []byte) (DomainReference, error) {
	var reference DomainReference
	err := json.Unmarshal(body, &reference)
	if err == nil && reference.ID == "" {
		err = fmt.Errorf("domain ID missing")
	}
	return reference, err
}
