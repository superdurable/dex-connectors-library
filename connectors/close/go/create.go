// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"bytes"
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

// CreateLeadInput supplies the fields for a new Close lead.
type CreateLeadInput struct {
	// Name is the lead's nonempty display name.
	Name string `json:"name"`
	// Description is optional free text.
	Description string `json:"description,omitempty"`
}

// CreateContactInput supplies a name and existing parent lead for a new contact.
type CreateContactInput struct {
	// LeadID identifies the existing parent lead.
	LeadID string `json:"leadId"`
	// Name is the contact's nonempty display name.
	Name string `json:"name"`
	// Title is an optional job title.
	Title string `json:"title,omitempty"`
}

// CreateLeadOperation creates a lead with bounded possible sends.
type CreateLeadOperation struct{ client *Client }

// CreateLead binds lead creation to this client.
func (client *Client) CreateLead() CreateLeadOperation { return CreateLeadOperation{client} }

// Definition returns the generated createLead contract.
func (CreateLeadOperation) Definition() sdkgo.MutationDefinition { return CreateLeadDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one lead and reconciles any known provider identity before resending.
func (operation CreateLeadOperation) Invoke(call sdkgo.Call, input CreateLeadInput) sdkgo.MutationAttempt[Lead] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || len(input.Description) > 10000 {
		return closeMutationBranch[Lead]("createLead", CreateLeadBranchDefect, sdkgo.FailureValidation, "name or description is invalid")
	}
	requestBody, err := json.Marshal(struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}{input.Name, input.Description})
	if err != nil {
		return closeMutationBranch[Lead]("createLead", CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createLead", "/lead/", requestBody, CreateLeadBranchCreated, CreateLeadBranchProviderRejected, CreateLeadBranchDefect, decodeGetLead, validLeadID)
}

// CreateContactOperation creates a contact with bounded possible sends.
type CreateContactOperation struct{ client *Client }

// CreateContact binds contact creation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated createContact contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a contact under an existing lead.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[Contact] {
	if !validLeadID(input.LeadID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 1000 || len(input.Title) > 1000 {
		return closeMutationBranch[Contact]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "leadId, name, or title is invalid")
	}
	requestBody, err := json.Marshal(struct {
		LeadID string `json:"lead_id"`
		Name   string `json:"name"`
		Title  string `json:"title,omitempty"`
	}{input.LeadID, input.Name, input.Title})
	if err != nil {
		return closeMutationBranch[Contact]("createContact", CreateContactBranchDefect, sdkgo.FailureLocalDefect, "contact request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createContact", "/contact/", requestBody, CreateContactBranchCreated, CreateContactBranchProviderRejected, CreateContactBranchDefect, decodeContact, validContactID)
}

func invokeCloseCreate[T interface {
	Lead | Contact | Opportunity | Task | Note
}](client *Client, call sdkgo.Call, operation, path string, body []byte, createdBranch, rejectedBranch, defectBranch sdkgo.BranchID, decode func([]byte) (T, error), validID func(string) bool) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, 45*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return closeMutationBranch[T](operation, defectBranch, sdkgo.FailureLocalDefect, "dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return closeCreateUncertain[T](operation, sdkgo.FailureTransport, "earlier creation cannot be reconciled")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return closeCreateUncertain[T](operation, sdkgo.FailureAuthentication, "earlier creation may have succeeded; connection credentials unavailable")
		}
		return closeMutationBranch[T](operation, defectBranch, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if decision.Marker.ProviderID != "" {
		if !validID(decision.Marker.ProviderID) {
			return closeCreateUncertain[T](operation, sdkgo.FailureProtocol, "saved provider identity is invalid")
		}
		value, found, retry := readBackCloseCreate(client, call.Context, credentials, path+decision.Marker.ProviderID+"/", decode)
		if found {
			return sdkgo.NewMutationBranch(createdBranch, value, nil, sdkgo.Receipt{})
		}
		if retry {
			return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "creation read-back unavailable", 0)
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return closeCreateUncertain[T](operation, sdkgo.FailureTransport, "earlier creation cannot be confirmed")
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		request, requestErr := http.NewRequestWithContext(requestContext, http.MethodPost, client.baseURL+path, bytes.NewReader(body))
		if requestErr != nil {
			return requestErr
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		request.SetBasicAuth(credentials.APIKey.Reveal(), "")
		if requestErr := singledispatch.PrepareHTTPWrite(request); requestErr != nil {
			return requestErr
		}
		response, requestErr = client.httpClient.Do(request)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "creation was not dispatched", 0)
	}
	if err != nil {
		return retryOrUncertainCloseCreate[T](operation, sdkgo.FailureTransport, "creation outcome is unknown", canResendUnknown)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return closeMutationRetry[T](operation, kind, "provider did not process creation", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return retryOrUncertainCloseCreate[T](operation, sdkgo.FailureAvailability, "creation outcome is unknown", canResendUnknown)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return closeCreateUncertain[T](operation, sdkgo.FailureProviderRejection, "earlier creation may have succeeded")
		}
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return closeMutationBranch[T](operation, rejectedBranch, kind, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return closeCreateUncertain[T](operation, sdkgo.FailureTransport, "creation succeeded but response was incomplete")
	}
	value, err := decode(responseBody)
	if err != nil {
		return closeCreateUncertain[T](operation, sdkgo.FailureProtocol, "creation succeeded but response identity is invalid")
	}
	providerID := closeCreatedID(value)
	_ = fence.RecordProviderID(providerID)
	return sdkgo.NewMutationBranch(createdBranch, value, nil, sdkgo.Receipt{})
}

func closeCreatedID[T interface {
	Lead | Contact | Opportunity | Task | Note
}](value T) string {
	switch resource := any(value).(type) {
	case Lead:
		return resource.ID
	case Contact:
		return resource.ID
	case Opportunity:
		return resource.ID
	case Task:
		return resource.ID
	case Note:
		return resource.ID
	default:
		return ""
	}
}

func readBackCloseCreate[T any](client *Client, ctx context.Context, credentials Credentials, path string, decode func([]byte) (T, error)) (T, bool, bool) {
	var zero T
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return zero, false, true
	}
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(credentials.APIKey.Reveal(), "")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return zero, false, true
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return zero, false, false
	}
	if response.StatusCode != http.StatusOK {
		return zero, false, true
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return zero, false, true
	}
	value, err := decode(responseBody)
	return value, err == nil, err != nil
}

func retryOrUncertainCloseCreate[T any](operation string, kind sdkgo.FailureKind, message string, canResend bool) sdkgo.MutationAttempt[T] {
	if canResend {
		return closeMutationRetry[T](operation, kind, message, 0)
	}
	return closeCreateUncertain[T](operation, kind, message)
}

func closeCreateUncertain[T any](operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	return sdkgo.NewMutationUncertain(zero, sdkgo.Failure{Kind: kind, Provider: "close", Operation: operation, Message: message}, sdkgo.Receipt{})
}
