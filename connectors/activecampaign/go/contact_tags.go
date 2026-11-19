// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListContactTagsInput selects one contact's tag associations.
type ListContactTagsInput struct {
	// ContactID is the numeric provider contact ID.
	ContactID string `json:"contactId"`
}

// ApplyTagToContactInput applies an existing tag to one contact.
type ApplyTagToContactInput struct {
	// ContactID is the numeric provider contact ID.
	ContactID string `json:"contactId"`
	// TagID is the numeric ID of an existing tag definition.
	TagID string `json:"tagId"`
}

// RemoveTagFromContactInput removes one tag from one contact.
type RemoveTagFromContactInput struct {
	// ContactID is the numeric provider contact ID.
	ContactID string `json:"contactId"`
	// TagID is the numeric provider tag ID.
	TagID string `json:"tagId"`
}

// ContactTagAssociation identifies one contact-to-tag relationship.
type ContactTagAssociation struct {
	// AssociationID is the provider relationship ID.
	AssociationID string `json:"associationId"`
	// ContactID is the owning contact ID.
	ContactID string `json:"contactId"`
	// TagID is the applied tag definition ID.
	TagID string `json:"tagId"`
}

// ContactTagAssociations lists one contact's applied tag identities.
type ContactTagAssociations struct {
	// ContactID is the selected contact ID.
	ContactID string `json:"contactId"`
	// Tags contains the tag associations visible on the contact.
	Tags []ContactTagAssociation `json:"tags"`
}

// ContactTagRemoval confirms that a tag is absent from one contact.
type ContactTagRemoval struct {
	// ContactID is the selected contact ID.
	ContactID string `json:"contactId"`
	// TagID is the selected tag definition ID.
	TagID string `json:"tagId"`
	// Removed is true when the relationship is absent after the call.
	Removed bool `json:"removed"`
}

// ListContactTagsOperation reads one contact's tag relationships.
type ListContactTagsOperation struct{ client *Client }

// ApplyTagToContactOperation applies an existing tag by ID.
type ApplyTagToContactOperation struct{ client *Client }

// RemoveTagFromContactOperation removes one contact's tag by ID.
type RemoveTagFromContactOperation struct{ client *Client }

// ListContactTags binds the list Query to this client.
func (client *Client) ListContactTags() ListContactTagsOperation {
	return ListContactTagsOperation{client}
}

// ApplyTagToContact binds the apply Mutation to this client.
func (client *Client) ApplyTagToContact() ApplyTagToContactOperation {
	return ApplyTagToContactOperation{client}
}

// RemoveTagFromContact binds the remove Mutation to this client.
func (client *Client) RemoveTagFromContact() RemoveTagFromContactOperation {
	return RemoveTagFromContactOperation{client}
}

// Definition returns the manifest-generated list contract.
func (ListContactTagsOperation) Definition() sdkgo.QueryDefinition { return ListContactTagsDefinition }

// Definition returns the manifest-generated apply contract.
func (ApplyTagToContactOperation) Definition() sdkgo.MutationDefinition {
	return ApplyTagToContactDefinition
}

// Definition returns the manifest-generated remove contract.
func (RemoveTagFromContactOperation) Definition() sdkgo.MutationDefinition {
	return RemoveTagFromContactDefinition
}

