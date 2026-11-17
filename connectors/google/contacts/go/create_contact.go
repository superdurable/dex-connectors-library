// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateContactInput provides a name and optional email address for one contact.
type CreateContactInput struct {
	// GivenName is a nonempty contact given name.
	GivenName string `json:"givenName"`
	// FamilyName is an optional contact family name.
	FamilyName string `json:"familyName,omitempty"`
	// EmailAddress is an optional email address; blank omits it.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// CreateContactOperation creates one Google contact with a durable single-dispatch fence.
type CreateContactOperation struct{ client *Client }

// CreateContact binds the contact-creation Mutation.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated contact-creation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey records stable call identity; Google does not deduplicate on this key.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one contact. A saved resource name is read back instead of resent, and an
// unknown outcome is resent while the send cap allows; uncertain is selected only at the cap.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[Contact] {
	if !validCreateContactInput(input) {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureValidation, "contact name or email is invalid")
	}
	payload := struct {
		Names []struct {
			GivenName  string `json:"givenName"`
			FamilyName string `json:"familyName,omitempty"`
		} `json:"names"`
		EmailAddresses []struct {
			Value string `json:"value"`
		} `json:"emailAddresses,omitempty"`
	}{}
	payload.Names = append(payload.Names, struct {
		GivenName  string `json:"givenName"`
		FamilyName string `json:"familyName,omitempty"`
	}{GivenName: input.GivenName, FamilyName: input.FamilyName})
	if input.EmailAddress != "" {
		payload.EmailAddresses = append(payload.EmailAddresses, struct {
			Value string `json:"value"`
		}{Value: input.EmailAddress})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureValidation, "contact could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureLocalDefect, "Google Contacts dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return createRetry(sdkgo.FailureAvailability, "contact dispatch checkpoint is unreadable; no contact was sent in this attempt", 0)
	case singledispatch.ReadBackRequired:
		if decision.Marker.ProviderID != "" {
			return operation.readBackCreatedContact(call, decision.Marker.ProviderID)
		}
		return createUncertain(sdkgo.FailureTransport, "contact creation reached the send cap without confirmation; inspect Contacts before creating another")
	case singledispatch.ReadBackThenResendAllowed:
		// Google returns a resource name only for a contact it created, so a saved one is never resent.
		if decision.Marker.ProviderID != "" {
			return operation.readBackCreatedContact(call, decision.Marker.ProviderID)
		}
	}
	// This attempt's send is the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return createLocalFailure(hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection requires reauthorization")
		}
		if decision.Kind == singledispatch.SendAllowed {
			if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
				return createRetry(sdkgo.FailureTransport, "credential refresh checkpoint is unavailable", 0)
			}
		}
		return createRetry(sdkgo.FailureTransport, "connection credentials could not be refreshed", 0)
	}
	if credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return createLocalFailure(hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"personFields": {"names,emailAddresses"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/v1/people:createContact?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return createLocalFailure(hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "contact request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return createLocalFailure(hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "contact request is invalid")
	}
	var didGetConnection atomic.Bool
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		request = request.WithContext(httptrace.WithClientTrace(requestContext, trace))
		response, err = operation.client.httpClient.Do(request)
		return err
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createRetry(sdkgo.FailureTransport, "contact dispatch was not started", 0)
		}
		if !didGetConnection.Load() && fence.RecordCanResend() == nil {
			return createRetry(sdkgo.FailureTransport, "Google could not be reached before sending", 0)
		}
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "contact creation outcome is unknown", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createRetry(sdkgo.FailureAvailability, "Google retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return createRetry(kind, "Google did not create the contact", retryAfter)
	}
	responseBody, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	isResponseUnsafe := bytes.Contains(responseBody, []byte(credentials.AccessToken.Reveal()))
	var person personWire
	contact, hasContact := Contact{}, false
	if readErr == nil && !isResponseUnsafe && json.Unmarshal(responseBody, &person) == nil {
		contact, hasContact = person.project()
	}
	if hasContact {
		// A later attempt reads this contact back instead of creating another one.
		_ = fence.RecordProviderID(contact.ResourceName)
	}
	if isDefiniteContactRefusal(response.StatusCode) && !hasContact {
		return rejectOrSelectUncertain(fence, hasPossibleEarlierSend, contactRefusalFailureKind(response.StatusCode), "Google rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureAvailability, "contact creation outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), retryAfter)
	}
	if readErr != nil || isResponseUnsafe {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "Google contact response could not be read safely", 0)
	}
	if !hasContact {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureProtocol, "Google returned no usable contact resource name", 0)
	}
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
}

