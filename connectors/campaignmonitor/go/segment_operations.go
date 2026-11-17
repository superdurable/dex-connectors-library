// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListSegmentsInput selects one list's segments.
type ListSegmentsInput struct {
	// ListID is returned by ListSubscriberLists.
	ListID string `json:"listId"`
}

// SegmentIdentity identifies one segment and its owning list when known.
type SegmentIdentity struct {
	// ID is the opaque segment ID.
	ID string `json:"id"`
	// ListID is the owning list ID when known.
	ListID string `json:"listId"`
	// Title is the segment title when known.
	Title string `json:"title"`
}

// SegmentsList contains segment identities for one list.
type SegmentsList struct {
	// Segments are the visible segments in the list.
	Segments []SegmentIdentity `json:"segments"`
}

// GetSegmentInput selects one segment by ID.
type GetSegmentInput struct {
	// SegmentID is returned by ListSegments.
	SegmentID string `json:"segmentId"`
}

// SegmentRule selects subscribers using a provider rule type and clause.
type SegmentRule struct {
	// RuleType is a provider-supported field such as EmailAddress.
	RuleType string `json:"ruleType"`
	// Clause is a provider expression such as CONTAINS @example.com.
	Clause string `json:"clause"`
}

// SegmentRuleGroup combines its rules according to Campaign Monitor semantics.
type SegmentRuleGroup struct {
	// Rules are nonempty provider rule expressions.
	Rules []SegmentRule `json:"rules"`
}

// SegmentProfile contains a segment's identity, active count, and rules.
type SegmentProfile struct {
	// ID is the opaque segment ID.
	ID string `json:"id"`
	// ListID is the owning list ID.
	ListID string `json:"listId"`
	// Title is the segment name.
	Title string `json:"title"`
	// ActiveSubscribers is the current provider count.
	ActiveSubscribers int `json:"activeSubscribers"`
	// RuleGroups are the provider's current rules.
	RuleGroups []SegmentRuleGroup `json:"ruleGroups"`
}

// CreateSegmentInput creates a segment on a subscriber list.
type CreateSegmentInput struct {
	// ListID is returned by ListSubscriberLists.
	ListID string `json:"listId"`
	// Title is the nonempty segment title.
	Title string `json:"title"`
	// RuleGroups optionally constrain subscribers; nil creates a segment without rules.
	RuleGroups []SegmentRuleGroup `json:"ruleGroups,omitempty"`
}

// UpdateSegmentInput replaces the title and optionally all rules.
type UpdateSegmentInput struct {
	// SegmentID is returned by ListSegments.
	SegmentID string `json:"segmentId"`
	// Title is the replacement segment title.
	Title string `json:"title"`
	// RuleGroups replaces all rules when nonnil; nil preserves existing rules.
	RuleGroups []SegmentRuleGroup `json:"ruleGroups,omitempty"`
}

// DeleteSegmentInput selects a segment for deletion.
type DeleteSegmentInput struct {
	// SegmentID is returned by ListSegments.
	SegmentID string `json:"segmentId"`
}

// ListSegmentsOperation reads segments in one list.
type ListSegmentsOperation struct{ client *Client }

// GetSegmentOperation reads one segment's details.
type GetSegmentOperation struct{ client *Client }

// CreateSegmentOperation creates a segment with bounded sends.
type CreateSegmentOperation struct{ client *Client }

// UpdateSegmentOperation replaces segment title and optional rules.
type UpdateSegmentOperation struct{ client *Client }

// DeleteSegmentOperation deletes a segment.
type DeleteSegmentOperation struct{ client *Client }

// ListSegments binds segment discovery to this client.
func (client *Client) ListSegments() ListSegmentsOperation { return ListSegmentsOperation{client} }

// GetSegment binds segment lookup to this client.
func (client *Client) GetSegment() GetSegmentOperation { return GetSegmentOperation{client} }

// CreateSegment binds segment creation to this client.
func (client *Client) CreateSegment() CreateSegmentOperation { return CreateSegmentOperation{client} }

// UpdateSegment binds segment replacement to this client.
func (client *Client) UpdateSegment() UpdateSegmentOperation { return UpdateSegmentOperation{client} }

// DeleteSegment binds segment deletion to this client.
func (client *Client) DeleteSegment() DeleteSegmentOperation { return DeleteSegmentOperation{client} }

