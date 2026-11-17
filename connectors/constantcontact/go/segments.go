// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SegmentMetadata holds one segment's identity and selection criteria when available.
type SegmentMetadata struct {
	// SegmentID is Constant Contact's positive numeric segment identity.
	SegmentID int64 `json:"segmentId"`
	// Name is unique within the account.
	Name string `json:"name"`
	// SegmentCriteria is a JSON object encoded as one string, absent on list results.
	SegmentCriteria string `json:"segmentCriteria,omitempty"`
}

// GetSegmentInput identifies a segment by its positive numeric ID.
type GetSegmentInput struct {
	SegmentID int64 `json:"segmentId"`
}

// ListSegmentsInput requests one page of segments.
type ListSegmentsInput struct {
	// Limit is 1 through 1000; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// SortBy is date or name; blank uses descending edit date.
	SortBy string `json:"sortBy,omitempty"`
	// Cursor is an opaque next token from the preceding page; blank starts again.
	Cursor string `json:"cursor,omitempty"`
}

// SegmentsPage contains one page and a next token when the provider has more.
type SegmentsPage struct {
	// Segments contains the returned segment summaries.
	Segments []SegmentMetadata `json:"segments"`
	// NextCursor is blank after the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// CreateSegmentInput supplies a unique name and full criteria JSON string.
type CreateSegmentInput struct {
	// Name is the account-unique segment name.
	Name string `json:"name"`
	// SegmentCriteria is the provider's criteria object encoded as JSON text.
	SegmentCriteria string `json:"segmentCriteria"`
}

// UpdateSegmentInput replaces the name and full criteria of one segment.
type UpdateSegmentInput struct {
	// SegmentID is the positive numeric identity.
	SegmentID int64 `json:"segmentId"`
	// Name is the account-unique replacement name.
	Name string `json:"name"`
	// SegmentCriteria is the complete replacement criteria JSON text.
	SegmentCriteria string `json:"segmentCriteria"`
}

// DeleteSegmentInput identifies the active segment to delete.
type DeleteSegmentInput struct {
	SegmentID int64 `json:"segmentId"`
}

// DeleteSegmentOutput identifies the segment removed from the active collection.
type DeleteSegmentOutput struct {
	SegmentID int64 `json:"segmentId"`
}

// GetSegmentOperation reads one segment.
type GetSegmentOperation struct{ client *Client }

// ListSegmentsOperation reads one segment page.
type ListSegmentsOperation struct{ client *Client }

// CreateSegmentOperation creates a segment with bounded sends.
type CreateSegmentOperation struct{ client *Client }

// UpdateSegmentOperation replaces a segment.
type UpdateSegmentOperation struct{ client *Client }

// DeleteSegmentOperation removes an active segment.
type DeleteSegmentOperation struct{ client *Client }

// GetSegment binds the Query to this client.
func (client *Client) GetSegment() GetSegmentOperation { return GetSegmentOperation{client} }

// ListSegments binds the Query to this client.
func (client *Client) ListSegments() ListSegmentsOperation { return ListSegmentsOperation{client} }

// CreateSegment binds the Mutation to this client.
func (client *Client) CreateSegment() CreateSegmentOperation { return CreateSegmentOperation{client} }

// UpdateSegment binds the Mutation to this client.
func (client *Client) UpdateSegment() UpdateSegmentOperation { return UpdateSegmentOperation{client} }

// DeleteSegment binds the Mutation to this client.
func (client *Client) DeleteSegment() DeleteSegmentOperation { return DeleteSegmentOperation{client} }

// Definition returns the generated Query contract.
func (GetSegmentOperation) Definition() sdkgo.QueryDefinition { return GetSegmentDefinition }

// Definition returns the generated Query contract.
func (ListSegmentsOperation) Definition() sdkgo.QueryDefinition { return ListSegmentsDefinition }

// Definition returns the generated Mutation contract.
func (CreateSegmentOperation) Definition() sdkgo.MutationDefinition { return CreateSegmentDefinition }

// Definition returns the generated Mutation contract.
func (UpdateSegmentOperation) Definition() sdkgo.MutationDefinition { return UpdateSegmentDefinition }

// Definition returns the generated Mutation contract.
func (DeleteSegmentOperation) Definition() sdkgo.MutationDefinition { return DeleteSegmentDefinition }

// IdempotencyKey correlates bounded create attempts; the provider does not accept the key.
func (CreateSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts of an absolute replacement.
func (UpdateSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts of a repeatable deletion request.
func (DeleteSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one segment including its criteria.
func (operation GetSegmentOperation) Invoke(call sdkgo.Call, input GetSegmentInput) sdkgo.QueryAttempt[SegmentMetadata] {
	if input.SegmentID <= 0 {
		return listQueryBranch[SegmentMetadata]("getSegment", GetSegmentBranchDefect, sdkgo.FailureValidation, "segment ID must be positive")
	}
	response := operation.client.get(call, "/segments/"+strconv.FormatInt(input.SegmentID, 10))
	if kind, failure := classifyListResponse("getSegment", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewQueryRetry[SegmentMetadata](failure, response.retryAfter)
		}
		branch := GetSegmentBranchProviderRejected
		if kind == listResponseDefect {
			branch = GetSegmentBranchDefect
		} else if kind == listResponseInvalid {
			branch = GetSegmentBranchInvalidResponse
		} else if response.status == http.StatusNotFound {
			branch = GetSegmentBranchNotFound
		}
		return sdkgo.NewQueryBranch(branch, SegmentMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listQueryBranch[SegmentMetadata]("getSegment", GetSegmentBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected the segment lookup")
	}
	segment, err := decodeSegment(response.body, true)
	if err != nil {
		return listQueryBranch[SegmentMetadata]("getSegment", GetSegmentBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid segment")
	}
	return sdkgo.NewQueryBranch(GetSegmentBranchFound, segment, nil, sdkgo.Receipt{})
}

// Invoke reads one bounded segment page.
func (operation ListSegmentsOperation) Invoke(call sdkgo.Call, input ListSegmentsInput) sdkgo.QueryAttempt[SegmentsPage] {
	if input.Limit < 0 || input.Limit > 1000 || (input.SortBy != "" && input.SortBy != "date" && input.SortBy != "name") || len(input.Cursor) > 4096 || strings.ContainsAny(input.Cursor, "\r\n") {
		return listQueryBranch[SegmentsPage]("listSegments", ListSegmentsBranchDefect, sdkgo.FailureValidation, "invalid segment page options")
	}
	parameters := url.Values{}
	if input.Limit > 0 {
		parameters.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.SortBy != "" {
		parameters.Set("sort_by", input.SortBy)
	}
	if input.Cursor != "" {
		parameters.Set("next", input.Cursor)
	}
	path := "/segments"
	if len(parameters) > 0 {
		path += "?" + parameters.Encode()
	}
	response := operation.client.get(call, path)
	if kind, failure := classifyListResponse("listSegments", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewQueryRetry[SegmentsPage](failure, response.retryAfter)
		}
		branch := ListSegmentsBranchProviderRejected
		if kind == listResponseDefect {
			branch = ListSegmentsBranchDefect
		} else if kind == listResponseInvalid {
			branch = ListSegmentsBranchInvalidResponse
		}
		return sdkgo.NewQueryBranch(branch, SegmentsPage{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listQueryBranch[SegmentsPage]("listSegments", ListSegmentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected the segment page")
	}
	page, err := decodeSegmentsPage(response.body)
	if err != nil {
		return listQueryBranch[SegmentsPage]("listSegments", ListSegmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid segment page")
	}
	return sdkgo.NewQueryBranch(ListSegmentsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke replaces a segment's name and complete criteria.
func (operation UpdateSegmentOperation) Invoke(call sdkgo.Call, input UpdateSegmentInput) sdkgo.MutationAttempt[SegmentMetadata] {
	if input.SegmentID <= 0 || !validSegmentSettings(input.Name, input.SegmentCriteria) {
		return listMutationBranch[SegmentMetadata]("updateSegment", UpdateSegmentBranchDefect, sdkgo.FailureValidation, "invalid segment settings")
	}
	payload, err := segmentPayload(input.Name, input.SegmentCriteria)
	if err != nil {
		return listMutationBranch[SegmentMetadata]("updateSegment", UpdateSegmentBranchDefect, sdkgo.FailureLocalDefect, "segment request could not be encoded")
	}
	response := operation.client.send(call, http.MethodPut, "/segments/"+strconv.FormatInt(input.SegmentID, 10), payload)
	if kind, failure := classifyListResponse("updateSegment", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewMutationRetry[SegmentMetadata](failure, response.retryAfter)
		}
		branch := UpdateSegmentBranchProviderRejected
		if kind == listResponseDefect {
			branch = UpdateSegmentBranchDefect
		} else if kind == listResponseInvalid {
			branch = UpdateSegmentBranchInvalidResponse
		} else if response.status == http.StatusNotFound {
			branch = UpdateSegmentBranchNotFound
		}
		return sdkgo.NewMutationBranch(branch, SegmentMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listMutationBranch[SegmentMetadata]("updateSegment", UpdateSegmentBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected the segment update")
	}
	segment, err := decodeSegment(response.body, true)
	if err != nil {
		return listMutationBranch[SegmentMetadata]("updateSegment", UpdateSegmentBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid segment")
	}
	return sdkgo.NewMutationBranch(UpdateSegmentBranchUpdated, segment, nil, sdkgo.Receipt{})
}

// Invoke removes an active segment; provider deletion preserves historical details.
func (operation DeleteSegmentOperation) Invoke(call sdkgo.Call, input DeleteSegmentInput) sdkgo.MutationAttempt[DeleteSegmentOutput] {
	if input.SegmentID <= 0 {
		return listMutationBranch[DeleteSegmentOutput]("deleteSegment", DeleteSegmentBranchDefect, sdkgo.FailureValidation, "segment ID must be positive")
	}
	response := operation.client.send(call, http.MethodDelete, "/segments/"+strconv.FormatInt(input.SegmentID, 10), nil)
	if kind, failure := classifyListResponse("deleteSegment", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewMutationRetry[DeleteSegmentOutput](failure, response.retryAfter)
		}
		branch := DeleteSegmentBranchProviderRejected
		if kind == listResponseDefect {
			branch = DeleteSegmentBranchDefect
		} else if response.status == http.StatusNotFound {
			branch = DeleteSegmentBranchNotFound
		}
		return sdkgo.NewMutationBranch(branch, DeleteSegmentOutput{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusNoContent {
		return listMutationBranch[DeleteSegmentOutput]("deleteSegment", DeleteSegmentBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected the segment deletion")
	}
	return sdkgo.NewMutationBranch(DeleteSegmentBranchDeleted, DeleteSegmentOutput{SegmentID: input.SegmentID}, nil, sdkgo.Receipt{})
}

func validSegmentSettings(name string, criteria string) bool {
	if strings.TrimSpace(name) == "" || len(name) > 255 || len(criteria) > 16384 {
		return false
	}
	var document map[string]json.RawMessage
	return json.Unmarshal([]byte(criteria), &document) == nil && len(document) > 0
}

func segmentPayload(name string, criteria string) ([]byte, error) {
	return json.Marshal(struct {
		Name            string `json:"name"`
		SegmentCriteria string `json:"segment_criteria"`
	}{name, criteria})
}

func decodeSegment(body []byte, requireCriteria bool) (SegmentMetadata, error) {
	var raw struct {
		SegmentID       int64  `json:"segment_id"`
		Name            string `json:"name"`
		SegmentCriteria string `json:"segment_criteria"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return SegmentMetadata{}, err
	}
	if raw.SegmentID <= 0 || strings.TrimSpace(raw.Name) == "" || (requireCriteria && !validSegmentSettings(raw.Name, raw.SegmentCriteria)) {
		return SegmentMetadata{}, errors.New("segment fields missing or invalid")
	}
	return SegmentMetadata{SegmentID: raw.SegmentID, Name: raw.Name, SegmentCriteria: raw.SegmentCriteria}, nil
}

func decodeSegmentsPage(body []byte) (SegmentsPage, error) {
	var envelope struct {
		Segments []json.RawMessage `json:"segments"`
		Links    struct {
			Next struct {
				Href string `json:"href"`
			} `json:"next"`
		} `json:"_links"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Segments == nil {
		return SegmentsPage{}, errors.New("segment collection missing")
	}
	page := SegmentsPage{Segments: make([]SegmentMetadata, 0, len(envelope.Segments))}
	for _, raw := range envelope.Segments {
		segment, err := decodeSegment(raw, false)
		if err != nil {
			return SegmentsPage{}, err
		}
		page.Segments = append(page.Segments, segment)
	}
	if envelope.Links.Next.Href != "" {
		link, err := url.Parse(envelope.Links.Next.Href)
		if err != nil || link.IsAbs() {
			return SegmentsPage{}, errors.New("invalid segment next link")
		}
		page.NextCursor = link.Query().Get("next")
		if page.NextCursor == "" || len(page.NextCursor) > 4096 || strings.ContainsAny(page.NextCursor, "\r\n") {
			return SegmentsPage{}, errors.New("segment cursor missing")
		}
	}
	return page, nil
}

// Invoke creates a segment using bounded sends and unique-name read-back.
func (operation CreateSegmentOperation) Invoke(call sdkgo.Call, input CreateSegmentInput) sdkgo.MutationAttempt[SegmentMetadata] {
	if !validSegmentSettings(input.Name, input.SegmentCriteria) {
		return listMutationBranch[SegmentMetadata]("createSegment", CreateSegmentBranchDefect, sdkgo.FailureValidation, "invalid segment settings")
	}
	payload, err := segmentPayload(input.Name, input.SegmentCriteria)
	if err != nil {
		return listMutationBranch[SegmentMetadata]("createSegment", CreateSegmentBranchDefect, sdkgo.FailureLocalDefect, "segment request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateSegmentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return listMutationBranch[SegmentMetadata]("createSegment", CreateSegmentBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createSegmentUncertain("previous segment creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		segment, isKnown, shouldRetry := operation.client.findSegmentByName(call, input)
		if shouldRetry {
			return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureAvailability, "segment read-back unavailable"), 0)
		}
		if segment != nil {
			return sdkgo.NewMutationBranch(CreateSegmentBranchCreated, *segment, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return createSegmentUncertain("previous segment creation cannot be confirmed")
		}
	}
	credentials, credentialResponse := operation.client.resolveCredentials(call)
	if credentialResponse.err != nil {
		if decision.Kind == singledispatch.SendAllowed {
			if err := fence.RecordNotSent(); err != nil {
				return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureAvailability, "dispatch checkpoint unavailable"), 0)
			}
		}
		if errors.Is(credentialResponse.err, errCredential) {
			return listMutationBranch[SegmentMetadata]("createSegment", CreateSegmentBranchDefect, sdkgo.FailureAuthentication, "Constant Contact credentials are unavailable or invalid")
		}
		return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureAvailability, "Constant Contact credentials could not be refreshed"), 0)
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.sendAuthorized(requestContext, credentials, http.MethodPost, "/segments", payload)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", kind, "Constant Contact did not create the segment"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureTransport, "segment creation outcome is unknown"), 0)
	}
	if kind, failure := classifyListResponse("createSegment", response); kind != listResponseSuccess {
		if kind == listResponseRetry || kind == listResponseInvalid {
			return sdkgo.NewMutationRetry[SegmentMetadata](failure, response.retryAfter)
		}
		if decision.MaximumPriorSends > 0 {
			return createSegmentUncertain("an earlier segment creation may have succeeded")
		}
		branch := CreateSegmentBranchProviderRejected
		if response.status == http.StatusConflict {
			branch = CreateSegmentBranchAlreadyExists
		}
		if kind == listResponseDefect {
			branch = CreateSegmentBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, SegmentMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return listMutationBranch[SegmentMetadata]("createSegment", CreateSegmentBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected segment creation")
	}
	segment, err := decodeSegment(response.body, true)
	if err != nil {
		return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureProtocol, "Constant Contact returned an invalid segment"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(segment.SegmentID, 10)); err != nil {
		return sdkgo.NewMutationRetry[SegmentMetadata](listFailure("createSegment", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateSegmentBranchCreated, segment, nil, sdkgo.Receipt{})
}

func (client *Client) findSegmentByName(call sdkgo.Call, input CreateSegmentInput) (*SegmentMetadata, bool, bool) {
	response := client.get(call, "/segments?limit=1000")
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	page, err := decodeSegmentsPage(response.body)
	if err != nil {
		return nil, false, true
	}
	for _, candidate := range page.Segments {
		if candidate.Name != input.Name {
			continue
		}
		full := client.get(call, "/segments/"+strconv.FormatInt(candidate.SegmentID, 10))
		if full.err != nil || full.status != http.StatusOK {
			return nil, false, true
		}
		segment, err := decodeSegment(full.body, true)
		if err != nil {
			return nil, false, true
		}
		if segment.SegmentCriteria == input.SegmentCriteria {
			return &segment, true, false
		}
		return nil, false, false
	}
	return nil, page.NextCursor == "", false
}

func createSegmentUncertain(message string) sdkgo.MutationAttempt[SegmentMetadata] {
	return sdkgo.NewMutationUncertain(SegmentMetadata{}, listFailure("createSegment", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
