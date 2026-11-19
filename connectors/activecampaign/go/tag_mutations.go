// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTagInput creates a contact tag definition identified by its name.
type CreateTagInput struct {
	// Name is a 1–250-character contact tag name without commas, line breaks, markup, or emoji.
	Name string `json:"name"`
	// Description is optional text shown with the tag.
	Description string `json:"description,omitempty"`
}

// UpdateTagInput replaces a contact tag's name and description by ID.
type UpdateTagInput struct {
	// TagID is the numeric provider tag ID.
	TagID string `json:"tagId"`
	// Name is the complete replacement tag name.
	Name string `json:"name"`
	// Description is the complete replacement description; blank clears it.
	Description string `json:"description"`
}

// DeleteTagInput selects a tag definition for removal from the account and all contacts.
type DeleteTagInput struct {
	// TagID is the numeric provider tag ID.
	TagID string `json:"tagId"`
}

// TagDeletion confirms the target tag is absent; deletion affects all tagged contacts.
type TagDeletion struct {
	// TagID is the tag removed or already absent.
	TagID string `json:"tagId"`
	// Deleted is true when the tag is absent after the call.
	Deleted bool `json:"deleted"`
}

// CreateTagOperation creates a tag definition by its provider-unique name.
type CreateTagOperation struct{ client *Client }

// UpdateTagOperation replaces a tag definition by ID.
type UpdateTagOperation struct{ client *Client }

// DeleteTagOperation deletes a tag definition by ID.
type DeleteTagOperation struct{ client *Client }

// CreateTag binds the create Mutation to this client.
func (client *Client) CreateTag() CreateTagOperation { return CreateTagOperation{client} }

// UpdateTag binds the update Mutation to this client.
func (client *Client) UpdateTag() UpdateTagOperation { return UpdateTagOperation{client} }

// DeleteTag binds the delete Mutation to this client.
func (client *Client) DeleteTag() DeleteTagOperation { return DeleteTagOperation{client} }

// Definition returns the manifest-generated create contract.
func (CreateTagOperation) Definition() sdkgo.MutationDefinition { return CreateTagDefinition }

// Definition returns the manifest-generated update contract.
func (UpdateTagOperation) Definition() sdkgo.MutationDefinition { return UpdateTagDefinition }

// Definition returns the manifest-generated delete contract.
func (DeleteTagOperation) Definition() sdkgo.MutationDefinition { return DeleteTagDefinition }

