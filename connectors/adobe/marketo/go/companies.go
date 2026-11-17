// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetCompanyByExternalIDInput identifies one company by its stable external key.
type GetCompanyByExternalIDInput struct {
	// ExternalCompanyID is the key supplied when the company was created.
	ExternalCompanyID string `json:"externalCompanyId"`
}

// CompanyProfile contains only the provider ID and display name.
type CompanyProfile struct {
	// ID is the Marketo company ID.
	ID int64 `json:"id"`
	// Company is the company display name.
	Company string `json:"company"`
}

// CreateCompanyInput creates a company under an external identifier.
type CreateCompanyInput struct {
	// ExternalCompanyID is a stable, unique key in the source system.
	ExternalCompanyID string `json:"externalCompanyId"`
	// Company is the display name to create.
	Company string `json:"company"`
}

// UpsertCompanyInput replaces a company's name by external identifier.
type UpsertCompanyInput struct {
	// ExternalCompanyID is the stable deduplication key.
	ExternalCompanyID string `json:"externalCompanyId"`
	// Company is the display name to create or replace.
	Company string `json:"company"`
}

// CompanyWriteResult identifies a created or updated company.
type CompanyWriteResult struct {
	// ID is the Marketo company ID.
	ID int64 `json:"id"`
	// Status is created or updated.
	Status string `json:"status"`
}

// DeleteCompanyInput identifies a company to remove by external key.
type DeleteCompanyInput struct {
	// ExternalCompanyID is the stable deduplication key.
	ExternalCompanyID string `json:"externalCompanyId"`
}

// DeletedCompany confirms the target company is absent.
type DeletedCompany struct {
	// Deleted is true for both removed and already-absent companies.
	Deleted bool `json:"deleted"`
}

// GetCompanyByExternalIDOperation reads one company by external ID.
type GetCompanyByExternalIDOperation struct{ client *Client }

// GetCompanyByExternalID binds the Query to this client.
func (client *Client) GetCompanyByExternalID() GetCompanyByExternalIDOperation {
	return GetCompanyByExternalIDOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetCompanyByExternalIDOperation) Definition() sdkgo.QueryDefinition {
	return GetCompanyByExternalIDDefinition
}

