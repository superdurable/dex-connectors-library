// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetOpportunityByExternalIDInput identifies one opportunity by its stable external key.
type GetOpportunityByExternalIDInput struct {
	// ExternalOpportunityID is the key supplied when the opportunity was created.
	ExternalOpportunityID string `json:"externalOpportunityId"`
}

// OpportunityProfile contains only the provider GUID and name.
type OpportunityProfile struct {
	// MarketoGUID is the provider opportunity identifier.
	MarketoGUID string `json:"marketoGUID"`
	// Name is the opportunity display name.
	Name string `json:"name"`
}

// CreateOpportunityInput creates an opportunity under an external identifier.
type CreateOpportunityInput struct {
	// ExternalOpportunityID is a stable, unique key in the source system.
	ExternalOpportunityID string `json:"externalOpportunityId"`
	// Name is the display name to create.
	Name string `json:"name"`
}

// UpsertOpportunityInput replaces an opportunity's name by external identifier.
type UpsertOpportunityInput struct {
	// ExternalOpportunityID is the stable deduplication key.
	ExternalOpportunityID string `json:"externalOpportunityId"`
	// Name is the display name to create or replace.
	Name string `json:"name"`
}

// OpportunityWriteResult identifies a created or updated opportunity.
type OpportunityWriteResult struct {
	// MarketoGUID is the provider opportunity identifier.
	MarketoGUID string `json:"marketoGUID"`
	// Status is created or updated.
	Status string `json:"status"`
}

// DeleteOpportunityInput identifies an opportunity to remove by external key.
type DeleteOpportunityInput struct {
	// ExternalOpportunityID is the stable deduplication key.
	ExternalOpportunityID string `json:"externalOpportunityId"`
}

// DeletedOpportunity confirms the target opportunity is absent.
type DeletedOpportunity struct {
	// Deleted is true for both removed and already-absent opportunities.
	Deleted bool `json:"deleted"`
}

// GetOpportunityByExternalIDOperation reads one opportunity by external ID.
type GetOpportunityByExternalIDOperation struct{ client *Client }

// GetOpportunityByExternalID binds the Query to this client.
func (client *Client) GetOpportunityByExternalID() GetOpportunityByExternalIDOperation {
	return GetOpportunityByExternalIDOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetOpportunityByExternalIDOperation) Definition() sdkgo.QueryDefinition {
	return GetOpportunityByExternalIDDefinition
}