// IdempotencyKey correlates bounded sends locally; ActiveCampaign does not receive this key.
func (CreateTagOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable call identity; absolute field replacement is safe to repeat.
func (UpdateTagOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable call identity; deleting an absent ID is a no-op.
func (DeleteTagOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a tag with bounded sends and reports unconfirmed outcomes.
func (operation CreateTagOperation) Invoke(call sdkgo.Call, input CreateTagInput) sdkgo.MutationAttempt[TagMetadata] {
	if !validTagName(input.Name) {
		return tagMutationBranch("createTag", CreateTagBranchDefect, sdkgo.FailureValidation, "tag name is invalid")
	}
	encoded, err := json.Marshal(map[string]any{"tag": map[string]string{"tag": input.Name, "tagType": "contact", "description": input.Description}})
	if err != nil {
		return tagMutationBranch("createTag", CreateTagBranchDefect, sdkgo.FailureLocalDefect, "tag request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateTagDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return tagMutationBranch("createTag", CreateTagBranchDefect, sdkgo.FailureLocalDefect, "tag create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag create checkpoint is unavailable"), 0)
	}
	if decision.Marker.ProviderID != "" {
		body, status, _, readErr := operation.client.get(call, "/tags/"+decision.Marker.ProviderID)
		if readErr == nil && status == http.StatusOK {
			tag, decodeErr := decodeTag(body)
			if decodeErr == nil && tag.ID == decision.Marker.ProviderID && tag.Name == input.Name && tag.Description == input.Description {
				return sdkgo.NewMutationBranch(CreateTagBranchCreated, tag, nil, dealReceipt(call, tag.ID))
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createTagUncertain(call, "tag create could not be confirmed by its provider ID")
		}
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag create confirmation requires read-back"), 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createTagUncertain(call, "an earlier tag create may have succeeded and reached the send cap")
	}
	dispatch := &createTagDispatch{client: operation.client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag create checkpoint is unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return limitedActiveCampaignUnknown[TagMetadata](call, decision, queryFailure("createTag", sdkgo.FailureAuthentication, "tag create remains unconfirmed after credentials became unavailable"), dealReceipt(call, ""))
		}
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag retry checkpoint is unavailable"), 0)
		}
		return tagMutationBranch("createTag", CreateTagBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(dispatch.err, errRequest) {
		return limitedActiveCampaignUnknown[TagMetadata](call, decision, queryFailure("createTag", sdkgo.FailureTransport, "tag create outcome is unknown"), dealReceipt(call, ""))
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag retry checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureRateLimit, "ActiveCampaign temporarily rejected the tag create"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusOK && dispatch.status != http.StatusCreated {
		if decision.MaximumPriorSends == 0 && dispatch.err == nil && isDefiniteActiveCampaignRefusal(dispatch.status, dispatch.response) {
			if err := fence.RecordCanResend(); err != nil {
				return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag refusal checkpoint is unavailable"), 0)
			}
			return tagMutationBranch("createTag", CreateTagBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
		}
		return limitedActiveCampaignUnknown[TagMetadata](call, decision, queryFailure("createTag", sdkgo.FailureTransport, "tag create response did not prove refusal"), dealReceipt(call, ""))
	}
	if dispatch.err != nil {
		return limitedActiveCampaignUnknown[TagMetadata](call, decision, queryFailure("createTag", sdkgo.FailureProtocol, "tag create response was incomplete"), dealReceipt(call, ""))
	}
	tag, err := decodeTag(dispatch.response)
	if err != nil {
		return limitedActiveCampaignUnknown[TagMetadata](call, decision, queryFailure("createTag", sdkgo.FailureProtocol, "tag create confirmation was invalid"), dealReceipt(call, ""))
	}
	if decision.MaximumPriorSends > 0 && dispatch.status == http.StatusOK {
		return limitedActiveCampaignUnknown[TagMetadata](call, decision, queryFailure("createTag", sdkgo.FailureProtocol, "an existing same-name tag cannot confirm an earlier create"), dealReceipt(call, ""))
	}
	if err := fence.RecordProviderID(tag.ID); err != nil {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure("createTag", sdkgo.FailureAvailability, "tag confirmation checkpoint is unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateTagBranchCreated, tag, nil, dealReceipt(call, tag.ID))
}

type createTagDispatch struct {
	client     *Client
	call       sdkgo.Call
	body       []byte
	response   []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (dispatch *createTagDispatch) execute(ctx context.Context) error {
	dispatch.response, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/tags", dispatch.body)
	return nil
}

func createTagUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[TagMetadata] {
	return sdkgo.NewMutationUncertain(TagMetadata{}, queryFailure("createTag", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}

// Invoke sets the tag's absolute name and description on every attempt.
func (operation UpdateTagOperation) Invoke(call sdkgo.Call, input UpdateTagInput) sdkgo.MutationAttempt[TagMetadata] {
	if !numericID.MatchString(input.TagID) || !validTagName(input.Name) {
		return tagMutationBranch("updateTag", UpdateTagBranchDefect, sdkgo.FailureValidation, "tag ID or name is invalid")
	}
	return invokeTagMutation(call, operation.client, "updateTag", http.MethodPut, "/tags/"+input.TagID, input.Name, input.Description, UpdateTagBranchUpdated, UpdateTagBranchNotFound, UpdateTagBranchProviderRejected, UpdateTagBranchInvalidResponse, UpdateTagBranchDefect)
}

// Invoke removes one tag definition; repeating a completed delete converges on absence.
func (operation DeleteTagOperation) Invoke(call sdkgo.Call, input DeleteTagInput) sdkgo.MutationAttempt[TagDeletion] {
	if !numericID.MatchString(input.TagID) {
		return tagDeletionBranch("deleteTag", DeleteTagBranchDefect, sdkgo.FailureValidation, "tag ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.mutate(call, http.MethodDelete, "/tags/"+input.TagID, nil)
	if errors.Is(err, errCredential) {
		return tagDeletionBranch("deleteTag", DeleteTagBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[TagDeletion](queryFailure("deleteTag", sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[TagDeletion](queryFailure("deleteTag", sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[TagDeletion](queryFailure("deleteTag", sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return tagDeletionBranch("deleteTag", DeleteTagBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return tagDeletionBranch("deleteTag", DeleteTagBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[TagDeletion](queryFailure("deleteTag", sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	if status == http.StatusOK && !json.Valid(body) {
		return tagDeletionBranch("deleteTag", DeleteTagBranchInvalidResponse, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(DeleteTagBranchDeleted, TagDeletion{TagID: input.TagID, Deleted: true}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "activecampaign", ProviderObjectID: input.TagID, ObservedAt: time.Now().UTC()})
}

func validTagName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 250 || strings.ContainsAny(name, ",\r\n<>") {
		return false
	}
	for _, character := range name {
		if utf8.RuneLen(character) > 3 || character == utf8.RuneError {
			return false
		}
	}
	return true
}

func invokeTagMutation(call sdkgo.Call, client *Client, operation, method, path, name, description string, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[TagMetadata] {
	encoded, err := json.Marshal(map[string]any{"tag": map[string]string{"tag": name, "tagType": "contact", "description": description}})
	if err != nil {
		return tagMutationBranch(operation, defect, sdkgo.FailureLocalDefect, "tag request could not be encoded")
	}
	body, status, retryAfter, err := client.mutate(call, method, path, encoded)
	if errors.Is(err, errCredential) {
		return tagMutationBranch(operation, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure(operation, sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure(operation, sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure(operation, sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound && notFound != "" {
		return tagMutationBranch(operation, notFound, sdkgo.FailureNotFound, "ActiveCampaign did not find the tag")
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return tagMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return tagMutationBranch(operation, invalid, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[TagMetadata](queryFailure(operation, sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	tag, err := decodeTag(body)
	if err != nil {
		return tagMutationBranch(operation, invalid, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(success, tag, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "activecampaign", ProviderObjectID: tag.ID, ObservedAt: time.Now().UTC()})
}

func tagMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TagMetadata] {
	failure := queryFailure(operation, kind, message)
	if branch == "invalidResponse" {
		return sdkgo.NewMutationRetry[TagMetadata](failure, 0)
	}
	return sdkgo.NewMutationBranch(branch, TagMetadata{}, &failure, sdkgo.Receipt{})
}

func tagDeletionBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TagDeletion] {
	failure := queryFailure(operation, kind, message)
	if branch == "invalidResponse" {
		return sdkgo.NewMutationRetry[TagDeletion](failure, 0)
	}
	return sdkgo.NewMutationBranch(branch, TagDeletion{}, &failure, sdkgo.Receipt{})
}
