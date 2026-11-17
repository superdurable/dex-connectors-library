// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// TriggerCampaignSendInput targets one existing user in an API-triggered campaign.
type TriggerCampaignSendInput struct {
	// CampaignID comes from a campaign configured for API-triggered delivery.
	CampaignID string `json:"campaignId"`
	// ExternalID is the existing user's external ID.
	ExternalID string `json:"externalId"`
}

// TriggerCanvasSendInput targets one existing user in an API-triggered Canvas.
type TriggerCanvasSendInput struct {
	// CanvasID comes from a Canvas configured for API-triggered delivery.
	CanvasID string `json:"canvasId"`
	// ExternalID is the existing user's external ID.
	ExternalID string `json:"externalId"`
}

// TargetedSendResult contains the Braze dispatch identifier.
type TargetedSendResult struct {
	// DispatchID identifies Braze's accepted transmission.
	DispatchID string `json:"dispatchId"`
}

// TriggerCampaignSendOperation dispatches one API-triggered campaign.
type TriggerCampaignSendOperation struct{ client *Client }

// TriggerCanvasSendOperation dispatches one API-triggered Canvas.
type TriggerCanvasSendOperation struct{ client *Client }

// TriggerCampaignSend binds a targeted campaign send to this client.
func (client *Client) TriggerCampaignSend() TriggerCampaignSendOperation {
	return TriggerCampaignSendOperation{client}
}

// TriggerCanvasSend binds a targeted Canvas send to this client.
func (client *Client) TriggerCanvasSend() TriggerCanvasSendOperation {
	return TriggerCanvasSendOperation{client}
}

// Definition returns the generated targeted campaign send contract.
func (TriggerCampaignSendOperation) Definition() sdkgo.MutationDefinition {
	return TriggerCampaignSendDefinition
}

// Definition returns the generated targeted Canvas send contract.
func (TriggerCanvasSendOperation) Definition() sdkgo.MutationDefinition {
	return TriggerCanvasSendDefinition
}

