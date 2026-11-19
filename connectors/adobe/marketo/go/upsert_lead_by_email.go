// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpsertLeadByEmailInput creates or updates one lead matched by email.
type UpsertLeadByEmailInput struct {
	// Email is the exact lookup address and is never returned in the result.
	Email string `json:"email"`
	// FirstName replaces the first name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the last name when supplied.
	LastName *string `json:"lastName,omitempty"`
}

// SyncedLead identifies a lead created or updated by Marketo.
type SyncedLead struct {
	// ID is the Marketo lead ID.
	ID int64 `json:"id"`
	// Status is created or updated.
	Status string `json:"status"`
}

// UpsertLeadByEmailOperation implements the email-keyed upsert Mutation.
type UpsertLeadByEmailOperation struct{ client *Client }

// UpsertLeadByEmail binds the Mutation to this client.
func (client *Client) UpsertLeadByEmail() UpsertLeadByEmailOperation {
	return UpsertLeadByEmailOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpsertLeadByEmailOperation) Definition() sdkgo.MutationDefinition {
	return UpsertLeadByEmailDefinition
}

// IdempotencyKey derives a stable call identity for the absolute upsert.
func (UpsertLeadByEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertLeadByEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one createOrUpdate call and classifies Marketo's record-level result.
func (operation UpsertLeadByEmailOperation) Invoke(call sdkgo.Call, input UpsertLeadByEmailInput) sdkgo.MutationAttempt[SyncedLead] {
	if !validLeadEmail(input.Email) || !isASCII(input.Email) || input.FirstName == nil && input.LastName == nil {
		return upsertLeadBranch(UpsertLeadByEmailBranchDefect, sdkgo.FailureValidation, "email or name fields are invalid")
	}
	body, err := encodeLeadSyncBody("createOrUpdate", input.Email, input.FirstName, input.LastName)
	if err != nil {
		return upsertLeadBranch(UpsertLeadByEmailBranchDefect, sdkgo.FailureLocalDefect, "lead upsert could not be encoded")
	}
	response := operation.client.requestLeadAPI(call, http.MethodPost, "/v1/leads.json", body)
	if errors.Is(response.err, errLeadReauthorization) {
		return upsertLeadBranch(UpsertLeadByEmailBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if errors.Is(response.err, errLeadCredentials) || errors.Is(response.err, errLeadTransport) {
		return upsertLeadRetry(sdkgo.FailureTransport, "Marketo request or credential refresh failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return upsertLeadRetry(sdkgo.FailureRateLimit, "Marketo rate limited the upsert", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return upsertLeadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return upsertLeadBranch(UpsertLeadByEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return upsertLeadBranch(UpsertLeadByEmailBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Marketo response exceeded the configured limit")
	}
	if response.err != nil {
		return upsertLeadRetry(sdkgo.FailureTransport, "Marketo response was interrupted", 0)
	}
	result, outcome := decodeLeadSyncResponse(response.body)
	if outcome != "" {
		switch outcome {
		case "invalid":
			return upsertLeadBranch(UpsertLeadByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid upsert JSON")
		case "retry-rate":
			return upsertLeadRetry(sdkgo.FailureRateLimit, "Marketo temporarily blocked the upsert", response.retryAfter)
		case "retry-availability":
			return upsertLeadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
		default:
			return upsertLeadBranch(UpsertLeadByEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the lead upsert")
		}
	}
	if result.Status != "created" && result.Status != "updated" || result.ID <= 0 {
		return upsertLeadBranch(UpsertLeadByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an unexpected upsert result")
	}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-marketo", ProviderObjectID: strconv.FormatInt(result.ID, 10), ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(UpsertLeadByEmailBranchSynced, SyncedLead{ID: result.ID, Status: result.Status}, nil, receipt)
}

type leadSyncResult struct {
	ID      int64  `json:"id"`
	Status  string `json:"status"`
	Reasons []struct {
		Code string `json:"code"`
	} `json:"reasons"`
}

func encodeLeadSyncBody(action, email string, firstName, lastName *string) ([]byte, error) {
	fields := map[string]any{"email": email}
	if firstName != nil {
		fields["firstName"] = *firstName
	}
	if lastName != nil {
		fields["lastName"] = *lastName
	}
	return json.Marshal(map[string]any{"action": action, "lookupField": "email", "input": []any{fields}})
}

func decodeLeadSyncResponse(body []byte) (leadSyncResult, string) {
	var envelope struct {
		Success *bool             `json:"success"`
		Result  *[]leadSyncResult `json:"result"`
		Errors  []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Success == nil {
		return leadSyncResult{}, "invalid"
	}
	if !*envelope.Success {
		if len(envelope.Errors) == 0 {
			return leadSyncResult{}, "invalid"
		}
		return leadSyncResult{}, classifyLeadSyncCode(envelope.Errors[0].Code)
	}
	if envelope.Result == nil || len(*envelope.Result) != 1 {
		return leadSyncResult{}, "invalid"
	}
	result := (*envelope.Result)[0]
	if result.Status == "skipped" {
		if len(result.Reasons) == 0 {
			return leadSyncResult{}, "invalid"
		}
		return result, classifyLeadSyncCode(result.Reasons[0].Code)
	}
	return result, ""
}

func classifyLeadSyncCode(code string) string {
	switch code {
	case "602", "606", "607", "615":
		return "retry-rate"
	case "604", "608", "611", "614", "500", "502":
		return "retry-availability"
	case "1005":
		return "already-exists"
	default:
		return "rejected"
	}
}

func upsertLeadFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "upsertLeadByEmail", Message: message}
}
func upsertLeadBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SyncedLead] {
	failure := upsertLeadFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, SyncedLead{}, &failure, sdkgo.Receipt{})
}
func upsertLeadRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[SyncedLead] {
	return sdkgo.NewMutationRetry[SyncedLead](upsertLeadFailure(kind, message), delay)
}