// IdempotencyKey derives a stable call identity; applying an existing tag twice returns the same association.
func (ApplyTagToContactOperation) IdempotencyKey(callID sdkgo.CallID, _ ApplyTagToContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity; removing an absent relationship is a no-op.
func (RemoveTagFromContactOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveTagFromContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads every tag association for one contact.
func (operation ListContactTagsOperation) Invoke(call sdkgo.Call, input ListContactTagsInput) sdkgo.QueryAttempt[ContactTagAssociations] {
	branches := queryBranches{found: ListContactTagsBranchListed, notFound: ListContactTagsBranchNotFound, rejected: ListContactTagsBranchProviderRejected, invalid: ListContactTagsBranchInvalidResponse, defect: ListContactTagsBranchDefect}
	if !numericID.MatchString(input.ContactID) {
		return queryBranch("listContactTags", branches.defect, ContactTagAssociations{}, sdkgo.FailureValidation, "contact ID must be numeric")
	}
	return invokeQuery(call, operation.client, "listContactTags", "/contacts/"+input.ContactID+"/contactTags", branches, func(body []byte) (ContactTagAssociations, error) { return decodeContactTags(body, input.ContactID) })
}

// Invoke applies an existing tag without creating a tag definition.
func (operation ApplyTagToContactOperation) Invoke(call sdkgo.Call, input ApplyTagToContactInput) sdkgo.MutationAttempt[ContactTagAssociation] {
	if !numericID.MatchString(input.ContactID) || !numericID.MatchString(input.TagID) {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchDefect, sdkgo.FailureValidation, "contact or tag ID must be numeric")
	}
	encoded, err := json.Marshal(map[string]any{"tagIds": []string{input.TagID}, "createMissing": false})
	if err != nil {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchDefect, sdkgo.FailureLocalDefect, "tag assignment could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPost, "/contacts/"+input.ContactID+"/tags", encoded)
	if errors.Is(err, errCredential) {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ContactTagAssociation](queryFailure("applyTagToContact", sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ContactTagAssociation](queryFailure("applyTagToContact", sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[ContactTagAssociation](queryFailure("applyTagToContact", sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchNotFound, sdkgo.FailureNotFound, "ActiveCampaign did not find the contact")
	}
	if status != http.StatusCreated {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ContactTagAssociation](queryFailure("applyTagToContact", sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	associations, err := decodeContactTags(body, input.ContactID)
	if err != nil || len(associations.Tags) != 1 || associations.Tags[0].TagID != input.TagID {
		return contactTagMutationBranch("applyTagToContact", ApplyTagToContactBranchInvalidResponse, sdkgo.FailureProtocol, "ActiveCampaign returned invalid tag associations")
	}
	association := associations.Tags[0]
	return sdkgo.NewMutationBranch(ApplyTagToContactBranchApplied, association, nil, dealReceipt(call, association.AssociationID))
}

// Invoke removes one tag; an already-absent relationship converges on removal.
func (operation RemoveTagFromContactOperation) Invoke(call sdkgo.Call, input RemoveTagFromContactInput) sdkgo.MutationAttempt[ContactTagRemoval] {
	if !numericID.MatchString(input.ContactID) || !numericID.MatchString(input.TagID) {
		return contactTagRemovalBranch("removeTagFromContact", RemoveTagFromContactBranchDefect, sdkgo.FailureValidation, "contact or tag ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, "/contacts/"+input.ContactID+"/tags/"+input.TagID, nil)
	if errors.Is(err, errCredential) {
		return contactTagRemovalBranch("removeTagFromContact", RemoveTagFromContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ContactTagRemoval](queryFailure("removeTagFromContact", sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ContactTagRemoval](queryFailure("removeTagFromContact", sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[ContactTagRemoval](queryFailure("removeTagFromContact", sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return contactTagRemovalBranch("removeTagFromContact", RemoveTagFromContactBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return contactTagRemovalBranch("removeTagFromContact", RemoveTagFromContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ContactTagRemoval](queryFailure("removeTagFromContact", sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	if status == http.StatusOK && len(body) > 0 && !json.Valid(body) {
		return contactTagRemovalBranch("removeTagFromContact", RemoveTagFromContactBranchInvalidResponse, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(RemoveTagFromContactBranchRemoved, ContactTagRemoval{ContactID: input.ContactID, TagID: input.TagID, Removed: true}, nil, dealReceipt(call, input.ContactID+":"+input.TagID))
}

type contactTagWire struct {
	ID      string `json:"id"`
	Contact string `json:"contact"`
	Tag     string `json:"tag"`
}

func decodeContactTags(body []byte, contactID string) (ContactTagAssociations, error) {
	var wire struct {
		ContactTags []contactTagWire `json:"contactTags"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactTagAssociations{}, err
	}
	if wire.ContactTags == nil {
		return ContactTagAssociations{}, fmt.Errorf("tag associations missing")
	}
	result := ContactTagAssociations{ContactID: contactID, Tags: make([]ContactTagAssociation, 0, len(wire.ContactTags))}
	for _, item := range wire.ContactTags {
		if !numericID.MatchString(item.ID) || item.Contact != contactID || !numericID.MatchString(item.Tag) {
			return ContactTagAssociations{}, fmt.Errorf("tag association identity invalid")
		}
		result.Tags = append(result.Tags, ContactTagAssociation{AssociationID: item.ID, ContactID: item.Contact, TagID: item.Tag})
	}
	return result, nil
}

func contactTagMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactTagAssociation] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ContactTagAssociation{}, &failure, sdkgo.Receipt{})
}

func contactTagRemovalBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactTagRemoval] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ContactTagRemoval{}, &failure, sdkgo.Receipt{})
}
