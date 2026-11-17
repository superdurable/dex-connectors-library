// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package segment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateSourceInput creates a Source type whose metadata needs no secret settings.
type CreateSourceInput struct {
	// Slug is the unique Source slug in the Workspace.
	Slug string `json:"slug"`
	// MetadataID identifies a Source type from Segment's Source catalog.
	MetadataID string `json:"metadataId"`
	// Enabled controls whether the new Source accepts events.
	Enabled bool `json:"enabled"`
}

// CreateSourceOperation creates a Source under the token's Workspace.
type CreateSourceOperation struct{ client *Client }

// CreateSource binds the createSource Mutation.
func (client *Client) CreateSource() CreateSourceOperation { return CreateSourceOperation{client} }

// Definition returns the generated createSource contract.
func (CreateSourceOperation) Definition() sdkgo.MutationDefinition { return CreateSourceDefinition }

// IdempotencyKey records the call identity; Segment does not accept it as a create key.
func (CreateSourceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSourceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one Source, limiting repeats after unknown outcomes.
func (operation CreateSourceOperation) Invoke(call sdkgo.Call, input CreateSourceInput) sdkgo.MutationAttempt[SourceProfile] {
	if !validSourceID(input.Slug) || !validSourceID(input.MetadataID) {
		return sourceMutationBranch[SourceProfile]("createSource", CreateSourceBranchDefect, sdkgo.FailureValidation, "source slug or metadata ID is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateSourceDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sourceMutationBranch[SourceProfile]("createSource", CreateSourceBranchDefect, sdkgo.FailureLocalDefect, "source dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SourceProfile{}, managementFailure("createSource", sdkgo.FailureTransport, "an earlier source creation may have succeeded"), sdkgo.Receipt{})
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	credentials, credentialErr := operation.client.credentials.Resolve(call)
	if credentialErr != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SourceProfile{}, managementFailure("createSource", sdkgo.FailureAuthentication, "earlier source creation may have succeeded and credentials are unavailable"), sdkgo.Receipt{})
		}
		if err := fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[SourceProfile](managementFailure("createSource", sdkgo.FailureAvailability, "source no-send checkpoint failed"), 0)
		}
		return sourceMutationBranch[SourceProfile]("createSource", CreateSourceBranchDefect, sdkgo.FailureAuthentication, "Segment credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		Slug       string `json:"slug"`
		MetadataID string `json:"metadataId"`
		Enabled    bool   `json:"enabled"`
	}{input.Slug, input.MetadataID, input.Enabled})
	if err != nil {
		return sourceMutationBranch[SourceProfile]("createSource", CreateSourceBranchDefect, sdkgo.FailureLocalDefect, "source create request could not be encoded")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(requestContext, call, http.MethodPost, "/sources", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SourceProfile](managementFailure("createSource", sdkgo.FailureAvailability, "source dispatch checkpoint is unavailable"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SourceProfile](managementFailure("createSource", sdkgo.FailureAvailability, "source resend checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SourceProfile](managementFailure("createSource", kind, "Segment temporarily rejected source creation"), response.retryAfter)
	}
	if err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil && response.status >= 200 && response.status < 300 {
		failure := managementFailure("createSource", sdkgo.FailureTransport, "Segment source creation outcome is unknown")
		if canResendUnknown {
			return sdkgo.NewMutationRetry[SourceProfile](failure, 0)
		}
		return sdkgo.NewMutationUncertain(SourceProfile{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SourceProfile{}, managementFailure("createSource", sdkgo.FailureProviderRejection, "an earlier source creation may have succeeded"), sdkgo.Receipt{})
		}
		return sourceMutationBranch[SourceProfile]("createSource", CreateSourceBranchProviderRejected, sdkgo.FailureProviderRejection, "Segment rejected source creation")
	}
	var envelope struct {
		Data struct {
			Source json.RawMessage `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		failure := managementFailure("createSource", sdkgo.FailureProtocol, "Segment returned invalid created source JSON")
		if canResendUnknown {
			return sdkgo.NewMutationRetry[SourceProfile](failure, 0)
		}
		return sdkgo.NewMutationUncertain(SourceProfile{}, failure, sdkgo.Receipt{})
	}
	profile, err := decodeSourceRecord(envelope.Data.Source)
	if err != nil || profile.Slug != input.Slug {
		failure := managementFailure("createSource", sdkgo.FailureProtocol, "Segment returned invalid created source JSON")
		if canResendUnknown {
			return sdkgo.NewMutationRetry[SourceProfile](failure, 0)
		}
		return sdkgo.NewMutationUncertain(SourceProfile{}, failure, sdkgo.Receipt{})
	}
	if err := fence.RecordProviderID(profile.ID); err != nil {
		return sdkgo.NewMutationRetry[SourceProfile](managementFailure("createSource", sdkgo.FailureAvailability, "source result checkpoint is unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateSourceBranchCreated, profile, nil, sdkgo.Receipt{ProviderObjectID: profile.ID})
}

// ListSourcesInput selects a page of Sources in the token's Workspace.
type ListSourcesInput struct {
	// Count limits the page size; zero uses the provider default.
	Count int `json:"count,omitempty"`
	// Cursor is the opaque cursor returned by an earlier page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// SourcePage contains public Source identities without write keys.
type SourcePage struct {
	// Sources contains one page of Source identities.
	Sources []SourceProfile `json:"sources"`
	// CurrentCursor identifies this page for a subsequent list request.
	CurrentCursor string `json:"currentCursor"`
	// TotalEntries is the total count reported by Segment.
	TotalEntries int `json:"totalEntries"`
}

// UpdateSourceInput replaces selected public Source fields.
type UpdateSourceInput struct {
	// SourceID identifies the Source to change.
	SourceID string `json:"sourceId"`
	// Name changes the display name when provided; an empty string clears it.
	Name *string `json:"name,omitempty"`
	// Enabled changes whether this Source accepts data when provided.
	Enabled *bool `json:"enabled,omitempty"`
}

// DeleteSourceInput identifies one Source to delete.
type DeleteSourceInput struct {
	// SourceID is the opaque Segment Source ID.
	SourceID string `json:"sourceId"`
}

// DeletedSource identifies a Source that Segment deleted or found absent.
type DeletedSource struct {
	// SourceID is the opaque Segment Source ID.
	SourceID string `json:"sourceId"`
}

// ListSourcesOperation reads Source identities.
type ListSourcesOperation struct{ client *Client }

// UpdateSourceOperation changes a Source's display name or enabled state.
type UpdateSourceOperation struct{ client *Client }

// DeleteSourceOperation deletes one Source.
type DeleteSourceOperation struct{ client *Client }

// ListSources binds the listSources Query.
func (client *Client) ListSources() ListSourcesOperation { return ListSourcesOperation{client} }

// UpdateSource binds the updateSource Mutation.
func (client *Client) UpdateSource() UpdateSourceOperation { return UpdateSourceOperation{client} }

// DeleteSource binds the deleteSource Mutation.
func (client *Client) DeleteSource() DeleteSourceOperation { return DeleteSourceOperation{client} }

// Definition returns the generated listSources contract.
func (ListSourcesOperation) Definition() sdkgo.QueryDefinition { return ListSourcesDefinition }

// Invoke reads one page of Source identities and classifies provider outcomes.
func (operation ListSourcesOperation) Invoke(call sdkgo.Call, input ListSourcesInput) sdkgo.QueryAttempt[SourcePage] {
	if input.Count < 0 || input.Count > 1000 || len(input.Cursor) > 512 || strings.ContainsAny(input.Cursor, "\r\n") {
		return sourcePageBranch(ListSourcesBranchDefect, sdkgo.FailureValidation, "source pagination is invalid")
	}
	query := url.Values{}
	if input.Count > 0 {
		query.Set("pagination.count", strconv.Itoa(input.Count))
	}
	if input.Cursor != "" {
		query.Set("pagination.cursor", input.Cursor)
	}
	path := "/sources"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return sourcePageBranch(ListSourcesBranchDefect, sdkgo.FailureAuthentication, "Segment credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SourcePage](managementFailure("listSources", sdkgo.FailureTransport, "Segment request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SourcePage](managementFailure("listSources", sdkgo.FailureRateLimit, "Segment rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SourcePage](managementFailure("listSources", sdkgo.FailureAvailability, "Segment is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return sourcePageBranch(ListSourcesBranchProviderRejected, sdkgo.FailureProviderRejection, "Segment rejected the source list")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sourcePageBranch(ListSourcesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Segment source list exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SourcePage](managementFailure("listSources", sdkgo.FailureTransport, "Segment response was interrupted"), 0)
	}
	var wire struct {
		Data struct {
			Sources    []json.RawMessage `json:"sources"`
			Pagination struct {
				Current      string `json:"current"`
				TotalEntries int    `json:"totalEntries"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data.Sources == nil || wire.Data.Pagination.TotalEntries < 0 {
		return sourcePageBranch(ListSourcesBranchInvalidResponse, sdkgo.FailureProtocol, "Segment returned invalid source list JSON")
	}
	page := SourcePage{Sources: make([]SourceProfile, 0, len(wire.Data.Sources)), CurrentCursor: wire.Data.Pagination.Current, TotalEntries: wire.Data.Pagination.TotalEntries}
	for _, raw := range wire.Data.Sources {
		profile, err := decodeSourceRecord(raw)
		if err != nil {
			return sourcePageBranch(ListSourcesBranchInvalidResponse, sdkgo.FailureProtocol, "Segment returned invalid source list JSON")
		}
		page.Sources = append(page.Sources, profile)
	}
	return sdkgo.NewQueryBranch(ListSourcesBranchListed, page, nil, sdkgo.Receipt{})
}

// Definition returns the generated updateSource contract.
func (UpdateSourceOperation) Definition() sdkgo.MutationDefinition { return UpdateSourceDefinition }

// IdempotencyKey records the call identity; absolute replacements are safe to repeat.
func (UpdateSourceOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSourceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected Source fields with exact supplied values.
func (operation UpdateSourceOperation) Invoke(call sdkgo.Call, input UpdateSourceInput) sdkgo.MutationAttempt[SourceProfile] {
	if !validSourceID(input.SourceID) || input.Name == nil && input.Enabled == nil || input.Name != nil && len(*input.Name) > 255 {
		return sourceMutationBranch[SourceProfile]("updateSource", UpdateSourceBranchDefect, sdkgo.FailureValidation, "source ID or update fields are invalid")
	}
	payload := struct {
		Name    *string `json:"name,omitempty"`
		Enabled *bool   `json:"enabled,omitempty"`
	}{input.Name, input.Enabled}
	body, err := json.Marshal(payload)
	if err != nil {
		return sourceMutationBranch[SourceProfile]("updateSource", UpdateSourceBranchDefect, sdkgo.FailureLocalDefect, "source update could not be encoded")
	}
	response := operation.client.request(call.Context, call, http.MethodPatch, "/sources/"+url.PathEscape(input.SourceID), body)
	if retry := sourceMutationRetry[SourceProfile]("updateSource", response); retry != nil {
		return *retry
	}
	if errors.Is(response.err, errCredential) {
		return sourceMutationBranch[SourceProfile]("updateSource", UpdateSourceBranchDefect, sdkgo.FailureAuthentication, "Segment credentials are unavailable or invalid")
	}
	if response.status == http.StatusNotFound {
		return sourceMutationBranch[SourceProfile]("updateSource", UpdateSourceBranchNotFound, sdkgo.FailureNotFound, "Segment did not expose the source")
	}
	if response.status != http.StatusOK {
		return sourceMutationBranch[SourceProfile]("updateSource", UpdateSourceBranchProviderRejected, sdkgo.FailureProviderRejection, "Segment rejected the source update")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[SourceProfile](managementFailure("updateSource", sdkgo.FailureTransport, "Segment update response was interrupted"), 0)
	}
	profile, err := decodeSource(response.body, input.SourceID)
	if err != nil {
		return sdkgo.NewMutationRetry[SourceProfile](managementFailure("updateSource", sdkgo.FailureProtocol, "Segment returned invalid source JSON"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateSourceBranchUpdated, profile, nil, sdkgo.Receipt{})
}

// Definition returns the generated deleteSource contract.
func (DeleteSourceOperation) Definition() sdkgo.MutationDefinition { return DeleteSourceDefinition }

// IdempotencyKey records the call identity; deleting an absent Source is harmless.
func (DeleteSourceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSourceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one Source, treating an already absent Source as deleted.
func (operation DeleteSourceOperation) Invoke(call sdkgo.Call, input DeleteSourceInput) sdkgo.MutationAttempt[DeletedSource] {
	if !validSourceID(input.SourceID) {
		return sourceMutationBranch[DeletedSource]("deleteSource", DeleteSourceBranchDefect, sdkgo.FailureValidation, "source ID is invalid")
	}
	response := operation.client.request(call.Context, call, http.MethodDelete, "/sources/"+url.PathEscape(input.SourceID), nil)
	if retry := sourceMutationRetry[DeletedSource]("deleteSource", response); retry != nil {
		return *retry
	}
	if errors.Is(response.err, errCredential) {
		return sourceMutationBranch[DeletedSource]("deleteSource", DeleteSourceBranchDefect, sdkgo.FailureAuthentication, "Segment credentials are unavailable or invalid")
	}
	if response.status == http.StatusOK || response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteSourceBranchDeleted, DeletedSource{SourceID: input.SourceID}, nil, sdkgo.Receipt{})
	}
	return sourceMutationBranch[DeletedSource]("deleteSource", DeleteSourceBranchProviderRejected, sdkgo.FailureProviderRejection, "Segment rejected the source deletion")
}

func decodeSourceRecord(raw []byte) (SourceProfile, error) {
	var record struct {
		ID   string  `json:"id"`
		Slug string  `json:"slug"`
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return SourceProfile{}, err
	}
	if !validSourceID(record.ID) || strings.TrimSpace(record.Slug) == "" || record.Name == nil {
		return SourceProfile{}, errors.New("invalid source identity")
	}
	return SourceProfile{ID: record.ID, Slug: record.Slug, Name: *record.Name}, nil
}

func managementFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "segment", Operation: operation, Message: message}
}

func sourcePageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SourcePage] {
	failure := managementFailure("listSources", kind, message)
	return sdkgo.NewQueryBranch(branch, SourcePage{}, &failure, sdkgo.Receipt{})
}

func sourceMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := managementFailure(operation, kind, message)
	var empty T
	return sdkgo.NewMutationBranch(branch, empty, &failure, sdkgo.Receipt{})
}

func sourceMutationRetry[T any](operation string, response providerResponse) *sdkgo.MutationAttempt[T] {
	var attempt sdkgo.MutationAttempt[T]
	switch {
	case errors.Is(response.err, errRequest):
		attempt = sdkgo.NewMutationRetry[T](managementFailure(operation, sdkgo.FailureTransport, "Segment request failed"), 0)
	case response.status == http.StatusTooManyRequests:
		attempt = sdkgo.NewMutationRetry[T](managementFailure(operation, sdkgo.FailureRateLimit, "Segment rate limited the request"), response.retryAfter)
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		attempt = sdkgo.NewMutationRetry[T](managementFailure(operation, sdkgo.FailureAvailability, "Segment is temporarily unavailable"), response.retryAfter)
	default:
		return nil
	}
	return &attempt
}
