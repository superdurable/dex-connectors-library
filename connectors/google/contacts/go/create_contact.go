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
	FamilyName string `json:"familyName"`
	// EmailAddress is an optional email address; blank omits it.
	EmailAddress string `json:"emailAddress"`
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

// Invoke sends at most once unless a definitive 429, 503, or pre-send failure permits repetition.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[Contact] {
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureLocalDefect, "Google Contacts dispatch fence is unavailable")
	}
	if fence.Decide().Kind != singledispatch.SendAllowed {
		return createUncertain(sdkgo.FailureTransport, "contact creation outcome cannot be confirmed; inspect Contacts before creating another")
	}
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
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return createFailure(CreateContactBranchDefect, sdkgo.FailureAuthentication, "connection requires reauthorization")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return createRetry(sdkgo.FailureTransport, "credential refresh checkpoint is unavailable", 0)
		}
		return createRetry(sdkgo.FailureTransport, "connection credentials could not be refreshed", 0)
	}
	if credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"personFields": {"names,emailAddresses"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/v1/people:createContact?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureLocalDefect, "contact request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return createFailure(CreateContactBranchDefect, sdkgo.FailureLocalDefect, "contact request is invalid")
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
		return createUncertain(sdkgo.FailureTransport, "contact creation outcome is unknown")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createRetry(sdkgo.FailureAvailability, "Google retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return createRetry(kind, "Google did not create the contact", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict {
		return createUncertain(sdkgo.FailureAvailability, "contact creation outcome is unknown")
	}
	if response.StatusCode == http.StatusUnauthorized {
		return createFailure(CreateContactBranchProviderRejected, sdkgo.FailureAuthentication, "Google rejected contact authorization")
	}
	if response.StatusCode == http.StatusForbidden {
		return createFailure(CreateContactBranchProviderRejected, sdkgo.FailureAuthorization, "Google denied contact creation")
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return createFailure(CreateContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return createUncertain(sdkgo.FailureProtocol, "Google returned an unexpected contact response")
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil || bytes.Contains(responseBody, []byte(credentials.AccessToken.Reveal())) {
		return createUncertain(sdkgo.FailureTransport, "Google created a contact but its response could not be read safely")
	}
	var person personWire
	if err := json.Unmarshal(responseBody, &person); err != nil {
		return createUncertain(sdkgo.FailureProtocol, "Google created a contact but returned invalid JSON")
	}
	contact, ok := person.project()
	if !ok {
		return createUncertain(sdkgo.FailureProtocol, "Google created a contact but returned no usable resource name")
	}
	_ = fence.RecordProviderID(contact.ResourceName)
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
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
