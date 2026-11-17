// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const sendCampaignOperation = "sendCampaign"

// SendCampaignInput identifies one existing campaign to send now. Create and review the campaign in
// Mailchimp first; the connector never edits campaign content or recipients.
type SendCampaignInput struct {
	// CampaignID is Mailchimp's campaign ID, such as 42694e9e57.
	CampaignID string `json:"campaignId"`
	// ExpectedListID, when set, sends only when the campaign targets this audience; a campaign whose
	// audience was changed after it was approved selects notSendable. Empty skips the check.
	ExpectedListID string `json:"expectedListId,omitempty"`
}

// SendCampaignOutput is the campaign as the connector read it.
type SendCampaignOutput struct {
	// Campaign is the campaign Mailchimp returned in the read before the send, or in the read that
	// found it already sending or sent. After sent, its Status is still the draft status save.
	Campaign Campaign `json:"campaign"`
}

// SendCampaignOperation is the sendCampaign Mutation.
type SendCampaignOperation struct {
	client *Client
}

// sendCampaignAttempt is one fenced attempt of sendCampaign after the Fence decided it may send.
type sendCampaignAttempt struct {
	client     *Client
	call       sdkgo.Call
	input      SendCampaignInput
	fence      *singledispatch.Fence
	decision   singledispatch.Decision
	connection resolvedConnection
	// canResendUnknownOutcome is false when this attempt's send is the last one the cap allows.
	canResendUnknownOutcome bool
	// hasPossibleEarlierSend means a refusal or a changed campaign cannot prove nothing was sent.
	hasPossibleEarlierSend bool
}

// Definition returns the immutable connector operation definition.
func (SendCampaignOperation) Definition() sdkgo.MutationDefinition { return SendCampaignDefinition }

