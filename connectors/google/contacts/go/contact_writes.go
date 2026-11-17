// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateContactInput replaces a contact's name and email at a known revision.
type UpdateContactInput struct {
	// ResourceName is the people/{id} identity returned by listContacts.
	ResourceName string `json:"resourceName"`
	// Etag is the opaque source etag returned by getContactRevision.
	Etag string `json:"etag"`
	// GivenName is the new nonempty given name.
	GivenName string `json:"givenName"`
	// FamilyName is the optional new family name; blank clears it.
	FamilyName string `json:"familyName,omitempty"`
	// EmailAddress is the optional new address; blank removes the contact email.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// DeleteContactInput selects one People API contact to delete.
type DeleteContactInput struct {
	// ResourceName is the people/{id} identity returned by listContacts.
	ResourceName string `json:"resourceName"`
}

// DeletedContact identifies an absent contact.
type DeletedContact struct {
	// ResourceName is the now-absent contact identity.
	ResourceName string `json:"resourceName"`
}

// UpdateContactOperation replaces selected contact fields.
type UpdateContactOperation struct{ client *Client }

// DeleteContactOperation deletes a People API contact.
type DeleteContactOperation struct{ client *Client }

// UpdateContact binds the guarded contact update Mutation.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// DeleteContact binds the contact deletion Mutation.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated contact update contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// Definition returns the generated contact deletion contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey identifies the guarded update across retries.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the deletion across retries.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a name and email only when the caller's source revision still matches.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[Contact] {
	if !contactResourceName.MatchString(input.ResourceName) || input.Etag == "" || len(input.Etag) > 1024 || !utf8.ValidString(input.Etag) || !validCreateContactInput(CreateContactInput{GivenName: input.GivenName, FamilyName: input.FamilyName, EmailAddress: input.EmailAddress}) {
		return contactUpdateBranch("defect", Contact{}, sdkgo.FailureValidation, "contact update input is invalid")
	}
	payload := struct {
		ResourceName string `json:"resourceName"`
		Metadata     struct {
			Sources []struct {
				Type string `json:"type"`
				Etag string `json:"etag"`
			} `json:"sources"`
		} `json:"metadata"`
		Names []struct {
			GivenName  string `json:"givenName"`
			FamilyName string `json:"familyName,omitempty"`
		} `json:"names"`
		EmailAddresses []struct {
			Value string `json:"value"`
		} `json:"emailAddresses"`
	}{ResourceName: input.ResourceName}
	payload.EmailAddresses = make([]struct {
		Value string `json:"value"`
	}, 0)
	payload.Metadata.Sources = append(payload.Metadata.Sources, struct {
		Type string `json:"type"`
		Etag string `json:"etag"`
	}{Type: "CONTACT", Etag: input.Etag})
	payload.Names = append(payload.Names, struct {
		GivenName  string `json:"givenName"`
		FamilyName string `json:"familyName,omitempty"`
	}{GivenName: input.GivenName, FamilyName: input.FamilyName})
	if input.EmailAddress != "" {
		payload.EmailAddresses = append(payload.EmailAddresses, struct {
			Value string `json:"value"`
		}{Value: input.EmailAddress})
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return contactUpdateBranch("defect", Contact{}, sdkgo.FailureLocalDefect, "contact update could not be encoded")
	}
	query := url.Values{"updatePersonFields": {"names,emailAddresses"}, "personFields": {"names,emailAddresses"}}
	path := "/v1/" + input.ResourceName + ":updateContact?" + query.Encode()
	response := operation.client.requestWithBody(call, call.Context, http.MethodPatch, path, requestBody)
	if errors.Is(response.err, errCredential) {
		return contactUpdateBranch("defect", Contact{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.status == http.StatusNotFound && response.err == nil {
		return contactUpdateBranch("notFound", Contact{}, sdkgo.FailureProviderRejection, "contact was not found")
	}
	if response.status == http.StatusPreconditionFailed || (response.status == http.StatusBadRequest && bytes.Contains(response.body, []byte("failedPrecondition"))) {
		return operation.confirmUpdatedContact(call, input)
	}
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return contactUpdateRetry(sdkgo.FailureAvailability, "Google Contacts update is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return contactUpdateBranch("providerRejected", Contact{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
	var person personWire
	if err := json.Unmarshal(response.body, &person); err != nil {
		return contactUpdateRetry(sdkgo.FailureProtocol, "Google returned invalid updated contact", 0)
	}
	contact, ok := person.project()
	if !ok || contact.ResourceName != input.ResourceName {
		return contactUpdateRetry(sdkgo.FailureProtocol, "Google returned the wrong updated contact", 0)
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("updated"), contact, nil, sdkgo.Receipt{})
}

func (operation UpdateContactOperation) confirmUpdatedContact(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[Contact] {
	query := url.Values{"personFields": {"names,emailAddresses,metadata"}}
	response := operation.client.request(call, "/v1/"+input.ResourceName+"?"+query.Encode())
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return contactUpdateRetry(sdkgo.FailureAvailability, "contact revision could not be confirmed", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return contactUpdateBranch("staleRevision", Contact{}, sdkgo.FailureProviderRejection, "contact revision changed before update")
	}
	var person personWire
	if err := json.Unmarshal(response.body, &person); err != nil {
		return contactUpdateRetry(sdkgo.FailureProtocol, "Google returned invalid contact revision", 0)
	}
	contact, ok := person.project()
	if !ok || contact.ResourceName != input.ResourceName {
		return contactUpdateRetry(sdkgo.FailureProtocol, "Google returned the wrong contact revision", 0)
	}
	if len(person.Names) > 0 && person.Names[0].GivenName == input.GivenName && person.Names[0].FamilyName == input.FamilyName && contact.EmailAddress == input.EmailAddress {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("updated"), contact, nil, sdkgo.Receipt{})
	}
	return contactUpdateBranch("staleRevision", Contact{}, sdkgo.FailureProviderRejection, "contact changed after its revision was read")
}

// Invoke deletes a contact and treats an already absent contact as complete.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if !contactResourceName.MatchString(input.ResourceName) {
		return contactDeleteBranch("defect", DeletedContact{}, sdkgo.FailureValidation, "contact resource name is invalid")
	}
	response := operation.client.requestWithBody(call, call.Context, http.MethodDelete, "/v1/"+input.ResourceName+":deleteContact", nil)
	if errors.Is(response.err, errCredential) {
		return contactDeleteBranch("defect", DeletedContact{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.err != nil {
		return contactDeleteRetry(sdkgo.FailureTransport, "contact deletion response failed", 0)
	}
	switch response.status {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), DeletedContact{ResourceName: input.ResourceName}, nil, sdkgo.Receipt{})
	case http.StatusTooManyRequests:
		return contactDeleteRetry(sdkgo.FailureRateLimit, "Google rate limited the deletion", response.retryAfter)
	case http.StatusRequestTimeout, http.StatusServiceUnavailable:
		return contactDeleteRetry(sdkgo.FailureAvailability, "contact deletion is temporarily unavailable", response.retryAfter)
	default:
		if response.status >= 500 {
			return contactDeleteRetry(sdkgo.FailureAvailability, "contact deletion is temporarily unavailable", response.retryAfter)
		}
		return contactDeleteBranch("providerRejected", DeletedContact{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
}

func contactUpdateBranch(branch sdkgo.BranchID, value Contact, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Contact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "updateContact", Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
func contactUpdateRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[Contact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "updateContact", Message: message}
	return sdkgo.NewMutationRetry[Contact](failure, delay)
}
func contactDeleteBranch(branch sdkgo.BranchID, value DeletedContact, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedContact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "deleteContact", Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
func contactDeleteRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedContact] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: "deleteContact", Message: message}
	return sdkgo.NewMutationRetry[DeletedContact](failure, delay)
}

