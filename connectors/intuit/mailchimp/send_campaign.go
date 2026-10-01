// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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

// Definition returns the immutable connector operation definition.
func (SendCampaignOperation) Definition() sdkgo.MutationDefinition { return SendCampaignDefinition }

// IdempotencyKey uses the stable connector Call ID. Mailchimp documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (SendCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ SendCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads GET /campaigns/{campaign_id}, then sends POST /campaigns/{campaign_id}/actions/send
// only when the campaign is a draft that sends immediately and, when ExpectedListID is set, targets
// that audience. A campaign already sending or sent selects alreadySent without a request.
//
// Before the send, Invoke records a Dex heartbeat checkpoint naming the Call ID. A later attempt of
// the same Step execution that finds it, after a lost Worker or an Execute timeout, never sends: it
// reads the campaign again and selects alreadySent when Mailchimp shows it sending or sent, and
// uncertain otherwise. Only a 429, a throttling 403, or a connection that never opened is retried
// after the send; a 5xx, a 408, or a lost response selects uncertain.
func (operation SendCampaignOperation) Invoke(call sdkgo.Call, input SendCampaignInput) sdkgo.MutationAttempt[SendCampaignOutput] {
	if err := validateSendCampaignInput(input); err != nil {
		return sdkgo.NewMutationBranch(SendCampaignBranchDefect, SendCampaignOutput{}, mailchimpFailurePointer(sdkgo.FailureValidation, sendCampaignOperation, err.Error()), sdkgo.Receipt{})
	}
	connection, failure := operation.client.resolveConnection(call, sendCampaignOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(SendCampaignBranchDefect, SendCampaignOutput{}, failure, sdkgo.Receipt{})
	}
	if hasEarlierDispatch(call) {
		return operation.reconcileEarlierDispatch(call, connection, input)
	}
	readResult := operation.client.exchange(call, connection, sendCampaignOperation, campaignReadRequest(input.CampaignID))
	receipt := operation.client.receipt(call, readResult.response, input.CampaignID)
	if attempt, isTerminal := campaignReadAttemptForExchange(readResult, receipt); isTerminal {
		return attempt
	}
	campaign, err := decodeCampaignBody(readResult.response.body, input.CampaignID)
	if err != nil {
		return sdkgo.NewMutationBranch(SendCampaignBranchInvalidResponse, SendCampaignOutput{}, mailchimpFailurePointer(sdkgo.FailureProtocol, sendCampaignOperation,
			"Mailchimp returned an invalid campaign, so it was not sent: "+err.Error()), receipt)
	}
	output := SendCampaignOutput{Campaign: campaign}
	if campaign.Status.IsSendingOrSent() {
		return sdkgo.NewMutationBranch(SendCampaignBranchAlreadySent, output, nil, receipt)
	}
	if blocker := describeSendBlocker(campaign, input.ExpectedListID); blocker != "" {
		return sdkgo.NewMutationBranch(SendCampaignBranchNotSendable, output, mailchimpFailurePointer(sdkgo.FailureConflict, sendCampaignOperation,
			blocker+"; nothing was sent"), receipt)
	}
	if err := recordDispatch(call); err != nil {
		return sdkgo.NewMutationRetry[SendCampaignOutput](mailchimpFailure(sdkgo.FailureAvailability, sendCampaignOperation,
			"Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	}
	sendResult := operation.client.exchange(call, connection, sendCampaignOperation, mailchimpRequest{
		method: http.MethodPost, path: "/campaigns/" + input.CampaignID + "/actions/send",
	})
	receipt = operation.client.receipt(call, sendResult.response, input.CampaignID)
	switch sendResult.outcome {
	case exchangeSucceeded:
		return sdkgo.NewMutationBranch(SendCampaignBranchSent, output, nil, receipt)
	case exchangeRateLimited, exchangeNotSent:
		releaseDispatch(call)
		return sdkgo.NewMutationRetry[SendCampaignOutput](sendResult.failure, sendResult.retryAfter)
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(output, sendResult.failure, receipt)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(SendCampaignBranchNotFound, output, &sendResult.failure, receipt)
	case exchangeDefect:
		releaseDispatch(call)
		return sdkgo.NewMutationBranch(SendCampaignBranchDefect, output, &sendResult.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(SendCampaignBranchProviderRejected, output, &sendResult.failure, receipt)
	}
}

// reconcileEarlierDispatch never sends; it reports alreadySent only when Mailchimp shows the campaign sending or sent.
func (operation SendCampaignOperation) reconcileEarlierDispatch(call sdkgo.Call, connection resolvedConnection, input SendCampaignInput) sdkgo.MutationAttempt[SendCampaignOutput] {
	readResult := operation.client.exchange(call, connection, sendCampaignOperation, campaignReadRequest(input.CampaignID))
	receipt := operation.client.receipt(call, readResult.response, input.CampaignID)
	if readResult.outcome == exchangeSucceeded {
		if campaign, err := decodeCampaignBody(readResult.response.body, input.CampaignID); err == nil {
			output := SendCampaignOutput{Campaign: campaign}
			if campaign.Status.IsSendingOrSent() {
				return sdkgo.NewMutationBranch(SendCampaignBranchAlreadySent, output, nil, receipt)
			}
			return sdkgo.NewMutationUncertain(output, mailchimpFailure(sdkgo.FailureTransport, sendCampaignOperation,
				"an earlier attempt of this Step may have sent the campaign and Mailchimp does not show it sending or sent, so it is not sent again"), receipt)
		}
	}
	return sdkgo.NewMutationUncertain(SendCampaignOutput{}, mailchimpFailure(sdkgo.FailureTransport, sendCampaignOperation,
		"an earlier attempt of this Step may have sent the campaign and its status could not be read, so it is not sent again"), receipt)
}

// campaignReadAttemptForExchange maps the read before a send; nothing has been sent, so unconfirmed outcomes are retried.
func campaignReadAttemptForExchange(result mailchimpExchange, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[SendCampaignOutput], bool) {
	return repeatableMutationAttemptForExchange[SendCampaignOutput](result, receipt, repeatableBranches{
		notFound: SendCampaignBranchNotFound, providerRejected: SendCampaignBranchProviderRejected,
		invalidResponse: SendCampaignBranchInvalidResponse, defect: SendCampaignBranchDefect,
	})
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