// Invoke reads only the selected opportunity ID and name.
func (operation GetOpportunityByExternalIDOperation) Invoke(call sdkgo.Call, input GetOpportunityByExternalIDInput) sdkgo.QueryAttempt[OpportunityProfile] {
	if !validExternalOpportunityID(input.ExternalOpportunityID) {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchDefect, sdkgo.FailureValidation, "external opportunity ID is invalid")
	}
	query := url.Values{"filterType": {"externalOpportunityId"}, "filterValues": {input.ExternalOpportunityID}, "fields": {"marketoGUID,name"}}
	response := operation.client.requestLeadAPI(call, http.MethodGet, "/v1/opportunities.json?"+query.Encode(), nil)
	if retryKind, message, isRetry := classifyOpportunityTransport(response); isRetry {
		return opportunityQueryRetry(retryKind, message, response.retryAfter)
	}
	if errors.Is(response.err, errLeadReauthorization) {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if response.status != http.StatusOK {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity lookup")
	}
	if response.err != nil {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo opportunity response was incomplete")
	}
	var envelope struct {
		Success *bool                 `json:"success"`
		Result  *[]OpportunityProfile `json:"result"`
		Errors  []opportunityError    `json:"errors"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Success == nil {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid opportunity JSON")
	}
	if !*envelope.Success {
		if kind, retry := classifyOpportunityCode(firstOpportunityErrorCode(envelope.Errors)); retry {
			return opportunityQueryRetry(kind, "Marketo temporarily blocked the opportunity lookup", response.retryAfter)
		}
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity lookup")
	}
	if envelope.Result == nil || len(*envelope.Result) > 1 {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an invalid opportunity result")
	}
	if len(*envelope.Result) == 0 {
		return sdkgo.NewQueryBranch(GetOpportunityByExternalIDBranchNotFound, OpportunityProfile{}, nil, sdkgo.Receipt{})
	}
	profile := (*envelope.Result)[0]
	if profile.MarketoGUID == "" {
		return opportunityQueryBranch(GetOpportunityByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an opportunity without GUID")
	}
	return sdkgo.NewQueryBranch(GetOpportunityByExternalIDBranchFound, profile, nil, sdkgo.Receipt{})
}

// UpsertOpportunityOperation creates or replaces one opportunity by external ID.
type UpsertOpportunityOperation struct{ client *Client }

// UpsertOpportunity binds the Mutation to this client.
func (client *Client) UpsertOpportunity() UpsertOpportunityOperation {
	return UpsertOpportunityOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (UpsertOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return UpsertOpportunityDefinition
}

// IdempotencyKey correlates this absolute replacement with its Dex call.
func (UpsertOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the opportunity name under the same external key on each attempt.
func (operation UpsertOpportunityOperation) Invoke(call sdkgo.Call, input UpsertOpportunityInput) sdkgo.MutationAttempt[OpportunityWriteResult] {
	if !validExternalOpportunityID(input.ExternalOpportunityID) || !validOpportunityName(input.Name) {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchDefect, sdkgo.FailureValidation, "external opportunity ID or name is invalid")
	}
	body, err := encodeOpportunityBody("createOrUpdate", input.ExternalOpportunityID, input.Name)
	if err != nil {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity upsert could not be encoded")
	}
	response := operation.client.requestLeadAPI(call, http.MethodPost, "/v1/opportunities.json", body)
	if kind, message, retry := classifyOpportunityTransport(response); retry {
		return opportunityWriteRetry("upsertOpportunity", kind, message, response.retryAfter)
	}
	if errors.Is(response.err, errLeadReauthorization) {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if response.status != http.StatusOK {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity upsert")
	}
	if response.err != nil {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo opportunity response was incomplete")
	}
	result, outcome := decodeOpportunityResult(response.body)
	if kind, retry := classifyOpportunityCode(outcome); retry {
		return opportunityWriteRetry("upsertOpportunity", kind, "Marketo temporarily blocked the opportunity upsert", response.retryAfter)
	}
	if outcome == "invalid" || outcome == "" && (result.MarketoGUID == "" || result.Status != "created" && result.Status != "updated") {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid opportunity upsert JSON")
	}
	if outcome != "" {
		return opportunityWriteBranch("upsertOpportunity", UpsertOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity upsert")
	}
	value := OpportunityWriteResult{MarketoGUID: result.MarketoGUID, Status: result.Status}
	return sdkgo.NewMutationBranch(UpsertOpportunityBranchSynced, value, nil, opportunityReceipt(call, result.MarketoGUID))
}

// DeleteOpportunityOperation removes one opportunity by external ID.
type DeleteOpportunityOperation struct{ client *Client }

// DeleteOpportunity binds the Mutation to this client.
func (client *Client) DeleteOpportunity() DeleteOpportunityOperation {
	return DeleteOpportunityOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (DeleteOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return DeleteOpportunityDefinition
}

// IdempotencyKey correlates a convergent delete with its Dex call.
func (DeleteOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the opportunity and treats a missing opportunity as the target state.
func (operation DeleteOpportunityOperation) Invoke(call sdkgo.Call, input DeleteOpportunityInput) sdkgo.MutationAttempt[DeletedOpportunity] {
	if !validExternalOpportunityID(input.ExternalOpportunityID) {
		return opportunityDeleteBranch(DeleteOpportunityBranchDefect, sdkgo.FailureValidation, "external opportunity ID is invalid")
	}
	body, err := json.Marshal(map[string]any{"deleteBy": "dedupeFields", "input": []any{map[string]string{"externalOpportunityId": input.ExternalOpportunityID}}})
	if err != nil {
		return opportunityDeleteBranch(DeleteOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity delete could not be encoded")
	}
	response := operation.client.requestLeadAPI(call, http.MethodPost, "/v1/opportunities/delete.json", body)
	if kind, message, retry := classifyOpportunityTransport(response); retry {
		return opportunityDeleteRetry(kind, message, response.retryAfter)
	}
	if errors.Is(response.err, errLeadReauthorization) {
		return opportunityDeleteBranch(DeleteOpportunityBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if response.status != http.StatusOK {
		return opportunityDeleteBranch(DeleteOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity deletion")
	}
	if response.err != nil {
		return opportunityDeleteBranch(DeleteOpportunityBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo opportunity response was incomplete")
	}
	result, outcome := decodeOpportunityResult(response.body)
	if kind, retry := classifyOpportunityCode(outcome); retry {
		return opportunityDeleteRetry(kind, "Marketo temporarily blocked the opportunity deletion", response.retryAfter)
	}
	if outcome == "invalid" {
		return opportunityDeleteBranch(DeleteOpportunityBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid opportunity delete JSON")
	}
	if outcome == "1013" || outcome == "" && result.Status == "deleted" {
		return sdkgo.NewMutationBranch(DeleteOpportunityBranchDeleted, DeletedOpportunity{Deleted: true}, nil, opportunityReceipt(call, result.MarketoGUID))
	}
	return opportunityDeleteBranch(DeleteOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity deletion")
}

type opportunityError struct {
	Code string `json:"code"`
}

type opportunityResult struct {
	MarketoGUID string             `json:"marketoGUID"`
	Status      string             `json:"status"`
	Reasons     []opportunityError `json:"reasons"`
}

func validExternalOpportunityID(identifier string) bool {
	return identifier != "" && len(identifier) <= 50 && strings.TrimSpace(identifier) == identifier && isASCII(identifier)
}

func validOpportunityName(name string) bool {
	return name != "" && len(name) <= 255 && strings.TrimSpace(name) == name
}

func encodeOpportunityBody(action, identifier, name string) ([]byte, error) {
	return json.Marshal(map[string]any{"action": action, "dedupeBy": "dedupeFields", "input": []any{map[string]string{"externalOpportunityId": identifier, "name": name}}})
}

func decodeOpportunityResult(body []byte) (opportunityResult, string) {
	var envelope struct {
		Success *bool                `json:"success"`
		Result  *[]opportunityResult `json:"result"`
		Errors  []opportunityError   `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Success == nil {
		return opportunityResult{}, "invalid"
	}
	if !*envelope.Success {
		if len(envelope.Errors) == 0 {
			return opportunityResult{}, "invalid"
		}
		return opportunityResult{}, envelope.Errors[0].Code
	}
	if envelope.Result == nil || len(*envelope.Result) != 1 {
		return opportunityResult{}, "invalid"
	}
	result := (*envelope.Result)[0]
	if result.Status == "skipped" {
		if len(result.Reasons) == 0 {
			return opportunityResult{}, "invalid"
		}
		return result, result.Reasons[0].Code
	}
	return result, ""
}

func firstOpportunityErrorCode(errors []opportunityError) string {
	if len(errors) == 0 {
		return ""
	}
	return errors[0].Code
}

func classifyOpportunityCode(code string) (sdkgo.FailureKind, bool) {
	switch code {
	case "602", "606", "607", "615":
		return sdkgo.FailureRateLimit, true
	case "604", "608", "611", "614", "500", "502":
		return sdkgo.FailureAvailability, true
	default:
		return "", false
	}
}

func classifyOpportunityTransport(response leadResponse) (sdkgo.FailureKind, string, bool) {
	if errors.Is(response.err, errLeadCredentials) || errors.Is(response.err, errLeadTransport) {
		return sdkgo.FailureTransport, "Marketo request or credential refresh failed", true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit, "Marketo rate limited the opportunity request", true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.FailureAvailability, "Marketo opportunity request is temporarily unavailable", true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.FailureResponseTooLarge, "Marketo opportunity response exceeded the configured limit", false
	}
	return "", "", false
}

func opportunityReceipt(call sdkgo.Call, opportunityGUID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-marketo", ProviderObjectID: opportunityGUID, ObservedAt: time.Now().UTC()}
}

func opportunityQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OpportunityProfile] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "getOpportunityByExternalID", Message: message}
	return sdkgo.NewQueryBranch(branch, OpportunityProfile{}, &failure, sdkgo.Receipt{})
}

func opportunityQueryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[OpportunityProfile] {
	return sdkgo.NewQueryRetry[OpportunityProfile](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "getOpportunityByExternalID", Message: message}, delay)
}

func opportunityWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[OpportunityWriteResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, OpportunityWriteResult{}, &failure, sdkgo.Receipt{})
}

func opportunityWriteRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[OpportunityWriteResult] {
	return sdkgo.NewMutationRetry[OpportunityWriteResult](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: operation, Message: message}, delay)
}

func opportunityDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedOpportunity] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "deleteOpportunity", Message: message}
	return sdkgo.NewMutationBranch(branch, DeletedOpportunity{}, &failure, sdkgo.Receipt{})
}

func opportunityDeleteRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedOpportunity] {
	return sdkgo.NewMutationRetry[DeletedOpportunity](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "deleteOpportunity", Message: message}, delay)
}
