// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"encoding/json"
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ContactRevision contains a contact and the source etag required for guarded updates.
type ContactRevision struct {
	// ResourceName is the People API identity returned by listContacts.
	ResourceName string `json:"resourceName"`
	// DisplayName is the first returned display name, if any.
	DisplayName string `json:"displayName"`
	// EmailAddress is the first returned email address, if any.
	EmailAddress string `json:"emailAddress"`
	// Etag is the opaque contact source revision to pass to updateContact.
	Etag string `json:"etag"`
}

// GetContactRevisionOperation reads a contact source revision.
type GetContactRevisionOperation struct{ client *Client }

// GetContactRevision binds the guarded-update prerequisite Query.
func (client *Client) GetContactRevision() GetContactRevisionOperation {
	return GetContactRevisionOperation{client}
}

// Definition returns the generated contact revision Query contract.
func (GetContactRevisionOperation) Definition() sdkgo.QueryDefinition {
	return GetContactRevisionDefinition
}

// Invoke gets a contact's current source etag and projected fields.
func (operation GetContactRevisionOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactRevision] {
	if !contactResourceName.MatchString(input.ResourceName) {
		return queryBranch("getContactRevision", "defect", ContactRevision{}, sdkgo.FailureValidation, "contact resource name is invalid")
	}
	query := url.Values{"personFields": {"names,emailAddresses,metadata"}}
	response := operation.client.request(call, "/v1/"+input.ResourceName+"?"+query.Encode())
	if response.status == http.StatusNotFound && response.err == nil {
		return queryBranch("getContactRevision", "notFound", ContactRevision{}, sdkgo.FailureProviderRejection, "contact was not found")
	}
	if decision, failed := classifyRead[ContactRevision](response, "getContactRevision"); failed {
		return decision
	}
	var person personWire
	if err := json.Unmarshal(response.body, &person); err != nil {
		return queryBranch("getContactRevision", "invalidResponse", ContactRevision{}, sdkgo.FailureProtocol, "Google returned invalid contact JSON")
	}
	contact, ok := person.project()
	if !ok || contact.ResourceName != input.ResourceName {
		return queryBranch("getContactRevision", "invalidResponse", ContactRevision{}, sdkgo.FailureProtocol, "Google returned the wrong contact resource")
	}
	for _, source := range person.Metadata.Sources {
		if source.Type == "CONTACT" && source.Etag != "" && len(source.Etag) <= 1024 && utf8.ValidString(source.Etag) {
			return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), ContactRevision{ResourceName: contact.ResourceName, DisplayName: contact.DisplayName, EmailAddress: contact.EmailAddress, Etag: source.Etag}, nil, sdkgo.Receipt{})
		}
	}
	return queryBranch("getContactRevision", "invalidResponse", ContactRevision{}, sdkgo.FailureProtocol, "Google returned no usable contact source etag")
}
