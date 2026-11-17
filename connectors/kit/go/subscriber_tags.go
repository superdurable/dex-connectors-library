// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpsertSubscriberInput identifies a subscriber by email and sets its first name.
type UpsertSubscriberInput struct {
	// EmailAddress is the subscriber's email identity.
	EmailAddress string `json:"emailAddress"`
	// FirstName is the subscriber's replacement first name.
	FirstName string `json:"firstName"`
}

// ListTagsInput selects a cursor page of tags.
type ListTagsInput struct {
	// PerPage bounds the page size; zero uses 20.
	PerPage int `json:"perPage,omitempty"`
	// After is the prior page's opaque end cursor.
	After string `json:"after,omitempty"`
}

// TagMetadata is a Kit tag's ID and non-secret name.
type TagMetadata struct {
	// ID is the stable numeric tag ID.
	ID int64 `json:"id"`
	// Name is the tag's display name.
	Name string `json:"name"`
}

// TagsPage is one cursor page of tag metadata.
type TagsPage struct {
	// Tags are the tags returned by Kit.
	Tags []TagMetadata `json:"tags"`
	// NextCursor is nonempty when another page exists.
	NextCursor string `json:"nextCursor"`
}

// CreateTagInput names the tag to create or reuse.
type CreateTagInput struct {
	// Name is the nonempty case-insensitive tag identity.
	Name string `json:"name"`
}

// TagSubscriberInput identifies an existing tag and subscriber.
type TagSubscriberInput struct {
	// TagID is the numeric tag ID from Kit.
	TagID int64 `json:"tagId"`
	// SubscriberID is the numeric subscriber ID from Kit.
	SubscriberID int64 `json:"subscriberId"`
}

// TagRemoval confirms removal of one tag association.
type TagRemoval struct {
	// TagID is the affected tag ID.
	TagID int64 `json:"tagId"`
	// SubscriberID is the affected subscriber ID.
	SubscriberID int64 `json:"subscriberId"`
	// Removed reports provider acceptance.
	Removed bool `json:"removed"`
}

// UpsertSubscriberOperation creates or updates a subscriber by email.
type UpsertSubscriberOperation struct{ client *Client }

// ListTagsOperation lists tag metadata.
type ListTagsOperation struct{ client *Client }

// CreateTagOperation creates or reuses a tag by name.
type CreateTagOperation struct{ client *Client }

// TagSubscriberOperation attaches an existing tag to a subscriber.
type TagSubscriberOperation struct{ client *Client }

// RemoveTagFromSubscriberOperation removes a tag association.
type RemoveTagFromSubscriberOperation struct{ client *Client }

// UpsertSubscriber binds the mutation to this client.
func (client *Client) UpsertSubscriber() UpsertSubscriberOperation {
	return UpsertSubscriberOperation{client}
}

// ListTags binds the query to this client.
func (client *Client) ListTags() ListTagsOperation { return ListTagsOperation{client} }

// CreateTag binds the mutation to this client.
func (client *Client) CreateTag() CreateTagOperation { return CreateTagOperation{client} }

// TagSubscriber binds the mutation to this client.
func (client *Client) TagSubscriber() TagSubscriberOperation { return TagSubscriberOperation{client} }

// RemoveTagFromSubscriber binds the mutation to this client.
func (client *Client) RemoveTagFromSubscriber() RemoveTagFromSubscriberOperation {
	return RemoveTagFromSubscriberOperation{client}
}

// Definition returns the generated mutation contract.
func (UpsertSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return UpsertSubscriberDefinition
}

// Definition returns the generated query contract.
func (ListTagsOperation) Definition() sdkgo.QueryDefinition { return ListTagsDefinition }

// Definition returns the generated mutation contract.
func (CreateTagOperation) Definition() sdkgo.MutationDefinition { return CreateTagDefinition }

// Definition returns the generated mutation contract.
func (TagSubscriberOperation) Definition() sdkgo.MutationDefinition { return TagSubscriberDefinition }

// Definition returns the generated mutation contract.
func (RemoveTagFromSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return RemoveTagFromSubscriberDefinition
}