// Invoke reads only the selected company ID and name.
func (operation GetCompanyByExternalIDOperation) Invoke(call sdkgo.Call, input GetCompanyByExternalIDInput) sdkgo.QueryAttempt[CompanyProfile] {
	if !validExternalCompanyID(input.ExternalCompanyID) {
		return companyQueryBranch(GetCompanyByExternalIDBranchDefect, sdkgo.FailureValidation, "external company ID is invalid")
	}
	query := url.Values{"filterType": {"externalCompanyId"}, "filterValues": {input.ExternalCompanyID}, "fields": {"id,company"}}
	response := operation.client.requestLeadAPI(call, http.MethodGet, "/v1/companies.json?"+query.Encode(), nil)
	if retryKind, message, isRetry := classifyCompanyTransport(response); isRetry {
		return companyQueryRetry(retryKind, message, response.retryAfter)
	}
	if errors.Is(response.err, errLeadReauthorization) {
		return companyQueryBranch(GetCompanyByExternalIDBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if response.status != http.StatusOK {
		return companyQueryBranch(GetCompanyByExternalIDBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company lookup")
	}
	if response.err != nil {
		return companyQueryBranch(GetCompanyByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo company response was incomplete")
	}
	var envelope struct {
		Success *bool             `json:"success"`
		Result  *[]CompanyProfile `json:"result"`
		Errors  []companyError    `json:"errors"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Success == nil {
		return companyQueryBranch(GetCompanyByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid company JSON")
	}
	if !*envelope.Success {
		if kind, retry := classifyCompanyCode(firstCompanyErrorCode(envelope.Errors)); retry {
			return companyQueryRetry(kind, "Marketo temporarily blocked the company lookup", response.retryAfter)
		}
		return companyQueryBranch(GetCompanyByExternalIDBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company lookup")
	}
	if envelope.Result == nil || len(*envelope.Result) > 1 {
		return companyQueryBranch(GetCompanyByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an invalid company result")
	}
	if len(*envelope.Result) == 0 {
		return sdkgo.NewQueryBranch(GetCompanyByExternalIDBranchNotFound, CompanyProfile{}, nil, sdkgo.Receipt{})
	}
	profile := (*envelope.Result)[0]
	if profile.ID <= 0 {
		return companyQueryBranch(GetCompanyByExternalIDBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned a company without ID")
	}
	return sdkgo.NewQueryBranch(GetCompanyByExternalIDBranchFound, profile, nil, sdkgo.Receipt{})
}

// UpsertCompanyOperation creates or replaces one company by external ID.
type UpsertCompanyOperation struct{ client *Client }

// UpsertCompany binds the Mutation to this client.
func (client *Client) UpsertCompany() UpsertCompanyOperation { return UpsertCompanyOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (UpsertCompanyOperation) Definition() sdkgo.MutationDefinition { return UpsertCompanyDefinition }

// IdempotencyKey correlates this absolute replacement with its Dex call.
func (UpsertCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the company name under the same external key on each attempt.
func (operation UpsertCompanyOperation) Invoke(call sdkgo.Call, input UpsertCompanyInput) sdkgo.MutationAttempt[CompanyWriteResult] {
	if !validExternalCompanyID(input.ExternalCompanyID) || !validCompanyName(input.Company) {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchDefect, sdkgo.FailureValidation, "external company ID or name is invalid")
	}
	body, err := encodeCompanyBody("createOrUpdate", input.ExternalCompanyID, input.Company)
	if err != nil {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchDefect, sdkgo.FailureLocalDefect, "company upsert could not be encoded")
	}
	response := operation.client.requestLeadAPI(call, http.MethodPost, "/v1/companies.json", body)
	if kind, message, retry := classifyCompanyTransport(response); retry {
		return companyWriteRetry("upsertCompany", kind, message, response.retryAfter)
	}
	if errors.Is(response.err, errLeadReauthorization) {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if response.status != http.StatusOK {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company upsert")
	}
	if response.err != nil {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo company response was incomplete")
	}
	result, outcome := decodeCompanyResult(response.body)
	if kind, retry := classifyCompanyCode(outcome); retry {
		return companyWriteRetry("upsertCompany", kind, "Marketo temporarily blocked the company upsert", response.retryAfter)
	}
	if outcome == "invalid" || outcome == "" && (result.ID <= 0 || result.Status != "created" && result.Status != "updated") {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid company upsert JSON")
	}
	if outcome != "" {
		return companyWriteBranch("upsertCompany", UpsertCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company upsert")
	}
	value := CompanyWriteResult{ID: result.ID, Status: result.Status}
	return sdkgo.NewMutationBranch(UpsertCompanyBranchSynced, value, nil, companyReceipt(call, result.ID))
}

// DeleteCompanyOperation removes one company by external ID.
type DeleteCompanyOperation struct{ client *Client }

// DeleteCompany binds the Mutation to this client.
func (client *Client) DeleteCompany() DeleteCompanyOperation { return DeleteCompanyOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (DeleteCompanyOperation) Definition() sdkgo.MutationDefinition { return DeleteCompanyDefinition }

// IdempotencyKey correlates a convergent delete with its Dex call.
func (DeleteCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the company and treats a missing company as the target state.
func (operation DeleteCompanyOperation) Invoke(call sdkgo.Call, input DeleteCompanyInput) sdkgo.MutationAttempt[DeletedCompany] {
	if !validExternalCompanyID(input.ExternalCompanyID) {
		return companyDeleteBranch(DeleteCompanyBranchDefect, sdkgo.FailureValidation, "external company ID is invalid")
	}
	body, err := json.Marshal(map[string]any{"deleteBy": "dedupeFields", "input": []any{map[string]string{"externalCompanyId": input.ExternalCompanyID}}})
	if err != nil {
		return companyDeleteBranch(DeleteCompanyBranchDefect, sdkgo.FailureLocalDefect, "company delete could not be encoded")
	}
	response := operation.client.requestLeadAPI(call, http.MethodPost, "/v1/companies/delete.json", body)
	if kind, message, retry := classifyCompanyTransport(response); retry {
		return companyDeleteRetry(kind, message, response.retryAfter)
	}
	if errors.Is(response.err, errLeadReauthorization) {
		return companyDeleteBranch(DeleteCompanyBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if response.status != http.StatusOK {
		return companyDeleteBranch(DeleteCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company deletion")
	}
	if response.err != nil {
		return companyDeleteBranch(DeleteCompanyBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo company response was incomplete")
	}
	result, outcome := decodeCompanyResult(response.body)
	if kind, retry := classifyCompanyCode(outcome); retry {
		return companyDeleteRetry(kind, "Marketo temporarily blocked the company deletion", response.retryAfter)
	}
	if outcome == "invalid" {
		return companyDeleteBranch(DeleteCompanyBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid company delete JSON")
	}
	if outcome == "1013" || outcome == "" && result.Status == "deleted" {
		return sdkgo.NewMutationBranch(DeleteCompanyBranchDeleted, DeletedCompany{Deleted: true}, nil, companyReceipt(call, result.ID))
	}
	return companyDeleteBranch(DeleteCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company deletion")
}

type companyError struct {
	Code string `json:"code"`
}

type companyResult struct {
	ID      int64          `json:"id"`
	Status  string         `json:"status"`
	Reasons []companyError `json:"reasons"`
}

func validExternalCompanyID(identifier string) bool {
	return identifier != "" && len(identifier) <= 255 && strings.TrimSpace(identifier) == identifier && isASCII(identifier)
}

func validCompanyName(name string) bool {
	return name != "" && len(name) <= 255 && strings.TrimSpace(name) == name
}

func encodeCompanyBody(action, identifier, name string) ([]byte, error) {
	return json.Marshal(map[string]any{"action": action, "dedupeBy": "dedupeFields", "input": []any{map[string]string{"externalCompanyId": identifier, "company": name}}})
}

func decodeCompanyResult(body []byte) (companyResult, string) {
	var envelope struct {
		Success *bool            `json:"success"`
		Result  *[]companyResult `json:"result"`
		Errors  []companyError   `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Success == nil {
		return companyResult{}, "invalid"
	}
	if !*envelope.Success {
		if len(envelope.Errors) == 0 {
			return companyResult{}, "invalid"
		}
		return companyResult{}, envelope.Errors[0].Code
	}
	if envelope.Result == nil || len(*envelope.Result) != 1 {
		return companyResult{}, "invalid"
	}
	result := (*envelope.Result)[0]
	if result.Status == "skipped" {
		if len(result.Reasons) == 0 {
			return companyResult{}, "invalid"
		}
		return result, result.Reasons[0].Code
	}
	return result, ""
}

func firstCompanyErrorCode(errors []companyError) string {
	if len(errors) == 0 {
		return ""
	}
	return errors[0].Code
}

func classifyCompanyCode(code string) (sdkgo.FailureKind, bool) {
	switch code {
	case "602", "606", "607", "615":
		return sdkgo.FailureRateLimit, true
	case "604", "608", "611", "614", "500", "502":
		return sdkgo.FailureAvailability, true
	default:
		return "", false
	}
}

func classifyCompanyTransport(response leadResponse) (sdkgo.FailureKind, string, bool) {
	if errors.Is(response.err, errLeadCredentials) || errors.Is(response.err, errLeadTransport) {
		return sdkgo.FailureTransport, "Marketo request or credential refresh failed", true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit, "Marketo rate limited the company request", true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.FailureAvailability, "Marketo company request is temporarily unavailable", true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.FailureResponseTooLarge, "Marketo company response exceeded the configured limit", false
	}
	return "", "", false
}

func companyReceipt(call sdkgo.Call, companyID int64) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-marketo", ProviderObjectID: strconv.FormatInt(companyID, 10), ObservedAt: time.Now().UTC()}
}

func companyQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CompanyProfile] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "getCompanyByExternalID", Message: message}
	return sdkgo.NewQueryBranch(branch, CompanyProfile{}, &failure, sdkgo.Receipt{})
}

func companyQueryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CompanyProfile] {
	return sdkgo.NewQueryRetry[CompanyProfile](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "getCompanyByExternalID", Message: message}, delay)
}

func companyWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CompanyWriteResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, CompanyWriteResult{}, &failure, sdkgo.Receipt{})
}

func companyWriteRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[CompanyWriteResult] {
	return sdkgo.NewMutationRetry[CompanyWriteResult](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: operation, Message: message}, delay)
}

func companyDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedCompany] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "deleteCompany", Message: message}
	return sdkgo.NewMutationBranch(branch, DeletedCompany{}, &failure, sdkgo.Receipt{})
}

func companyDeleteRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedCompany] {
	return sdkgo.NewMutationRetry[DeletedCompany](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "deleteCompany", Message: message}, delay)
}
