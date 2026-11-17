// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactTagsInput selects one page of tags on a contact.
type ListContactTagsInput struct {
	// ContactID is the contact ID in Keap CRM > Contacts.
	ContactID string `json:"contactId"`
	// PageSize is the maximum number of tags; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the opaque continuation token; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// ContactTagInput selects a contact and an existing tag.
type ContactTagInput struct {
	// ContactID is the contact ID in Keap CRM > Contacts.
	ContactID string `json:"contactId"`
	// TagID is the tag ID in Keap CRM > Settings > Tags.
	TagID string `json:"tagId"`
}

// ContactTag contains the identity and name of an applied tag.
type ContactTag struct {
	// ID is Keap's tag ID.
	ID string `json:"id"`
	// Name is the tag's display name.
	Name string `json:"name"`
}

// ContactTagsPage contains one page of applied tags.
type ContactTagsPage struct {
	// Tags are the applied tags.
	Tags []ContactTag `json:"tags"`
	// NextPageToken selects the next page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// ContactTagChange confirms a tag assignment or removal.
type ContactTagChange struct {
	// ContactID is the affected contact ID.
	ContactID string `json:"contactId"`
	// TagID is the affected tag ID.
	TagID string `json:"tagId"`
	// Changed reports Keap's acceptance of the requested state.
	Changed bool `json:"changed"`
}

// ListContactTagsOperation lists tags applied to a contact.
type ListContactTagsOperation struct{ client *Client }

// ApplyContactTagOperation applies one tag to a contact.
type ApplyContactTagOperation struct{ client *Client }

// RemoveContactTagOperation removes one tag from a contact.
type RemoveContactTagOperation struct{ client *Client }

// ListContactTags binds a tag list to the client.
func (client *Client) ListContactTags() ListContactTagsOperation {
	return ListContactTagsOperation{client}
}

// ApplyContactTag binds tag application to the client.
func (client *Client) ApplyContactTag() ApplyContactTagOperation {
	return ApplyContactTagOperation{client}
}

// RemoveContactTag binds tag removal to the client.
func (client *Client) RemoveContactTag() RemoveContactTagOperation {
	return RemoveContactTagOperation{client}
}

// Definition returns the generated query contract.
func (ListContactTagsOperation) Definition() sdkgo.QueryDefinition { return ListContactTagsDefinition }

// Definition returns the generated mutation contract.
func (ApplyContactTagOperation) Definition() sdkgo.MutationDefinition {
	return ApplyContactTagDefinition
}

// Definition returns the generated mutation contract.
func (RemoveContactTagOperation) Definition() sdkgo.MutationDefinition {
	return RemoveContactTagDefinition
}

// IdempotencyKey derives the stable Dex call identity.
func (ApplyContactTagOperation) IdempotencyKey(callID sdkgo.CallID, _ ContactTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (RemoveContactTagOperation) IdempotencyKey(callID sdkgo.CallID, _ ContactTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists a contact's applied tags.
func (operation ListContactTagsOperation) Invoke(call sdkgo.Call, input ListContactTagsInput) sdkgo.QueryAttempt[ContactTagsPage] {
	if !validContactID(input.ContactID) || input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listContactTags", ListContactTagsBranchDefect, ContactTagsPage{}, sdkgo.FailureValidation, "contact ID, page size, or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/contacts/"+input.ContactID+"/tags?"+query.Encode(), nil)
	if response.status == 404 {
		return keapQueryBranch("listContactTags", ListContactTagsBranchNotFound, ContactTagsPage{}, sdkgo.FailureNotFound, "Keap did not find the contact")
	}
	if branch, retry, handled := classifyKeapQuery(response, "listContactTags", ListContactTagsBranchProviderRejected, ListContactTagsBranchInvalidResponse, ListContactTagsBranchDefect, ContactTagsPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Tags []struct {
			Tag struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"tag"`
		} `json:"tags"`
		NextPageToken string `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Tags == nil {
		return keapQueryBranch("listContactTags", ListContactTagsBranchInvalidResponse, ContactTagsPage{}, sdkgo.FailureProtocol, "Keap returned invalid tag list JSON")
	}
	page := ContactTagsPage{Tags: make([]ContactTag, 0, len(wire.Tags)), NextPageToken: wire.NextPageToken}
	for _, item := range wire.Tags {
		if !validContactID(item.Tag.ID) {
			return keapQueryBranch("listContactTags", ListContactTagsBranchInvalidResponse, ContactTagsPage{}, sdkgo.FailureProtocol, "Keap returned invalid tag identity")
		}
		page.Tags = append(page.Tags, ContactTag{ID: item.Tag.ID, Name: item.Tag.Name})
	}
	return sdkgo.NewQueryBranch(ListContactTagsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke applies one existing tag to a contact.
func (operation ApplyContactTagOperation) Invoke(call sdkgo.Call, input ContactTagInput) sdkgo.MutationAttempt[ContactTagChange] {
	if !validContactID(input.ContactID) || !validContactID(input.TagID) {
		return keapMutationBranch("applyContactTag", ApplyContactTagBranchDefect, ContactTagChange{}, sdkgo.FailureValidation, "contact and tag IDs are required")
	}
	return operation.client.changeContactTag(call, input, "applyContactTag", ":applyTags", ApplyContactTagBranchApplied, ApplyContactTagBranchNotFound, ApplyContactTagBranchProviderRejected, ApplyContactTagBranchInvalidResponse, ApplyContactTagBranchDefect)
}

// Invoke removes one existing tag from a contact.
func (operation RemoveContactTagOperation) Invoke(call sdkgo.Call, input ContactTagInput) sdkgo.MutationAttempt[ContactTagChange] {
	if !validContactID(input.ContactID) || !validContactID(input.TagID) {
		return keapMutationBranch("removeContactTag", RemoveContactTagBranchDefect, ContactTagChange{}, sdkgo.FailureValidation, "contact and tag IDs are required")
	}
	return operation.client.changeContactTag(call, input, "removeContactTag", ":removeTags", RemoveContactTagBranchRemoved, RemoveContactTagBranchNotFound, RemoveContactTagBranchProviderRejected, RemoveContactTagBranchInvalidResponse, RemoveContactTagBranchDefect)
}

func (client *Client) changeContactTag(call sdkgo.Call, input ContactTagInput, operationName, action string, accepted, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[ContactTagChange] {
	body, _ := json.Marshal(map[string][]string{"contact_ids": {input.ContactID}})
	response := client.request(call, http.MethodPost, "/rest/v2/tags/"+input.TagID+"/contacts"+action, body)
	if response.status == 404 {
		return keapMutationBranch(operationName, notFound, ContactTagChange{}, sdkgo.FailureNotFound, "Keap did not find the contact or tag")
	}
	if branch, retry, handled := classifyKeapMutation(response, operationName, rejected, invalid, defect, ContactTagChange{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if action == ":applyTags" {
		if response.status != 200 {
			return keapMutationBranch(operationName, rejected, ContactTagChange{}, sdkgo.FailureProviderRejection, "Keap rejected tag application")
		}
		var wire struct {
			Results map[string]string `json:"results"`
		}
		if json.Unmarshal(response.body, &wire) != nil || wire.Results == nil || wire.Results[input.TagID] == "" {
			return keapMutationBranch(operationName, invalid, ContactTagChange{}, sdkgo.FailureProtocol, "Keap returned invalid tag application JSON")
		}
		if !strings.EqualFold(wire.Results[input.TagID], "Successfully applied tag") {
			return keapMutationBranch(operationName, rejected, ContactTagChange{}, sdkgo.FailureProviderRejection, "Keap did not confirm tag application")
		}
	} else {
		if response.status != 204 && response.status != 200 {
			return keapMutationBranch(operationName, rejected, ContactTagChange{}, sdkgo.FailureProviderRejection, "Keap rejected tag removal")
		}
		if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
			return keapMutationBranch(operationName, invalid, ContactTagChange{}, sdkgo.FailureProtocol, "Keap returned invalid tag removal JSON")
		}
	}
	return sdkgo.NewMutationBranch(accepted, ContactTagChange{ContactID: input.ContactID, TagID: input.TagID, Changed: true}, nil, sdkgo.Receipt{})
}
