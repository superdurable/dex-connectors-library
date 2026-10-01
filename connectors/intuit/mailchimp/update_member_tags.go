// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateMemberTagsOperation = "updateMemberTags"

	// MaxMemberTagChanges bounds the tags one updateMemberTags adds and removes together.
	MaxMemberTagChanges = 50
	// MaxMemberTagNameBytes bounds one tag name.
	MaxMemberTagNameBytes = 100

	memberTagActive   = "active"
	memberTagInactive = "inactive"
)

// UpdateMemberTagsInput adds and removes tags on one existing audience contact.
type UpdateMemberTagsInput struct {
	// ListID is the audience ID, Mailchimp's list_id.
	ListID string `json:"listId"`
	// EmailAddress is the contact's bare email address.
	EmailAddress string `json:"emailAddress"`
	// AddTags are declared active. Mailchimp creates a tag that does not exist yet, and a tag the
	// contact already has stays as it is.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags are declared inactive; a tag the contact does not have is ignored.
	RemoveTags []string `json:"removeTags,omitempty"`
	// ShouldSuppressAutomations sends is_syncing=true, so Mailchimp does not start automations
	// triggered by these tag changes.
	ShouldSuppressAutomations bool `json:"shouldSuppressAutomations,omitempty"`
}

// UpdateMemberTagsOutput reports the tag declarations Mailchimp accepted.
type UpdateMemberTagsOutput struct {
	// SubscriberHash is the contact's MD5 subscriber hash.
	SubscriberHash string `json:"subscriberHash"`
	// ActiveTags are the tags declared active.
	ActiveTags []string `json:"activeTags,omitempty"`
	// InactiveTags are the tags declared inactive.
	InactiveTags []string `json:"inactiveTags,omitempty"`
}

// UpdateMemberTagsOperation is the updateMemberTags Mutation.
type UpdateMemberTagsOperation struct {
	client *Client
}

type memberTagsRequestWire struct {
	Tags      []memberTagWire `json:"tags"`
	IsSyncing bool            `json:"is_syncing,omitempty"`
}

type memberTagWire struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Definition returns the immutable connector operation definition.
func (UpdateMemberTagsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateMemberTagsDefinition
}

// IdempotencyKey uses the stable connector Call ID. Mailchimp documents no idempotency key; each tag
// is declared active or inactive, so a repeated request leaves the same tags and the key only
// correlates the Receipt.
func (UpdateMemberTagsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateMemberTagsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /lists/{list_id}/members/{subscriber_hash}/tags, which Mailchimp answers with
// 204. Every unconfirmed outcome is retried, because a repeated declaration leaves the same tags.
func (operation UpdateMemberTagsOperation) Invoke(call sdkgo.Call, input UpdateMemberTagsInput) sdkgo.MutationAttempt[UpdateMemberTagsOutput] {
	if err := validateUpdateMemberTagsInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateMemberTagsBranchDefect, UpdateMemberTagsOutput{}, mailchimpFailurePointer(sdkgo.FailureValidation, updateMemberTagsOperation, err.Error()), sdkgo.Receipt{})
	}
	connection, failure := operation.client.resolveConnection(call, updateMemberTagsOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateMemberTagsBranchDefect, UpdateMemberTagsOutput{}, failure, sdkgo.Receipt{})
	}
	subscriberHash := SubscriberHash(input.EmailAddress)
	result := operation.client.exchange(call, connection, updateMemberTagsOperation, mailchimpRequest{
		method: http.MethodPost, path: memberPath(input.ListID, subscriberHash) + "/tags", payload: buildMemberTagsRequest(input),
	})
	receipt := operation.client.receipt(call, result.response, subscriberHash)
	if attempt, isTerminal := repeatableMutationAttemptForExchange[UpdateMemberTagsOutput](result, receipt, repeatableBranches{
		notFound: UpdateMemberTagsBranchNotFound, providerRejected: UpdateMemberTagsBranchProviderRejected,
		invalidResponse: UpdateMemberTagsBranchInvalidResponse, defect: UpdateMemberTagsBranchDefect,
	}); isTerminal {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdateMemberTagsBranchUpdated, UpdateMemberTagsOutput{
		SubscriberHash: subscriberHash, ActiveTags: input.AddTags, InactiveTags: input.RemoveTags,
	}, nil, receipt)
}

func validateUpdateMemberTagsInput(input UpdateMemberTagsInput) error {
	if err := validateMemberAddress(input.ListID, input.EmailAddress); err != nil {
		return err
	}
	if len(input.AddTags)+len(input.RemoveTags) == 0 {
		return errors.New("at least one of addTags or removeTags is required")
	}
	if len(input.AddTags)+len(input.RemoveTags) > MaxMemberTagChanges {
		return fmt.Errorf("addTags and removeTags hold at most %d tags together", MaxMemberTagChanges)
	}
	seen := map[string]bool{}
	for _, tag := range append(append([]string(nil), input.AddTags...), input.RemoveTags...) {
		if err := validateMemberTagName(tag); err != nil {
			return err
		}
		folded := strings.ToLower(tag)
		if seen[folded] {
			return fmt.Errorf("tag %q appears more than once across addTags and removeTags", tag)
		}
		seen[folded] = true
	}
	return nil
}

func validateMemberTagName(tag string) error {
	if tag == "" || len(tag) > MaxMemberTagNameBytes || !utf8.ValidString(tag) || strings.TrimSpace(tag) != tag {
		return fmt.Errorf("tag %q must be 1 to %d bytes of UTF-8 without surrounding spaces", tag, MaxMemberTagNameBytes)
	}
	for _, character := range tag {
		if unicode.IsControl(character) {
			return fmt.Errorf("tag %q cannot contain control characters", tag)
		}
	}
	return nil
}

func buildMemberTagsRequest(input UpdateMemberTagsInput) memberTagsRequestWire {
	request := memberTagsRequestWire{IsSyncing: input.ShouldSuppressAutomations}
	for _, tag := range input.AddTags {
		request.Tags = append(request.Tags, memberTagWire{Name: tag, Status: memberTagActive})
	}
	for _, tag := range input.RemoveTags {
		request.Tags = append(request.Tags, memberTagWire{Name: tag, Status: memberTagInactive})
	}
	return request
}
