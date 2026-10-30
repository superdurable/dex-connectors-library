// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	voidEnvelopeOperationID = "voidEnvelope"
	// maximumVoidedReasonLength is where DocuSign truncates a void reason.
	maximumVoidedReasonLength = 200
)

// voidRefusalCodes answer a void of an envelope whose status DocuSign does not void.
var voidRefusalCodes = []string{"ENVELOPE_CANNOT_VOID_INVALID_STATE", "ENVELOPE_INVALID_STATUS"}

// VoidEnvelopeInput names the envelope to void and why.
type VoidEnvelopeInput struct {
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// VoidedReason is shown to the recipients, 1 to 200 characters, such as "Signing deadline passed".
	VoidedReason string `json:"voidedReason"`
}

// VoidedEnvelope is the outcome of voidEnvelope.
type VoidedEnvelope struct {
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// WasAlreadyVoided is true when the envelope was voided before this call, such as by an earlier
	// attempt of the same Step, by its sender, or by expiry.
	WasAlreadyVoided bool `json:"wasAlreadyVoided,omitempty"`
	// Status is the envelope's status read back after DocuSign refused the void: voided, or on the
	// notVoidable branch completed, declined, or created. It is empty when this call voided it.
	Status EnvelopeStatus `json:"status,omitempty"`
	// VoidedReason is the reason DocuSign holds for an envelope that was already voided.
	VoidedReason string `json:"voidedReason,omitempty"`
}

// VoidEnvelopeOperation implements the voidEnvelope Mutation.
type VoidEnvelopeOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (VoidEnvelopeOperation) Definition() sdkgo.MutationDefinition { return VoidEnvelopeDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. DocuSign accepts no
// idempotency key; a repeated void is safe because an envelope that is already voided reads back as voided.
func (VoidEnvelopeOperation) IdempotencyKey(callID sdkgo.CallID, _ VoidEnvelopeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke voids the envelope. When DocuSign refuses because of the envelope's status, it reads the
// envelope back: voided selects voided with WasAlreadyVoided, and another final status selects notVoidable.
func (operation VoidEnvelopeOperation) Invoke(call sdkgo.Call, input VoidEnvelopeInput) sdkgo.MutationAttempt[VoidedEnvelope] {
	client := operation.client
	if err := validateVoidEnvelopeInput(input); err != nil {
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchDefect, VoidedEnvelope{}, docusignFailurePointer(voidEnvelopeOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	envelopeID := lowercaseGUID(input.EnvelopeID)
	session, sessionFailure := client.openSession(call, voidEnvelopeOperationID)
	if sessionFailure != nil {
		return mutationSessionFailure[VoidedEnvelope](sessionFailure, VoidEnvelopeBranchProviderRejected, VoidEnvelopeBranchInvalidResponse, VoidEnvelopeBranchDefect)
	}
	response, err := client.sendAPIRequest(call, &session, docusignRequest{
		method: http.MethodPut, path: "/envelopes/" + url.PathEscape(envelopeID),
		body: map[string]string{"status": string(EnvelopeStatusVoided), "voidedReason": input.VoidedReason},
	}, nil)
	switch {
	case err != nil && !errors.Is(err, errDocuSignResponseTooLarge):
		return sdkgo.NewMutationRetry[VoidedEnvelope](docusignFailure(voidEnvelopeOperationID, sdkgo.FailureTransport, "DocuSign could not be reached to void the envelope; voiding again is safe"), 0)
	case err == nil && response.statusCode != http.StatusOK:
		return client.classifyVoidRefusal(call, &session, envelopeID, response)
	}
	// The PUT succeeded; its summary body is not needed, so an oversized one is ignored.
	return sdkgo.NewMutationBranch(VoidEnvelopeBranchVoided, VoidedEnvelope{EnvelopeID: envelopeID}, nil, client.receipt(envelopeID, ""))
}

// classifyVoidRefusal reads the envelope back after a status refusal and classifies every other answer.
func (client *Client) classifyVoidRefusal(
	call sdkgo.Call, session *docusignSession, envelopeID string, response docusignResponse,
) sdkgo.MutationAttempt[VoidedEnvelope] {
	outcome := client.classifyDocuSignFailure(voidEnvelopeOperationID, response)
	receipt := client.receipt(envelopeID, outcome.errorCode)
	switch {
	case outcome.isRetry:
		return sdkgo.NewMutationRetry[VoidedEnvelope](outcome.failure, outcome.retryAfter)
	case outcome.isNotFound:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchNotFound, VoidedEnvelope{}, &outcome.failure, receipt)
	case !isDocuSignCodeIn(outcome.errorCode, voidRefusalCodes):
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchProviderRejected, VoidedEnvelope{}, &outcome.failure, receipt)
	}
	envelope, readOutcome, err := client.readEnvelope(call, session, voidEnvelopeOperationID, envelopeID)
	switch {
	case err != nil:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchInvalidResponse, VoidedEnvelope{}, docusignFailurePointer(voidEnvelopeOperationID, responseFailureKind(err), responseFailureMessage(err)), receipt)
	case readOutcome != nil && readOutcome.isRetry:
		return sdkgo.NewMutationRetry[VoidedEnvelope](readOutcome.failure, readOutcome.retryAfter)
	case readOutcome != nil && readOutcome.isNotFound:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchNotFound, VoidedEnvelope{}, &readOutcome.failure, client.receipt(envelopeID, readOutcome.errorCode))
	case readOutcome != nil:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchProviderRejected, VoidedEnvelope{}, &readOutcome.failure, client.receipt(envelopeID, readOutcome.errorCode))
	case envelope.Status == EnvelopeStatusVoided:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchVoided, VoidedEnvelope{
			EnvelopeID: envelopeID, WasAlreadyVoided: true, Status: envelope.Status, VoidedReason: envelope.VoidedReason,
		}, nil, receipt)
	case envelope.Status.IsTerminal() || envelope.Status == EnvelopeStatusCreated:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchNotVoidable, VoidedEnvelope{EnvelopeID: envelopeID, Status: envelope.Status},
			docusignFailurePointer(voidEnvelopeOperationID, sdkgo.FailureConflict, fmt.Sprintf("DocuSign does not void an envelope whose status is %s", envelope.Status)), receipt)
	default:
		return sdkgo.NewMutationBranch(VoidEnvelopeBranchProviderRejected, VoidedEnvelope{Status: envelope.Status}, &outcome.failure, receipt)
	}
}

func validateVoidEnvelopeInput(input VoidEnvelopeInput) error {
	if err := validateEnvelopeID(input.EnvelopeID); err != nil {
		return err
	}
	reason := strings.TrimSpace(input.VoidedReason)
	if reason == "" || utf8.RuneCountInString(input.VoidedReason) > maximumVoidedReasonLength || !utf8.ValidString(input.VoidedReason) {
		return fmt.Errorf("voidedReason must be 1 to %d characters of text", maximumVoidedReasonLength)
	}
	return nil
}

// mutationSessionFailure reports a call that never reached the eSignature API, so nothing changed.
func mutationSessionFailure[T any](failure *sessionFailure, rejected sdkgo.BranchID, invalidResponse sdkgo.BranchID, defect sdkgo.BranchID) sdkgo.MutationAttempt[T] {
	var zero T
	switch failure.kind {
	case sessionFailureRetry:
		return sdkgo.NewMutationRetry[T](failure.failure, failure.retryAfter)
	case sessionFailureRejected:
		return sdkgo.NewMutationBranch(rejected, zero, &failure.failure, sdkgo.Receipt{})
	case sessionFailureInvalidResponse:
		return sdkgo.NewMutationBranch(invalidResponse, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}