// readBackCreatedContact confirms a contact whose resource name an earlier attempt saved.
func (operation CreateContactOperation) readBackCreatedContact(call sdkgo.Call, resourceName string) sdkgo.MutationAttempt[Contact] {
	if !contactResourceName.MatchString(resourceName) {
		return createUncertain(sdkgo.FailureProtocol, "the saved contact resource name is invalid; inspect Contacts before creating another")
	}
	query := url.Values{"personFields": {"names,emailAddresses"}}
	response := operation.client.request(call, "/v1/"+resourceName+"?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return createUncertain(sdkgo.FailureAuthentication, "contact read-back credentials are unavailable; reconnect the connection to confirm the contact")
	}
	if errors.Is(response.err, errRequest) {
		return createRetry(sdkgo.FailureTransport, "contact read-back request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return createRetry(sdkgo.FailureRateLimit, "Google rate limited the contact read-back", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createRetry(sdkgo.FailureAvailability, "Google Contacts is temporarily unavailable for read-back", response.retryAfter)
	}
	var person personWire
	if response.status != http.StatusOK || response.err != nil || json.Unmarshal(response.body, &person) != nil {
		return createUncertain(sdkgo.FailureProtocol, "contact read-back could not confirm the contact; inspect Contacts before creating another")
	}
	contact, ok := person.project()
	if !ok || contact.ResourceName != resourceName {
		return createUncertain(sdkgo.FailureProtocol, "contact read-back returned another resource; inspect Contacts before creating another")
	}
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
}

// isDefiniteContactRefusal reports a status that proves Google refused the request.
func isDefiniteContactRefusal(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func contactRefusalFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

// resendOrSelectUncertain retries a possibly created contact while the send cap allows another send.
func resendOrSelectUncertain(canResendUnknownOutcome bool, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[Contact] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return createRetry(kind, message, delay)
	}
	return createUncertain(kind, message+"; the send cap is reached, so inspect Contacts before creating another")
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send may have created the contact.
func rejectOrSelectUncertain(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Contact] {
	if hasPossibleEarlierSend {
		return createUncertain(kind, message+" after an earlier unconfirmed send; inspect Contacts before creating another")
	}
	// The refusal ends the Step, so a lost checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return createFailure(CreateContactBranchProviderRejected, kind, message)
}

// createLocalFailure selects defect only when no earlier send of this call may have created the contact.
func createLocalFailure(hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Contact] {
	if hasPossibleEarlierSend {
		return createUncertain(kind, message+" after an earlier unconfirmed send; inspect Contacts before creating another")
	}
	return createFailure(CreateContactBranchDefect, kind, message)
}

func validCreateContactInput(input CreateContactInput) bool {
	if strings.TrimSpace(input.GivenName) == "" || len(input.GivenName) > 256 || len(input.FamilyName) > 256 || len(input.EmailAddress) > 320 ||
		!utf8.ValidString(input.GivenName) || !utf8.ValidString(input.FamilyName) || !utf8.ValidString(input.EmailAddress) || strings.ContainsAny(input.EmailAddress, "\r\n") {
		return false
	}
	if input.EmailAddress != "" && (!strings.Contains(input.EmailAddress, "@") || strings.ContainsAny(input.EmailAddress, " \t")) {
		return false
	}
	return true
}

func createFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Contact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "createContact", Message: message}
	return sdkgo.NewMutationBranch(branch, Contact{}, &failure, sdkgo.Receipt{})
}

func createRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[Contact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "createContact", Message: message}
	return sdkgo.NewMutationRetry[Contact](failure, delay)
}

func createUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Contact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "createContact", Message: message}
	return sdkgo.NewMutationUncertain(Contact{}, failure, sdkgo.Receipt{})
}