// Definition returns the generated segment discovery contract.
func (ListSegmentsOperation) Definition() sdkgo.QueryDefinition { return ListSegmentsDefinition }

// Definition returns the generated segment lookup contract.
func (GetSegmentOperation) Definition() sdkgo.QueryDefinition { return GetSegmentDefinition }

// Definition returns the generated segment creation contract.
func (CreateSegmentOperation) Definition() sdkgo.MutationDefinition { return CreateSegmentDefinition }

// Definition returns the generated segment update contract.
func (UpdateSegmentOperation) Definition() sdkgo.MutationDefinition { return UpdateSegmentDefinition }

// Definition returns the generated segment deletion contract.
func (DeleteSegmentOperation) Definition() sdkgo.MutationDefinition { return DeleteSegmentDefinition }

// IdempotencyKey preserves identity across bounded segment creation attempts.
func (CreateSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across segment replacements.
func (UpdateSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across segment deletions.
func (DeleteSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists segment identities for one subscriber list.
func (operation ListSegmentsOperation) Invoke(call sdkgo.Call, input ListSegmentsInput) sdkgo.QueryAttempt[SegmentsList] {
	const name = "listSegments"
	if !validCampaignMonitorID(input.ListID) {
		return queryBranch(name, ListSegmentsBranchDefect, SegmentsList{}, sdkgo.FailureValidation, "list ID is invalid")
	}
	return invokeQuery(call, operation.client, name, "/lists/"+url.PathEscape(input.ListID)+"/segments.json", queryBranches{found: ListSegmentsBranchListed, rejected: ListSegmentsBranchProviderRejected, invalid: ListSegmentsBranchInvalidResponse, defect: ListSegmentsBranchDefect}, func(body []byte) (SegmentsList, error) {
		var rows []struct {
			ID     string `json:"SegmentID"`
			ListID string `json:"ListID"`
			Title  string `json:"Title"`
		}
		if err := json.Unmarshal(body, &rows); err != nil || rows == nil {
			return SegmentsList{}, errors.New("segments are invalid")
		}
		result := SegmentsList{Segments: make([]SegmentIdentity, 0, len(rows))}
		for _, row := range rows {
			if !validCampaignMonitorID(row.ID) || row.ListID != input.ListID || row.Title == "" {
				return SegmentsList{}, errors.New("segment identity is invalid")
			}
			result.Segments = append(result.Segments, SegmentIdentity{ID: row.ID, ListID: row.ListID, Title: row.Title})
		}
		return result, nil
	})
}

// Invoke reads segment identity, active count, and current rules.
func (operation GetSegmentOperation) Invoke(call sdkgo.Call, input GetSegmentInput) sdkgo.QueryAttempt[SegmentProfile] {
	const name = "getSegment"
	if !validCampaignMonitorID(input.SegmentID) {
		return queryBranch(name, GetSegmentBranchDefect, SegmentProfile{}, sdkgo.FailureValidation, "segment ID is invalid")
	}
	return invokeQuery(call, operation.client, name, segmentPath(input.SegmentID), queryBranches{found: GetSegmentBranchFound, notFound: GetSegmentBranchNotFound, rejected: GetSegmentBranchProviderRejected, invalid: GetSegmentBranchInvalidResponse, defect: GetSegmentBranchDefect}, func(body []byte) (SegmentProfile, error) {
		var wire struct {
			ID                string `json:"SegmentID"`
			ListID            string `json:"ListID"`
			Title             string `json:"Title"`
			ActiveSubscribers int    `json:"ActiveSubscribers"`
			RuleGroups        []struct {
				Rules []struct {
					RuleType string `json:"RuleType"`
					Clause   string `json:"Clause"`
				} `json:"Rules"`
			} `json:"RuleGroups"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.ID != input.SegmentID || !validCampaignMonitorID(wire.ListID) || wire.Title == "" || wire.ActiveSubscribers < 0 || wire.RuleGroups == nil {
			return SegmentProfile{}, errors.New("segment details are invalid")
		}
		result := SegmentProfile{ID: wire.ID, ListID: wire.ListID, Title: wire.Title, ActiveSubscribers: wire.ActiveSubscribers, RuleGroups: make([]SegmentRuleGroup, 0, len(wire.RuleGroups))}
		for _, group := range wire.RuleGroups {
			if group.Rules == nil {
				return SegmentProfile{}, errors.New("segment rules are invalid")
			}
			rules := make([]SegmentRule, 0, len(group.Rules))
			for _, rule := range group.Rules {
				if rule.RuleType == "" || rule.Clause == "" {
					return SegmentProfile{}, errors.New("segment rule is invalid")
				}
				rules = append(rules, SegmentRule{RuleType: rule.RuleType, Clause: rule.Clause})
			}
			result.RuleGroups = append(result.RuleGroups, SegmentRuleGroup{Rules: rules})
		}
		return result, nil
	})
}

// Invoke creates a segment and reads its list after an unknown send.
func (operation CreateSegmentOperation) Invoke(call sdkgo.Call, input CreateSegmentInput) sdkgo.MutationAttempt[SegmentIdentity] {
	const name = "createSegment"
	if !validCampaignMonitorID(input.ListID) || strings.TrimSpace(input.Title) == "" || !validSegmentRuleGroups(input.RuleGroups) {
		return segmentMutationBranch(name, CreateSegmentBranchDefect, SegmentIdentity{}, sdkgo.FailureValidation, "list ID, title, or rule groups are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateSegmentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return segmentMutationBranch(name, CreateSegmentBranchDefect, SegmentIdentity{}, sdkgo.FailureLocalDefect, "segment dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if segmentID := operation.findCreatedSegment(call, input.ListID, input.Title); segmentID != "" {
			return sdkgo.NewMutationBranch(CreateSegmentBranchCreated, SegmentIdentity{ID: segmentID, ListID: input.ListID, Title: input.Title}, nil, sdkgo.Receipt{})
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SegmentIdentity{}, queryFailure(name, sdkgo.FailureTransport, "an earlier segment may have been created"), sdkgo.Receipt{})
	}
	payload := segmentPayload(input.Title, input.RuleGroups)
	var body []byte
	var status int
	var retryAfter time.Duration
	var requestErr error
	err = fence.Dispatch(func(ctx context.Context) error {
		body, status, retryAfter, requestErr = operation.client.requestWithContext(ctx, call, http.MethodPost, "/segments/"+url.PathEscape(input.ListID)+".json", payload)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureAvailability, "segment dispatch was not recorded or sent"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureAvailability, "segment retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, kind, "Campaign Monitor asked to retry segment creation"), retryAfter)
	}
	if err != nil || status == http.StatusRequestTimeout || status >= 500 {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(SegmentIdentity{}, queryFailure(name, sdkgo.FailureTransport, "segment outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureTransport, "segment outcome is unknown"), 0)
	}
	if status != http.StatusCreated {
		return segmentMutationBranch(name, CreateSegmentBranchProviderRejected, SegmentIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	}
	var segmentID string
	if json.Unmarshal(body, &segmentID) != nil || !validCampaignMonitorID(segmentID) {
		return sdkgo.NewMutationUncertain(SegmentIdentity{}, queryFailure(name, sdkgo.FailureProtocol, "Campaign Monitor created a segment but its ID is unknown"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateSegmentBranchCreated, SegmentIdentity{ID: segmentID, ListID: input.ListID, Title: input.Title}, nil, sdkgo.Receipt{})
}

func (operation CreateSegmentOperation) findCreatedSegment(call sdkgo.Call, listID, title string) string {
	body, status, _, err := operation.client.get(call, "/lists/"+url.PathEscape(listID)+"/segments.json")
	if err != nil || status != http.StatusOK {
		return ""
	}
	var rows []struct {
		ID     string `json:"SegmentID"`
		ListID string `json:"ListID"`
		Title  string `json:"Title"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return ""
	}
	var found string
	for _, row := range rows {
		if row.ListID == listID && row.Title == title {
			if found != "" || !validCampaignMonitorID(row.ID) {
				return ""
			}
			found = row.ID
		}
	}
	return found
}

// Invoke replaces the title and, when provided, all rule groups.
func (operation UpdateSegmentOperation) Invoke(call sdkgo.Call, input UpdateSegmentInput) sdkgo.MutationAttempt[SegmentIdentity] {
	const name = "updateSegment"
	if !validCampaignMonitorID(input.SegmentID) || strings.TrimSpace(input.Title) == "" || !validSegmentRuleGroups(input.RuleGroups) {
		return segmentMutationBranch(name, UpdateSegmentBranchDefect, SegmentIdentity{}, sdkgo.FailureValidation, "segment ID, title, or rule groups are invalid")
	}
	return invokeSegmentWrite(call, operation.client, name, http.MethodPut, segmentPath(input.SegmentID), segmentPayload(input.Title, input.RuleGroups), SegmentIdentity{ID: input.SegmentID, Title: input.Title}, UpdateSegmentBranchUpdated, UpdateSegmentBranchNotFound, UpdateSegmentBranchProviderRejected, UpdateSegmentBranchInvalidResponse, UpdateSegmentBranchDefect)
}

// Invoke deletes a segment unless Campaign Monitor protects a system segment or draft dependency.
func (operation DeleteSegmentOperation) Invoke(call sdkgo.Call, input DeleteSegmentInput) sdkgo.MutationAttempt[SegmentIdentity] {
	const name = "deleteSegment"
	if !validCampaignMonitorID(input.SegmentID) {
		return segmentMutationBranch(name, DeleteSegmentBranchDefect, SegmentIdentity{}, sdkgo.FailureValidation, "segment ID is invalid")
	}
	return invokeSegmentWrite(call, operation.client, name, http.MethodDelete, segmentPath(input.SegmentID), nil, SegmentIdentity{ID: input.SegmentID}, DeleteSegmentBranchDeleted, "", DeleteSegmentBranchProviderRejected, DeleteSegmentBranchInvalidResponse, DeleteSegmentBranchDefect)
}

func invokeSegmentWrite(call sdkgo.Call, client *Client, name, method, path string, payload any, identity SegmentIdentity, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[SegmentIdentity] {
	body, status, retryAfter, err := client.request(call, method, path, payload)
	switch {
	case errors.Is(err, errCredential):
		return segmentMutationBranch(name, defect, SegmentIdentity{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	case errors.Is(err, errRequest):
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor request failed"), 0)
	case status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureRateLimit, "Campaign Monitor rate limited the request"), retryAfter)
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureAvailability, "Campaign Monitor is temporarily unavailable"), retryAfter)
	case status == http.StatusNotFound && notFound != "":
		return segmentMutationBranch(name, notFound, SegmentIdentity{}, sdkgo.FailureNotFound, "segment was not found")
	case status != http.StatusOK && status != http.StatusNoContent:
		return segmentMutationBranch(name, rejected, SegmentIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return segmentMutationBranch(name, invalid, SegmentIdentity{}, sdkgo.FailureResponseTooLarge, "Campaign Monitor response exceeded the limit")
	case err != nil:
		return sdkgo.NewMutationRetry[SegmentIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor response was interrupted"), 0)
	case len(body) != 0:
		return segmentMutationBranch(name, invalid, SegmentIdentity{}, sdkgo.FailureProtocol, "Campaign Monitor returned an unexpected response body")
	}
	return sdkgo.NewMutationBranch(success, identity, nil, sdkgo.Receipt{})
}

func segmentPath(segmentID string) string { return "/segments/" + url.PathEscape(segmentID) + ".json" }

func validSegmentRuleGroups(groups []SegmentRuleGroup) bool {
	for _, group := range groups {
		if len(group.Rules) == 0 {
			return false
		}
		for _, rule := range group.Rules {
			if strings.TrimSpace(rule.RuleType) == "" || strings.TrimSpace(rule.Clause) == "" {
				return false
			}
		}
	}
	return true
}

func segmentPayload(title string, groups []SegmentRuleGroup) map[string]any {
	payload := map[string]any{"Title": title}
	if groups != nil {
		wireGroups := make([]map[string]any, 0, len(groups))
		for _, group := range groups {
			wireRules := make([]map[string]string, 0, len(group.Rules))
			for _, rule := range group.Rules {
				wireRules = append(wireRules, map[string]string{"RuleType": rule.RuleType, "Clause": rule.Clause})
			}
			wireGroups = append(wireGroups, map[string]any{"Rules": wireRules})
		}
		payload["RuleGroups"] = wireGroups
	}
	return payload
}

func segmentMutationBranch(name string, branch sdkgo.BranchID, value SegmentIdentity, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SegmentIdentity] {
	failure := queryFailure(name, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