// IdempotencyKey preserves Dex's call identity across bounded sends.
func (TriggerCampaignSendOperation) IdempotencyKey(callID sdkgo.CallID, _ TriggerCampaignSendInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves Dex's call identity across bounded sends.
func (TriggerCanvasSendOperation) IdempotencyKey(callID sdkgo.CallID, _ TriggerCanvasSendInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke targets one existing user in a dashboard-configured campaign.
func (operation TriggerCampaignSendOperation) Invoke(call sdkgo.Call, input TriggerCampaignSendInput) sdkgo.MutationAttempt[TargetedSendResult] {
	const name = "triggerCampaignSend"
	if !validBrazeMessageID(input.CampaignID) || !validExternalID(input.ExternalID) {
		return brazeMutationBranch(TriggerCampaignSendBranchDefect, TargetedSendResult{}, name, sdkgo.FailureValidation, "campaign ID and existing external user ID are required")
	}
	payload, err := json.Marshal(struct {
		CampaignID string `json:"campaign_id"`
		Recipients []struct {
			ExternalUserID     string `json:"external_user_id"`
			SendToExistingOnly bool   `json:"send_to_existing_only"`
		} `json:"recipients"`
	}{input.CampaignID, []struct {
		ExternalUserID     string `json:"external_user_id"`
		SendToExistingOnly bool   `json:"send_to_existing_only"`
	}{{input.ExternalID, true}}})
	if err != nil {
		return brazeMutationBranch(TriggerCampaignSendBranchDefect, TargetedSendResult{}, name, sdkgo.FailureValidation, "campaign request is invalid")
	}
	return invokeBrazeTargetedSend(call, operation.client, name, "/campaigns/trigger/send", payload, TriggerCampaignSendDefinition.StepDefaults.ExecuteMethodTimeout, TriggerCampaignSendBranchSent, TriggerCampaignSendBranchProviderRejected, TriggerCampaignSendBranchDefect)
}

// Invoke targets one existing user in a dashboard-configured Canvas.
func (operation TriggerCanvasSendOperation) Invoke(call sdkgo.Call, input TriggerCanvasSendInput) sdkgo.MutationAttempt[TargetedSendResult] {
	const name = "triggerCanvasSend"
	if !validBrazeMessageID(input.CanvasID) || !validExternalID(input.ExternalID) {
		return brazeMutationBranch(TriggerCanvasSendBranchDefect, TargetedSendResult{}, name, sdkgo.FailureValidation, "Canvas ID and existing external user ID are required")
	}
	payload, err := json.Marshal(struct {
		CanvasID   string `json:"canvas_id"`
		Recipients []struct {
			ExternalUserID string `json:"external_user_id"`
		} `json:"recipients"`
	}{input.CanvasID, []struct {
		ExternalUserID string `json:"external_user_id"`
	}{{input.ExternalID}}})
	if err != nil {
		return brazeMutationBranch(TriggerCanvasSendBranchDefect, TargetedSendResult{}, name, sdkgo.FailureValidation, "Canvas request is invalid")
	}
	return invokeBrazeTargetedSend(call, operation.client, name, "/canvas/trigger/send", payload, TriggerCanvasSendDefinition.StepDefaults.ExecuteMethodTimeout, TriggerCanvasSendBranchSent, TriggerCanvasSendBranchProviderRejected, TriggerCanvasSendBranchDefect)
}

func invokeBrazeTargetedSend(call sdkgo.Call, client *Client, name, path string, payload []byte, timeout time.Duration, sent, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[TargetedSendResult] {
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return brazeMutationBranch(defect, TargetedSendResult{}, name, sdkgo.FailureLocalDefect, "send fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(TargetedSendResult{}, brazeFailure(name, sdkgo.FailureTransport, "an earlier message may have been dispatched"), sdkgo.Receipt{})
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.RESTAPIKey.Reveal()) {
		return brazeMutationBranch(defect, TargetedSendResult{}, name, sdkgo.FailureAuthentication, "Braze credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return brazeMutationBranch(defect, TargetedSendResult{}, name, sdkgo.FailureLocalDefect, "Braze request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.RESTAPIKey.Reveal())
	var response *http.Response
	var body []byte
	err = fence.Dispatch(func(ctx context.Context) error {
		var dispatchError error
		response, dispatchError = client.httpClient.Do(request.WithContext(ctx))
		if dispatchError != nil {
			return dispatchError
		}
		defer func() { _ = response.Body.Close() }()
		body, dispatchError = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
		return dispatchError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[TargetedSendResult](brazeFailure(name, sdkgo.FailureAvailability, "send dispatch was not recorded or sent"), 0)
	}
	if response != nil && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable) {
		if checkpointError := fence.RecordCanResend(); checkpointError != nil {
			return sdkgo.NewMutationRetry[TargetedSendResult](brazeFailure(name, sdkgo.FailureAvailability, "send retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[TargetedSendResult](brazeFailure(name, sdkgo.FailureRateLimit, "Braze asked to retry the send"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if err != nil || response == nil || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(TargetedSendResult{}, brazeFailure(name, sdkgo.FailureTransport, "send outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[TargetedSendResult](brazeFailure(name, sdkgo.FailureTransport, "send outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		return brazeMutationBranch(rejected, TargetedSendResult{}, name, sdkgo.FailureProviderRejection, "Braze rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	var acknowledgment struct {
		Message    string `json:"message"`
		DispatchID string `json:"dispatch_id"`
	}
	if json.Unmarshal(body, &acknowledgment) != nil || acknowledgment.Message != "success" || acknowledgment.DispatchID == "" {
		return sdkgo.NewMutationUncertain(TargetedSendResult{}, brazeFailure(name, sdkgo.FailureProtocol, "Braze accepted a send but its dispatch ID could not be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(sent, TargetedSendResult{DispatchID: acknowledgment.DispatchID}, nil, sdkgo.Receipt{})
}

func validBrazeMessageID(identifier string) bool {
	return strings.TrimSpace(identifier) == identifier && identifier != "" && len(identifier) <= 128
}