// IdempotencyKey derives a stable call identity across retries.
func (UpsertSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity across retries.
func (CreateTagOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity across retries.
func (TagSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ TagSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity across retries.
func (RemoveTagFromSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ TagSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or updates a subscriber through Kit's email upsert endpoint.
func (operation UpsertSubscriberOperation) Invoke(call sdkgo.Call, input UpsertSubscriberInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	address, err := mail.ParseAddress(input.EmailAddress)
	if err != nil || address.Address != input.EmailAddress || len(input.EmailAddress) > 320 || len(input.FirstName) > 255 {
		return kitMutationBranch("upsertSubscriber", UpsertSubscriberBranchDefect, SubscriberMetadata{}, sdkgo.FailureValidation, "valid email and bounded first name are required")
	}
	body, err := json.Marshal(map[string]string{"email_address": input.EmailAddress, "first_name": input.FirstName})
	if err != nil {
		return kitMutationBranch("upsertSubscriber", UpsertSubscriberBranchDefect, SubscriberMetadata{}, sdkgo.FailureValidation, "subscriber input cannot be encoded")
	}
	return invokeKitMutation(call, operation.client, "upsertSubscriber", http.MethodPost, "/subscribers", body,
		mutationBranches{success: UpsertSubscriberBranchUpserted, rejected: UpsertSubscriberBranchProviderRejected, invalid: UpsertSubscriberBranchInvalidResponse, defect: UpsertSubscriberBranchDefect},
		func(body []byte) (SubscriberMetadata, error) { return decodeSubscriber(body) })
}

// Invoke reads one tag page without subscriber counts or other metadata.
func (operation ListTagsOperation) Invoke(call sdkgo.Call, input ListTagsInput) sdkgo.QueryAttempt[TagsPage] {
	branches := queryBranches{found: ListTagsBranchListed, rejected: ListTagsBranchProviderRejected, invalid: ListTagsBranchInvalidResponse, defect: ListTagsBranchDefect}
	if input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 {
		return queryBranch("listTags", branches.defect, TagsPage{}, sdkgo.FailureValidation, "cursor or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return invokeQuery(call, operation.client, "listTags", "/tags?"+query.Encode(), branches, decodeTagsPage)
}

// Invoke creates or reuses a tag under Kit's case-insensitive name identity.
func (operation CreateTagOperation) Invoke(call sdkgo.Call, input CreateTagInput) sdkgo.MutationAttempt[TagMetadata] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return kitMutationBranch("createTag", CreateTagBranchDefect, TagMetadata{}, sdkgo.FailureValidation, "bounded tag name is required")
	}
	body, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return kitMutationBranch("createTag", CreateTagBranchDefect, TagMetadata{}, sdkgo.FailureValidation, "tag name cannot be encoded")
	}
	return invokeKitMutation(call, operation.client, "createTag", http.MethodPost, "/tags", body,
		mutationBranches{success: CreateTagBranchCreated, rejected: CreateTagBranchProviderRejected, invalid: CreateTagBranchInvalidResponse, defect: CreateTagBranchDefect},
		decodeTag)
}

// Invoke tags a subscriber; Kit returns an existing association on repeats.
func (operation TagSubscriberOperation) Invoke(call sdkgo.Call, input TagSubscriberInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	if input.TagID <= 0 || input.SubscriberID <= 0 {
		return kitMutationBranch("tagSubscriber", TagSubscriberBranchDefect, SubscriberMetadata{}, sdkgo.FailureValidation, "positive tag and subscriber IDs are required")
	}
	path := "/tags/" + strconv.FormatInt(input.TagID, 10) + "/subscribers/" + strconv.FormatInt(input.SubscriberID, 10)
	return invokeKitMutation(call, operation.client, "tagSubscriber", http.MethodPost, path, nil,
		mutationBranches{success: TagSubscriberBranchTagged, notFound: TagSubscriberBranchNotFound, rejected: TagSubscriberBranchProviderRejected, invalid: TagSubscriberBranchInvalidResponse, defect: TagSubscriberBranchDefect},
		decodeSubscriber)
}

// Invoke removes one tag association; repeating the removal has no further effect.
func (operation RemoveTagFromSubscriberOperation) Invoke(call sdkgo.Call, input TagSubscriberInput) sdkgo.MutationAttempt[TagRemoval] {
	if input.TagID <= 0 || input.SubscriberID <= 0 {
		return kitMutationBranch("removeTagFromSubscriber", RemoveTagFromSubscriberBranchDefect, TagRemoval{}, sdkgo.FailureValidation, "positive tag and subscriber IDs are required")
	}
	path := "/tags/" + strconv.FormatInt(input.TagID, 10) + "/subscribers/" + strconv.FormatInt(input.SubscriberID, 10)
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, path, nil)
	branches := mutationBranches{success: RemoveTagFromSubscriberBranchRemoved, notFound: RemoveTagFromSubscriberBranchNotFound, rejected: RemoveTagFromSubscriberBranchProviderRejected, invalid: RemoveTagFromSubscriberBranchInvalidResponse, defect: RemoveTagFromSubscriberBranchDefect}
	if outcome, handled := classifyKitMutation[TagRemoval](body, status, retryAfter, err, "removeTagFromSubscriber", branches); handled {
		return outcome
	}
	if status != http.StatusNoContent {
		return kitMutationBranch("removeTagFromSubscriber", branches.invalid, TagRemoval{}, sdkgo.FailureProtocol, "Kit returned unexpected tag-removal response")
	}
	return sdkgo.NewMutationBranch(branches.success, TagRemoval{TagID: input.TagID, SubscriberID: input.SubscriberID, Removed: true}, nil, sdkgo.Receipt{})
}

func decodeTagsPage(body []byte) (TagsPage, error) {
	var wire struct {
		Tags       []TagMetadata `json:"tags"`
		Pagination *struct {
			HasNextPage bool   `json:"has_next_page"`
			EndCursor   string `json:"end_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Tags == nil || wire.Pagination == nil {
		return TagsPage{}, fmt.Errorf("invalid tag page")
	}
	page := TagsPage{Tags: wire.Tags}
	if wire.Pagination.HasNextPage {
		if wire.Pagination.EndCursor == "" {
			return TagsPage{}, fmt.Errorf("missing tag cursor")
		}
		page.NextCursor = wire.Pagination.EndCursor
	}
	for _, tag := range page.Tags {
		if tag.ID <= 0 || tag.Name == "" {
			return TagsPage{}, fmt.Errorf("invalid tag identity")
		}
	}
	return page, nil
}

func decodeTag(body []byte) (TagMetadata, error) {
	var wire struct {
		Tag TagMetadata `json:"tag"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Tag.ID <= 0 || wire.Tag.Name == "" {
		return TagMetadata{}, fmt.Errorf("invalid tag JSON")
	}
	return wire.Tag, nil
}

type mutationBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

func invokeKitMutation[T any](call sdkgo.Call, client *Client, operation, method, path string, requestBody []byte, branches mutationBranches, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	body, status, retryAfter, err := client.request(call, method, path, requestBody)
	if outcome, handled := classifyKitMutation[T](body, status, retryAfter, err, operation, branches); handled {
		return outcome
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return kitMutationBranch(operation, branches.rejected, *new(T), sdkgo.FailureProviderRejection, "Kit rejected HTTP "+strconv.Itoa(status))
	}
	value, err := decode(body)
	if err != nil {
		return kitMutationBranch(operation, branches.invalid, *new(T), sdkgo.FailureProtocol, "Kit returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(branches.success, value, nil, sdkgo.Receipt{})
}

func classifyKitMutation[T any](_ []byte, status int, retryAfter time.Duration, err error, operation string, branches mutationBranches) (sdkgo.MutationAttempt[T], bool) {
	var zero T
	if errors.Is(err, errCredential) {
		return kitMutationBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "Kit credentials are unavailable"), true
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[T](kitFailure(operation, sdkgo.FailureTransport, "Kit request failed"), 0), true
	}
	if status == 429 {
		return sdkgo.NewMutationRetry[T](kitFailure(operation, sdkgo.FailureRateLimit, "Kit rate limited the request"), retryAfter), true
	}
	if status == 408 || status >= 500 {
		return sdkgo.NewMutationRetry[T](kitFailure(operation, sdkgo.FailureAvailability, "Kit temporarily unavailable"), retryAfter), true
	}
	if status == 404 && branches.notFound != "" {
		return kitMutationBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "Kit did not find the resource"), true
	}
	if status >= 400 {
		return kitMutationBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Kit rejected HTTP "+strconv.Itoa(status)), true
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return kitMutationBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "Kit response exceeded configured limit"), true
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](kitFailure(operation, sdkgo.FailureTransport, "Kit response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}

func kitFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "kit", Operation: operation, Message: message}
}

func kitMutationBranch[T any](operation string, branch sdkgo.BranchID, zero T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := kitFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