// IdempotencyKey uses the stable connector Call ID. Mailchimp documents no idempotency key, so the
// key only correlates the Receipt; the send count comes from a single-dispatch fence instead.
func (SendCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ SendCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads GET /campaigns/{campaign_id}, then sends POST /campaigns/{campaign_id}/actions/send
// only when the campaign is a draft that sends immediately and, when ExpectedListID is set, targets
// that audience. A campaign already sending or sent selects alreadySent without a send.
//
// The send runs under a single-dispatch fence capped by call.MaxSends (the manifest's maxSends of 3
// unless the application overrides it). A 429, a throttling 403, a 503, or a connection that never
// opened proves Mailchimp did not apply the send and is retried. Any other outcome after the send,
// such as a 5xx, a 408, a 409, or a lost response, is unknown: it is retried while another send is
// allowed, and the retry reads the campaign first, selecting alreadySent when it is sending or sent
// and sending again only when it is still a draft. At the cap the connector only reads the campaign
// and selects uncertain unless Mailchimp shows it sending or sent. A definite refusal after a
// possible earlier send reads the campaign the same way instead of selecting its refusal branch.
func (operation SendCampaignOperation) Invoke(call sdkgo.Call, input SendCampaignInput) sdkgo.MutationAttempt[SendCampaignOutput] {
	if err := validateSendCampaignInput(input); err != nil {
		return sdkgo.NewMutationBranch(SendCampaignBranchDefect, SendCampaignOutput{}, mailchimpFailurePointer(sdkgo.FailureValidation, sendCampaignOperation, err.Error()), sdkgo.Receipt{})
	}
	if hasLegacyDispatch(call) {
		return operation.client.readBackWithoutSend(call, input, "an earlier attempt of this Step may have sent the campaign")
	}
	fence, err := singledispatch.NewFence(call, SendCampaignDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(SendCampaignBranchDefect, SendCampaignOutput{}, mailchimpFailurePointer(sdkgo.FailureLocalDefect, sendCampaignOperation,
			"the Mailchimp dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[SendCampaignOutput](mailchimpFailure(sdkgo.FailureAvailability, sendCampaignOperation,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	case singledispatch.ReadBackRequired:
		return operation.client.readBackWithoutSend(call, input, "the send cap is reached")
	}
	attempt := &sendCampaignAttempt{
		client: operation.client, call: call, input: input, fence: fence, decision: decision,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		hasPossibleEarlierSend:  decision.MaximumPriorSends > 0,
	}
	return attempt.readThenSend()
}

// readThenSend reads the campaign, which is also the read-back of any earlier send, and sends a draft.
func (attempt *sendCampaignAttempt) readThenSend() sdkgo.MutationAttempt[SendCampaignOutput] {
	connection, failure := attempt.client.resolveConnection(attempt.call, sendCampaignOperation)
	if failure != nil {
		if attempt.hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(SendCampaignOutput{}, mailchimpFailure(sdkgo.FailureAuthentication, sendCampaignOperation,
				"an earlier attempt may have sent the campaign, and the connection credentials are unavailable to read it; reconnect the connection"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(SendCampaignBranchDefect, SendCampaignOutput{}, failure, sdkgo.Receipt{})
	}
	attempt.connection = connection
	readResult := attempt.client.exchange(attempt.call, connection, sendCampaignOperation, campaignReadRequest(attempt.input.CampaignID))
	receipt := attempt.client.receipt(attempt.call, readResult.response, attempt.input.CampaignID)
	if readResult.outcome == exchangeRateLimited || readResult.outcome == exchangeNotSent || readResult.outcome == exchangeUnavailable {
		return attempt.retryBeforeSend(readResult.failure, readResult.retryAfter)
	}
	if readResult.outcome != exchangeSucceeded {
		return attempt.refuseOrSelectUncertain(campaignReadBranch(readResult.outcome), SendCampaignOutput{}, readResult.failure, receipt)
	}
	campaign, err := decodeCampaignBody(readResult.response.body, attempt.input.CampaignID)
	if err != nil {
		return attempt.refuseOrSelectUncertain(SendCampaignBranchInvalidResponse, SendCampaignOutput{}, mailchimpFailure(sdkgo.FailureProtocol, sendCampaignOperation,
			"Mailchimp returned an invalid campaign, so it was not sent: "+err.Error()), receipt)
	}
	output := SendCampaignOutput{Campaign: campaign}
	if campaign.Status.IsSendingOrSent() {
		return sdkgo.NewMutationBranch(SendCampaignBranchAlreadySent, output, nil, receipt)
	}
	if blocker := describeSendBlocker(campaign, attempt.input.ExpectedListID); blocker != "" {
		// A campaign canceled or changed after a possible earlier send may have partly sent.
		return attempt.refuseOrSelectUncertain(SendCampaignBranchNotSendable, output, mailchimpFailure(sdkgo.FailureConflict, sendCampaignOperation,
			blocker+"; nothing was sent in this attempt"), receipt)
	}
	return attempt.send(output)
}

// send dispatches the send of the draft in output under the fence and maps the outcome.
func (attempt *sendCampaignAttempt) send(output SendCampaignOutput) sdkgo.MutationAttempt[SendCampaignOutput] {
	var sendResult mailchimpExchange
	err := attempt.fence.Dispatch(func(requestContext context.Context) error {
		sendResult = attempt.client.exchangeWithContext(requestContext, attempt.connection, sendCampaignOperation, mailchimpRequest{
			method: http.MethodPost, path: "/campaigns/" + attempt.input.CampaignID + "/actions/send", isFencedWrite: true,
		})
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) {
		return sdkgo.NewMutationRetry[SendCampaignOutput](mailchimpFailure(sdkgo.FailureAvailability, sendCampaignOperation,
			"Dex did not record the dispatch checkpoint; nothing was sent in this attempt"), 0)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[SendCampaignOutput](mailchimpFailure(sdkgo.FailureAvailability, sendCampaignOperation,
			"too little of the Step deadline remained to send; nothing was sent in this attempt"), 0)
	}
	receipt := attempt.client.receipt(attempt.call, sendResult.response, attempt.input.CampaignID)
	switch {
	case sendResult.outcome == exchangeSucceeded:
		return sdkgo.NewMutationBranch(SendCampaignBranchSent, output, nil, receipt)
	case isSendProvablyUnprocessed(sendResult):
		if checkpointErr := attempt.fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[SendCampaignOutput](mailchimpFailure(sdkgo.FailureAvailability, sendCampaignOperation,
				"the Mailchimp retry checkpoint could not be recorded"), 0)
		}
		return sdkgo.NewMutationRetry[SendCampaignOutput](sendResult.failure, sendResult.retryAfter)
	case sendResult.isOutcomeUnknownAfterDispatch():
		if attempt.canResendUnknownOutcome {
			// The claim stays the last marker, so the next attempt counts this send and reads first.
			return sdkgo.NewMutationRetry[SendCampaignOutput](sendResult.failure, sendResult.retryAfter)
		}
		return sdkgo.NewMutationUncertain(output, sendResult.failure, receipt)
	}
	if attempt.hasPossibleEarlierSend {
		// A refused resend usually means an earlier send landed, so the campaign is read instead.
		return attempt.client.readBackWithoutSend(attempt.call, attempt.input, "Mailchimp refused a resend after a possible earlier send")
	}
	// The refusal proves nothing was sent; a lost checkpoint only makes a replay read first.
	_ = attempt.fence.RecordCanResend()
	branch := SendCampaignBranchProviderRejected
	switch sendResult.outcome {
	case exchangeNotFound:
		branch = SendCampaignBranchNotFound
	case exchangeDefect:
		branch = SendCampaignBranchDefect
	}
	return sdkgo.NewMutationBranch(branch, output, &sendResult.failure, receipt)
}

// retryBeforeSend records notSent before a pre-send Retry so the next attempt does not count a send.
func (attempt *sendCampaignAttempt) retryBeforeSend(failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[SendCampaignOutput] {
	if attempt.decision.Kind == singledispatch.SendAllowed {
		// After ReadBackThenResendAllowed, notSent would hide the earlier possible send.
		if err := attempt.fence.RecordNotSent(); err != nil {
			failure = mailchimpFailure(sdkgo.FailureAvailability, sendCampaignOperation,
				"the no-send checkpoint could not be recorded; nothing was sent in this attempt")
			retryAfter = 0
		}
	}
	return sdkgo.NewMutationRetry[SendCampaignOutput](failure, retryAfter)
}

// refuseOrSelectUncertain selects branch only when no earlier attempt may have sent the campaign.
func (attempt *sendCampaignAttempt) refuseOrSelectUncertain(branch sdkgo.BranchID, output SendCampaignOutput, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[SendCampaignOutput] {
	if attempt.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(output, mailchimpFailure(failure.Kind, sendCampaignOperation,
			"an earlier attempt may have sent the campaign and Mailchimp does not show it sending or sent: "+failure.Message), receipt)
	}
	return sdkgo.NewMutationBranch(branch, output, &failure, receipt)
}

// readBackWithoutSend never sends; it reports alreadySent only when Mailchimp shows the campaign sending or sent.
func (client *Client) readBackWithoutSend(call sdkgo.Call, input SendCampaignInput, reason string) sdkgo.MutationAttempt[SendCampaignOutput] {
	connection, failure := client.resolveConnection(call, sendCampaignOperation)
	if failure != nil {
		return sdkgo.NewMutationUncertain(SendCampaignOutput{}, mailchimpFailure(sdkgo.FailureAuthentication, sendCampaignOperation,
			reason+", and the connection credentials are unavailable to read the campaign; reconnect the connection"), sdkgo.Receipt{})
	}
	readResult := client.exchange(call, connection, sendCampaignOperation, campaignReadRequest(input.CampaignID))
	receipt := client.receipt(call, readResult.response, input.CampaignID)
	switch readResult.outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		// Reading is harmless to repeat, and a retry of this path never sends.
		return sdkgo.NewMutationRetry[SendCampaignOutput](readResult.failure, readResult.retryAfter)
	case exchangeSucceeded:
		if campaign, err := decodeCampaignBody(readResult.response.body, input.CampaignID); err == nil {
			output := SendCampaignOutput{Campaign: campaign}
			if campaign.Status.IsSendingOrSent() {
				return sdkgo.NewMutationBranch(SendCampaignBranchAlreadySent, output, nil, receipt)
			}
			return sdkgo.NewMutationUncertain(output, mailchimpFailure(sdkgo.FailureTransport, sendCampaignOperation,
				reason+" and Mailchimp does not show it sending or sent, so it is not sent again"), receipt)
		}
	}
	return sdkgo.NewMutationUncertain(SendCampaignOutput{}, mailchimpFailure(sdkgo.FailureTransport, sendCampaignOperation,
		reason+" and its status could not be read, so it is not sent again"), receipt)
}

// isSendProvablyUnprocessed reports a 429, a throttling 403, a 503, or a connection that never opened.
func isSendProvablyUnprocessed(result mailchimpExchange) bool {
	return result.outcome == exchangeRateLimited || result.outcome == exchangeNotSent ||
		(result.outcome == exchangeUnavailable && result.response.statusCode == http.StatusServiceUnavailable)
}

// campaignReadBranch maps a definite read failure before a send; nothing was sent by this attempt.
func campaignReadBranch(outcome exchangeOutcome) sdkgo.BranchID {
	return repeatableBranches{
		notFound: SendCampaignBranchNotFound, providerRejected: SendCampaignBranchProviderRejected,
		invalidResponse: SendCampaignBranchInvalidResponse, defect: SendCampaignBranchDefect,
	}.branchFor(outcome)
}

func campaignReadRequest(campaignID string) mailchimpRequest {
	return mailchimpRequest{method: http.MethodGet, path: "/campaigns/" + campaignID, query: url.Values{"fields": {campaignReadFields}}}
}

func validateSendCampaignInput(input SendCampaignInput) error {
	if err := validateResourceID("campaignId", input.CampaignID); err != nil {
		return err
	}
	if input.ExpectedListID != "" {
		return validateResourceID("expectedListId", input.ExpectedListID)
	}
	return nil
}
