// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateAgreementReminderInput selects participants in one agreement.
type CreateAgreementReminderInput struct {
	// AgreementID is the non-secret agreement identifier.
	AgreementID string `json:"agreementId"`
	// RecipientParticipantIDs lists one or more participant IDs from Acrobat Sign.
	RecipientParticipantIDs []string `json:"recipientParticipantIds"`
}

// CreatedAgreementReminder contains only the new reminder ID.
type CreatedAgreementReminder struct {
	// ReminderID is the provider-assigned reminder identifier.
	ReminderID string `json:"reminderId"`
}

// CreateAgreementReminderOperation sends one bounded reminder request.
type CreateAgreementReminderOperation struct{ client *Client }

type agreementReminderDispatch struct {
	client   *Client
	call     sdkgo.Call
	path     string
	body     []byte
	response providerResponse
}

// CreateAgreementReminder binds the Mutation to this client.
func (client *Client) CreateAgreementReminder() CreateAgreementReminderOperation {
	return CreateAgreementReminderOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateAgreementReminderOperation) Definition() sdkgo.MutationDefinition {
	return CreateAgreementReminderDefinition
}

// IdempotencyKey correlates bounded sends; Acrobat Sign does not receive this key.
func (CreateAgreementReminderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAgreementReminderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an immediate one-time reminder, allowing at most the configured send cap.
func (operation CreateAgreementReminderOperation) Invoke(call sdkgo.Call, input CreateAgreementReminderInput) sdkgo.MutationAttempt[CreatedAgreementReminder] {
	if !validAgreementID(input.AgreementID) || len(input.RecipientParticipantIDs) == 0 || len(input.RecipientParticipantIDs) > 50 {
		return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureValidation, "agreement or recipient participant IDs are invalid")
	}
	for _, participantID := range input.RecipientParticipantIDs {
		if !validAgreementID(participantID) || strings.TrimSpace(participantID) != participantID {
			return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureValidation, "recipient participant ID is invalid")
		}
	}
	body, err := json.Marshal(struct {
		RecipientParticipantIDs []string `json:"recipientParticipantIds"`
		Status                  string   `json:"status"`
	}{RecipientParticipantIDs: input.RecipientParticipantIDs, Status: "ACTIVE"})
	if err != nil {
		return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureLocalDefect, "reminder request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateAgreementReminderDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureLocalDefect, "reminder checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureLocalDefect, "reminder checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return reminderUncertain(call, "a prior reminder may have been created and reached the send cap")
	}
	dispatch := &agreementReminderDispatch{
		client: operation.client, call: call,
		path: "/agreements/" + url.PathEscape(input.AgreementID) + "/reminders", body: body,
	}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return reminderRetry(sdkgo.FailureAvailability, "reminder dispatch checkpoint is unavailable", 0)
	}
	response := dispatch.response
	if errors.Is(response.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return reminderRetry(sdkgo.FailureAvailability, "reminder retry checkpoint is unavailable", 0)
		}
		return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureAuthentication, "Acrobat Sign credentials are unavailable or invalid")
	}
	if response.isDiscovery && response.err == errInvalidAccessPoint {
		return reminderBranch(CreateAgreementReminderBranchDefect, sdkgo.FailureProtocol, "Acrobat Sign returned an unsafe API access point")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return reminderRetry(sdkgo.FailureAvailability, "reminder retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return reminderRetry(kind, "Acrobat Sign temporarily rejected the reminder", response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return reminderRetry(sdkgo.FailureAvailability, "Acrobat Sign reminder outcome is unknown", response.retryAfter)
	}
	if response.status < 200 || response.status >= 300 {
		if decision.MaximumPriorSends > 0 {
			return reminderUncertain(call, "an earlier reminder may have been created")
		}
		return reminderBranch(CreateAgreementReminderBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected the reminder")
	}
	if response.err != nil {
		return reminderUncertain(call, "Acrobat Sign reminder response could not be read completely")
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(response.body, &result) != nil || !validAgreementID(result.ID) {
		return reminderUncertain(call, "Acrobat Sign accepted the reminder but returned invalid metadata")
	}
	// The provider ID is already in the returned receipt if this best-effort checkpoint fails.
	_ = fence.RecordProviderID(result.ID)
	value := CreatedAgreementReminder{ReminderID: result.ID}
	return sdkgo.NewMutationBranch(CreateAgreementReminderBranchCreated, value, nil, reminderReceipt(call, result.ID))
}

func (dispatch *agreementReminderDispatch) execute(ctx context.Context) error {
	dispatch.response = dispatch.client.postWithContext(ctx, dispatch.call, dispatch.path, dispatch.body)
	return nil
}

func reminderReceipt(call sdkgo.Call, reminderID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-acrobat-sign", ProviderObjectID: reminderID, ObservedAt: time.Now().UTC()}
}

func reminderBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedAgreementReminder] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "createAgreementReminder", Message: message}
	return sdkgo.NewMutationBranch(branch, CreatedAgreementReminder{}, &failure, sdkgo.Receipt{})
}

func reminderRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[CreatedAgreementReminder] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "createAgreementReminder", Message: message}
	return sdkgo.NewMutationRetry[CreatedAgreementReminder](failure, delay)
}

func reminderUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[CreatedAgreementReminder] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "adobe-acrobat-sign", Operation: "createAgreementReminder", Message: message}
	return sdkgo.NewMutationUncertain(CreatedAgreementReminder{}, failure, reminderReceipt(call, ""))
}
