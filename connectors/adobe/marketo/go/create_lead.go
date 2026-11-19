// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateLeadInput creates one lead using an email as Marketo's deduplication key.
type CreateLeadInput struct {
	// Email is the lead's exact ASCII email address and is omitted from results.
	Email string `json:"email"`
	// FirstName is the lead's first name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName is the lead's last name when supplied.
	LastName *string `json:"lastName,omitempty"`
}

// CreatedLead identifies a lead accepted by Marketo.
type CreatedLead struct {
	// ID is the Marketo lead ID.
	ID int64 `json:"id"`
	// Status is created.
	Status string `json:"status"`
}

// CreateLeadOperation implements the bounded createOnly Mutation.
type CreateLeadOperation struct{ client *Client }

type createLeadDispatch struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response leadResponse
}

// CreateLead binds the Mutation to this client.
func (client *Client) CreateLead() CreateLeadOperation { return CreateLeadOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateLeadOperation) Definition() sdkgo.MutationDefinition { return CreateLeadDefinition }

// IdempotencyKey correlates bounded sends; Marketo does not receive this key.
func (CreateLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates once by email and reads back before a bounded resend.
func (operation CreateLeadOperation) Invoke(call sdkgo.Call, input CreateLeadInput) sdkgo.MutationAttempt[CreatedLead] {
	if !validLeadEmail(input.Email) || !isASCII(input.Email) {
		return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureValidation, "email must be a valid ASCII address")
	}
	body, err := encodeLeadSyncBody("createOnly", input.Email, input.FirstName, input.LastName)
	if err != nil {
		return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead create could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateLeadDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createLeadRetry(sdkgo.FailureAvailability, "lead create checkpoint is unavailable", 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createLeadUncertain(call, "an earlier Marketo create may have succeeded and reached the send cap")
	}
	dispatch := &createLeadDispatch{client: operation.client, call: call, body: body}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return createLeadRetry(sdkgo.FailureAvailability, "lead create dispatch checkpoint is unavailable", 0)
	}
	response := dispatch.response
	if errors.Is(response.err, errLeadReauthorization) || errors.Is(response.err, errLeadCredentials) {
		if err := fence.RecordCanResend(); err != nil {
			return createLeadRetry(sdkgo.FailureAvailability, "lead create retry checkpoint is unavailable", 0)
		}
		if errors.Is(response.err, errLeadReauthorization) {
			return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
		}
		return createLeadRetry(sdkgo.FailureTransport, "Marketo credentials are unavailable", 0)
	}
	if errors.Is(response.err, errLeadTransport) {
		return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureTransport, "Marketo create outcome is unknown"), createLeadReceipt(call, 0))
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createLeadRetry(sdkgo.FailureAvailability, "lead create retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createLeadRetry(kind, "Marketo temporarily rejected the create", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureAvailability, "Marketo create outcome is unknown"), createLeadReceipt(call, 0))
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends == 0 && response.err == nil && isDefiniteMarketoRefusal(response.status, response.body) {
			if err := fence.RecordCanResend(); err != nil {
				return createLeadRetry(sdkgo.FailureAvailability, "lead refusal checkpoint is unavailable", 0)
			}
			return createLeadBranch(CreateLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected HTTP "+strconv.Itoa(response.status))
		}
		return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureAvailability, "Marketo create outcome is unknown"), createLeadReceipt(call, 0))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) || response.err != nil {
		return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureProtocol, "Marketo create response could not be read completely"), createLeadReceipt(call, 0))
	}
	result, outcome := decodeLeadSyncResponse(response.body)
	if outcome != "" {
		switch outcome {
		case "retry-rate":
			return createLeadRetry(sdkgo.FailureRateLimit, "Marketo temporarily blocked the create", response.retryAfter)
		case "retry-availability":
			return createLeadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
		case "already-exists":
			if decision.MaximumPriorSends > 0 {
				return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureTransport, "a prior Marketo create may have succeeded before the duplicate response"), createLeadReceipt(call, 0))
			}
			if err := fence.RecordCanResend(); err != nil {
				return createLeadRetry(sdkgo.FailureAvailability, "lead refusal checkpoint is unavailable", 0)
			}
			return createLeadBranch(CreateLeadBranchAlreadyExists, sdkgo.FailureProviderRejection, "Marketo already has a lead with this email")
		case "invalid":
			return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureProtocol, "Marketo create response was invalid"), createLeadReceipt(call, 0))
		default:
			return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureProtocol, "Marketo create response was contradictory"), createLeadReceipt(call, 0))
		}
	}
	if result.ID <= 0 || result.Status != "created" {
		return limitedMarketoUnknown[CreatedLead](call, decision, createLeadFailure(sdkgo.FailureProtocol, "Marketo create response was unexpected"), createLeadReceipt(call, 0))
	}
	_ = fence.RecordProviderID(strconv.FormatInt(result.ID, 10))
	created := CreatedLead{ID: result.ID, Status: result.Status}
	return sdkgo.NewMutationBranch(CreateLeadBranchCreated, created, nil, createLeadReceipt(call, created.ID))
}

func (dispatch *createLeadDispatch) execute(ctx context.Context) error {
	dispatch.response = dispatch.client.requestLeadAPIWithContext(ctx, dispatch.call, http.MethodPost, "/v1/leads.json", dispatch.body)
	return nil
}

func isASCII(value string) bool {
	for _, character := range value {
		if character > 127 {
			return false
		}
	}
	return true
}

func createLeadReceipt(call sdkgo.Call, leadID int64) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-marketo", ProviderObjectID: strconv.FormatInt(leadID, 10), ObservedAt: time.Now().UTC()}
}
func createLeadFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "createLead", Message: message}
}
func createLeadBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedLead] {
	failure := createLeadFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CreatedLead{}, &failure, sdkgo.Receipt{})
}
func createLeadRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[CreatedLead] {
	return sdkgo.NewMutationRetry[CreatedLead](createLeadFailure(kind, message), delay)
}
func createLeadUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[CreatedLead] {
	return sdkgo.NewMutationUncertain(CreatedLead{}, createLeadFailure(sdkgo.FailureTransport, message), createLeadReceipt(call, 0))
}
