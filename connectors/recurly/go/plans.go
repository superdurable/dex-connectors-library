// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// PlanSummary contains plan identity and display name without pricing internals.
type PlanSummary struct {
	// ID is the Recurly plan ID.
	ID string `json:"id"`
	// Code is the stable merchant-facing plan code.
	Code string `json:"code"`
	// Name is the plan name shown to subscribers.
	Name string `json:"name"`
}

// PlanPage contains a bounded page of plans.
type PlanPage struct {
	// Plans contains safe plan identity fields.
	Plans []PlanSummary `json:"plans"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// DeletedPlan identifies a removed plan.
type DeletedPlan struct {
	// ID is the plan ID or code- prefixed code supplied for deletion.
	ID string `json:"id"`
}

// ListPlansInput selects 1 to 200 plans.
type ListPlansInput struct {
	// Limit is the requested page size from 1 to 200.
	Limit int `json:"limit"`
}

// GetPlanInput selects a plan by ID or code- prefixed code.
type GetPlanInput struct {
	// PlanID is the plan ID or code- prefixed code.
	PlanID string `json:"planId"`
}

// CreatePlanInput defines one fixed-price recurring plan.
type CreatePlanInput struct {
	// CodePrefix is a lowercase 1 to 12 character prefix for the call-derived unique code.
	CodePrefix string `json:"codePrefix"`
	// Name is the subscriber-facing name of at most 255 characters.
	Name string `json:"name"`
	// CurrencyCode is the uppercase ISO 4217 code.
	CurrencyCode string `json:"currencyCode"`
	// UnitAmount is the price in major currency units from 0 to 1000000.
	UnitAmount float64 `json:"unitAmount"`
	// IntervalUnit is days or months.
	IntervalUnit string `json:"intervalUnit"`
	// IntervalLength is the positive number of interval units.
	IntervalLength int `json:"intervalLength"`
}

// RenamePlanInput replaces the displayed name of one plan.
type RenamePlanInput struct {
	// PlanID is the plan ID or code- prefixed code.
	PlanID string `json:"planId"`
	// Name is the new subscriber-facing name.
	Name string `json:"name"`
}

// DeletePlanInput selects a plan for removal.
type DeletePlanInput struct {
	// PlanID is the plan ID or code- prefixed code.
	PlanID string `json:"planId"`
}

// ListPlansOperation lists plan identities.
type ListPlansOperation struct{ client *Client }

// ListPlans binds the Query to this client.
func (client *Client) ListPlans() ListPlansOperation { return ListPlansOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListPlansOperation) Definition() sdkgo.QueryDefinition { return ListPlansDefinition }

// Invoke lists plans without pricing internals.
func (operation ListPlansOperation) Invoke(call sdkgo.Call, input ListPlansInput) sdkgo.QueryAttempt[PlanPage] {
	if input.Limit < 1 || input.Limit > 200 {
		return planPageBranch(ListPlansBranchDefect, sdkgo.FailureValidation, "Recurly plan page size is invalid")
	}
	response := operation.client.get(call, "/plans?limit="+strconv.Itoa(input.Limit))
	if errors.Is(response.err, errCredential) {
		return planPageBranch(ListPlansBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return planPageRetry(sdkgo.FailureTransport, "Recurly plan list request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return planPageRetry(sdkgo.FailureRateLimit, "Recurly rate limited plan listing", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return planPageRetry(sdkgo.FailureAvailability, "Recurly plan listing is unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return planPageBranch(ListPlansBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return planPageBranch(ListPlansBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly plan list exceeded the response limit")
	}
	if response.err != nil {
		return planPageRetry(sdkgo.FailureTransport, "Recurly plan list was interrupted", 0)
	}
	var wire struct {
		Object  string            `json:"object"`
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Object != "list" || wire.Data == nil {
		return planPageBranch(ListPlansBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly plan list is invalid")
	}
	page := PlanPage{Plans: make([]PlanSummary, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, raw := range wire.Data {
		summary, valid := decodePlan(raw)
		if !valid {
			return planPageBranch(ListPlansBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly plan list item is invalid")
		}
		page.Plans = append(page.Plans, summary)
	}
	return sdkgo.NewQueryBranch(ListPlansBranchListed, page, nil, sdkgo.Receipt{})
}

// GetPlanOperation reads one plan identity.
type GetPlanOperation struct{ client *Client }

// GetPlan binds the Query to this client.
func (client *Client) GetPlan() GetPlanOperation { return GetPlanOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetPlanOperation) Definition() sdkgo.QueryDefinition { return GetPlanDefinition }

// Invoke reads one plan by ID or code.
func (operation GetPlanOperation) Invoke(call sdkgo.Call, input GetPlanInput) sdkgo.QueryAttempt[PlanSummary] {
	if !validAccountID(input.PlanID) {
		return getPlanBranch(GetPlanBranchDefect, sdkgo.FailureValidation, "Recurly plan ID is invalid")
	}
	response := operation.client.get(call, "/plans/"+url.PathEscape(input.PlanID))
	if errors.Is(response.err, errCredential) {
		return getPlanBranch(GetPlanBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return getPlanRetry(sdkgo.FailureTransport, "Recurly plan request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return getPlanRetry(sdkgo.FailureRateLimit, "Recurly rate limited plan lookup", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return getPlanRetry(sdkgo.FailureAvailability, "Recurly plan lookup is unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getPlanBranch(GetPlanBranchNotFound, sdkgo.FailureNotFound, "Recurly plan was not found")
	}
	if response.status != http.StatusOK {
		return getPlanBranch(GetPlanBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan lookup")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getPlanBranch(GetPlanBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly plan exceeded the response limit")
	}
	if response.err != nil {
		return getPlanRetry(sdkgo.FailureTransport, "Recurly plan response was interrupted", 0)
	}
	summary, valid := decodePlan(response.body)
	if !valid || !planMatchesID(summary, input.PlanID) {
		return getPlanBranch(GetPlanBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly plan response is invalid")
	}
	return sdkgo.NewQueryBranch(GetPlanBranchFound, summary, nil, sdkgo.Receipt{})
}

// CreatePlanOperation creates a fixed-price plan with a call-derived code.
type CreatePlanOperation struct{ client *Client }

// CreatePlan binds the Mutation to this client.
func (client *Client) CreatePlan() CreatePlanOperation { return CreatePlanOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreatePlanOperation) Definition() sdkgo.MutationDefinition { return CreatePlanDefinition }

// IdempotencyKey uses the Dex Call ID to make the plan code unique.
func (CreatePlanOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePlanInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var planCodePrefixPattern = regexp.MustCompile(`^[a-z0-9_-]{1,12}$`)

// Invoke creates a plan and confirms duplicate codes by read-back.
func (operation CreatePlanOperation) Invoke(call sdkgo.Call, input CreatePlanInput) sdkgo.MutationAttempt[PlanSummary] {
	if !planCodePrefixPattern.MatchString(input.CodePrefix) || len(input.Name) == 0 || len(input.Name) > 255 || strings.TrimSpace(input.Name) == "" || !validPlanCurrency(input.CurrencyCode) || math.IsNaN(input.UnitAmount) || math.IsInf(input.UnitAmount, 0) || input.UnitAmount < 0 || input.UnitAmount > 1000000 || (input.IntervalUnit != "days" && input.IntervalUnit != "months") || input.IntervalLength < 1 {
		return createPlanBranch(CreatePlanBranchDefect, sdkgo.FailureValidation, "Recurly plan input is invalid")
	}
	code := input.CodePrefix + "_" + strings.ReplaceAll(string(call.IdempotencyKey), "-", "")
	if len(code) > 50 || len(code) < 34 {
		return createPlanBranch(CreatePlanBranchDefect, sdkgo.FailureLocalDefect, "Recurly plan code could not be derived")
	}
	payload, _ := json.Marshal(map[string]any{"code": code, "name": input.Name, "pricing_model": "fixed", "currencies": []map[string]any{{"currency": input.CurrencyCode, "unit_amount": input.UnitAmount}}, "interval_unit": input.IntervalUnit, "interval_length": input.IntervalLength})
	response := operation.client.sendPlanRequest(call, http.MethodPost, "/plans", payload)
	if errors.Is(response.err, errCredential) {
		return createPlanBranch(CreatePlanBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return createPlanRetry(sdkgo.FailureTransport, "Recurly plan request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return createPlanRetry(sdkgo.FailureRateLimit, "Recurly rate limited plan creation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createPlanRetry(sdkgo.FailureAvailability, "Recurly plan outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusUnprocessableEntity {
		return operation.confirmCreatedPlan(call, code, input.Name)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return createPlanBranch(CreatePlanBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan creation")
	}
	if response.err != nil {
		return createPlanRetry(sdkgo.FailureTransport, "Recurly plan response was interrupted", 0)
	}
	summary, valid := decodePlan(response.body)
	if !valid || summary.Code != code || summary.Name != input.Name {
		return createPlanRetry(sdkgo.FailureProtocol, "Recurly plan may exist but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreatePlanBranchCreated, summary, nil, sdkgo.Receipt{})
}
func (operation CreatePlanOperation) confirmCreatedPlan(call sdkgo.Call, code, name string) sdkgo.MutationAttempt[PlanSummary] {
	response := operation.client.get(call, "/plans/"+url.PathEscape("code-"+code))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createPlanRetry(sdkgo.FailureAvailability, "Recurly plan code could not be checked", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return createPlanBranch(CreatePlanBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan creation")
	}
	if response.status != http.StatusOK {
		return createPlanBranch(CreatePlanBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan code lookup")
	}
	summary, valid := decodePlan(response.body)
	if !valid || summary.Code != code || summary.Name != name {
		return createPlanRetry(sdkgo.FailureProtocol, "Recurly plan code response is invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreatePlanBranchCreated, summary, nil, sdkgo.Receipt{})
}

// RenamePlanOperation replaces a plan's display name.
type RenamePlanOperation struct{ client *Client }

// RenamePlan binds the Mutation to this client.
func (client *Client) RenamePlan() RenamePlanOperation { return RenamePlanOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (RenamePlanOperation) Definition() sdkgo.MutationDefinition { return RenamePlanDefinition }

// IdempotencyKey records the stable Dex Call ID for this replacement.
func (RenamePlanOperation) IdempotencyKey(callID sdkgo.CallID, _ RenamePlanInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the plan name with an absolute value.
func (operation RenamePlanOperation) Invoke(call sdkgo.Call, input RenamePlanInput) sdkgo.MutationAttempt[PlanSummary] {
	if !validAccountID(input.PlanID) || len(input.Name) == 0 || len(input.Name) > 255 || strings.TrimSpace(input.Name) == "" {
		return renamePlanBranch(RenamePlanBranchDefect, sdkgo.FailureValidation, "Recurly plan ID or name is invalid")
	}
	payload, _ := json.Marshal(map[string]string{"name": input.Name})
	response := operation.client.sendPlanRequest(call, http.MethodPut, "/plans/"+url.PathEscape(input.PlanID), payload)
	if errors.Is(response.err, errCredential) {
		return renamePlanBranch(RenamePlanBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return renamePlanRetry(sdkgo.FailureTransport, "Recurly rename request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return renamePlanRetry(sdkgo.FailureRateLimit, "Recurly rate limited plan rename", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return renamePlanRetry(sdkgo.FailureAvailability, "Recurly plan rename outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return renamePlanBranch(RenamePlanBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan rename")
	}
	if response.err != nil {
		return renamePlanRetry(sdkgo.FailureTransport, "Recurly rename response was interrupted", 0)
	}
	summary, valid := decodePlan(response.body)
	if !valid || !planMatchesID(summary, input.PlanID) || summary.Name != input.Name {
		return renamePlanRetry(sdkgo.FailureProtocol, "Recurly plan may have been renamed but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(RenamePlanBranchRenamed, summary, nil, sdkgo.Receipt{})
}

// DeletePlanOperation removes a plan from future use.
type DeletePlanOperation struct{ client *Client }

// DeletePlan binds the Mutation to this client.
func (client *Client) DeletePlan() DeletePlanOperation { return DeletePlanOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (DeletePlanOperation) Definition() sdkgo.MutationDefinition { return DeletePlanDefinition }

// IdempotencyKey records the stable Dex Call ID for this deletion.
func (DeletePlanOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePlanInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a plan; a missing plan is already in the desired state.
func (operation DeletePlanOperation) Invoke(call sdkgo.Call, input DeletePlanInput) sdkgo.MutationAttempt[DeletedPlan] {
	if !validAccountID(input.PlanID) {
		return deletePlanBranch(DeletePlanBranchDefect, sdkgo.FailureValidation, "Recurly plan ID is invalid")
	}
	response := operation.client.sendPlanRequest(call, http.MethodDelete, "/plans/"+url.PathEscape(input.PlanID), nil)
	if errors.Is(response.err, errCredential) {
		return deletePlanBranch(DeletePlanBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return deletePlanRetry(sdkgo.FailureTransport, "Recurly delete request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return deletePlanRetry(sdkgo.FailureRateLimit, "Recurly rate limited plan deletion", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return deletePlanRetry(sdkgo.FailureAvailability, "Recurly plan deletion outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusNotFound || response.status == http.StatusOK || response.status == http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeletePlanBranchDeleted, DeletedPlan{ID: input.PlanID}, nil, sdkgo.Receipt{})
	}
	return deletePlanBranch(DeletePlanBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected plan deletion")
}

func (client *Client) sendPlanRequest(call sdkgo.Call, method, path string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	key := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(key) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/vnd.recurly.v2021-02-25+json")
	if method != http.MethodDelete {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(key+":")))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func validPlanCurrency(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, character := range code {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}
func decodePlan(body []byte) (PlanSummary, bool) {
	var wire struct {
		ID   string `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &wire) != nil || !validAccountID(wire.ID) || wire.Code == "" || wire.Name == "" {
		return PlanSummary{}, false
	}
	return PlanSummary{ID: wire.ID, Code: wire.Code, Name: wire.Name}, true
}
func planMatchesID(summary PlanSummary, id string) bool {
	if strings.HasPrefix(id, "code-") {
		return summary.Code == strings.TrimPrefix(id, "code-")
	}
	return summary.ID == id
}
func planFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: operation, Message: message}
}
func planPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PlanPage] {
	failure := planFailure("listPlans", kind, message)
	return sdkgo.NewQueryBranch(branch, PlanPage{}, &failure, sdkgo.Receipt{})
}
func planPageRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[PlanPage] {
	return sdkgo.NewQueryRetry[PlanPage](planFailure("listPlans", kind, message), after)
}
func getPlanBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PlanSummary] {
	failure := planFailure("getPlan", kind, message)
	return sdkgo.NewQueryBranch(branch, PlanSummary{}, &failure, sdkgo.Receipt{})
}
func getPlanRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[PlanSummary] {
	return sdkgo.NewQueryRetry[PlanSummary](planFailure("getPlan", kind, message), after)
}
func createPlanBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PlanSummary] {
	failure := planFailure("createPlan", kind, message)
	return sdkgo.NewMutationBranch(branch, PlanSummary{}, &failure, sdkgo.Receipt{})
}
func createPlanRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[PlanSummary] {
	return sdkgo.NewMutationRetry[PlanSummary](planFailure("createPlan", kind, message), after)
}
func renamePlanBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PlanSummary] {
	failure := planFailure("renamePlan", kind, message)
	return sdkgo.NewMutationBranch(branch, PlanSummary{}, &failure, sdkgo.Receipt{})
}
func renamePlanRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[PlanSummary] {
	return sdkgo.NewMutationRetry[PlanSummary](planFailure("renamePlan", kind, message), after)
}
func deletePlanBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedPlan] {
	failure := planFailure("deletePlan", kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedPlan{}, &failure, sdkgo.Receipt{})
}
func deletePlanRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[DeletedPlan] {
	return sdkgo.NewMutationRetry[DeletedPlan](planFailure("deletePlan", kind, message), after)
}
