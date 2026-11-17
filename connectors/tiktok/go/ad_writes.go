// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateAdInput creates one regular single-video ad.
type CreateAdInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// AdgroupID is the parent ad group ID.
	AdgroupID string `json:"adgroupId"`
	// Name is the ad display name.
	Name string `json:"name"`
	// Text is the visible ad copy.
	Text string `json:"text"`
	// VideoID identifies a video uploaded to TikTok's creative library.
	VideoID string `json:"videoId"`
	// ImageID identifies the video's cover image.
	ImageID string `json:"imageId"`
	// IdentityID is the authorized TikTok identity.
	IdentityID string `json:"identityId"`
	// IdentityType is the provider identity type, such as CUSTOMIZED_USER.
	IdentityType string `json:"identityType"`
	// CallToAction is a provider CTA such as LEARN_MORE.
	CallToAction string `json:"callToAction"`
	// LandingPageURL is the public HTTPS destination without credentials.
	LandingPageURL string `json:"landingPageUrl"`
}

// UpdateAdInput patches one regular ad's name or text.
type UpdateAdInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// AdgroupID is the parent ad group ID.
	AdgroupID string `json:"adgroupId"`
	// AdID is the selected ad ID.
	AdID string `json:"adId"`
	// Name is an optional replacement display name.
	Name *string `json:"name,omitempty"`
	// Text is optional replacement ad copy.
	Text *string `json:"text,omitempty"`
}

// AdWriteResult identifies a confirmed ad write.
type AdWriteResult struct {
	// ID is the created or updated ad ID.
	ID string `json:"id"`
}

// CreateAdOperation creates one ad under a limited resend cap.
type CreateAdOperation struct{ client *Client }

// CreateAd binds the mutation to this client.
func (client *Client) CreateAd() CreateAdOperation {
	return CreateAdOperation{client}
}

// Definition returns the generated ad create contract.
func (CreateAdOperation) Definition() sdkgo.MutationDefinition { return CreateAdDefinition }

// IdempotencyKey derives stable Dex identity; TikTok does not deduplicate regular ad creates.
func (CreateAdOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAdInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends at most the configured number of creates and preserves uncertainty after an unconfirmed cap.
func (operation CreateAdOperation) Invoke(call sdkgo.Call, input CreateAdInput) sdkgo.MutationAttempt[AdWriteResult] {
	landingPageURL, err := url.Parse(input.LandingPageURL)
	if !validDecimalID(input.AdvertiserID) || !validDecimalID(input.AdgroupID) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Text) == "" || strings.TrimSpace(input.VideoID) == "" || strings.TrimSpace(input.ImageID) == "" || strings.TrimSpace(input.IdentityID) == "" || strings.TrimSpace(input.IdentityType) == "" || strings.TrimSpace(input.CallToAction) == "" || err != nil || landingPageURL.Scheme != "https" || landingPageURL.Host == "" || landingPageURL.User != nil || landingPageURL.RawQuery != "" || landingPageURL.Fragment != "" {
		return adWriteBranch(CreateAdBranchDefect, "createAd", sdkgo.FailureValidation, "ad create input invalid")
	}
	creative := map[string]any{"ad_name": input.Name, "ad_text": input.Text, "ad_format": "SINGLE_VIDEO", "video_id": input.VideoID, "image_ids": []string{input.ImageID}, "identity_id": input.IdentityID, "identity_type": input.IdentityType, "call_to_action": input.CallToAction, "landing_page_url": input.LandingPageURL}
	payload := map[string]any{"advertiser_id": input.AdvertiserID, "adgroup_id": input.AdgroupID, "creatives": []any{creative}}
	body, err := json.Marshal(payload)
	if err != nil {
		return adWriteBranch(CreateAdBranchDefect, "createAd", sdkgo.FailureLocalDefect, "ad input could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateAdDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return adWriteBranch(CreateAdBranchDefect, "createAd", sdkgo.FailureLocalDefect, "ad create checkpoint unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return adWriteBranch(CreateAdBranchDefect, "createAd", sdkgo.FailureLocalDefect, "ad create checkpoint invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainAdCreate(sdkgo.FailureAvailability, "ad create could not be confirmed")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postWithContext(call, requestContext, "/ad/create/", body)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", sdkgo.FailureTransport, "ad dispatch was not started"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
	}
	if errors.Is(response.err, errCredential) {
		return adWriteBranch(CreateAdBranchDefect, "createAd", sdkgo.FailureAuthentication, "TikTok credentials unavailable")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", sdkgo.FailureRateLimit, "TikTok rate limited ad create"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", sdkgo.FailureAvailability, "TikTok did not process ad create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return retryOrUncertainAdCreate(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "ad create outcome unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adWriteBranch(CreateAdBranchProviderRejected, "createAd", sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Code *int `json:"code"`
		Data struct {
			AdIDs []string `json:"ad_ids"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return retryOrUncertainAdCreate(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "ad create response invalid", 0)
	}
	if *envelope.Code == tiktokCodeRateLimited {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", sdkgo.FailureRateLimit, "TikTok rate limited ad create"), response.retryAfter)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return retryOrUncertainAdCreate(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "ad create outcome unknown", response.retryAfter)
	}
	if *envelope.Code != 0 {
		return adWriteBranch(CreateAdBranchProviderRejected, "createAd", sdkgo.FailureProviderRejection, "TikTok rejected ad create with code "+strconv.Itoa(*envelope.Code))
	}
	if len(envelope.Data.AdIDs) != 1 || !validDecimalID(envelope.Data.AdIDs[0]) {
		return retryOrUncertainAdCreate(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "ad ID was not confirmed", 0)
	}
	_ = fence.RecordProviderID(envelope.Data.AdIDs[0])
	return sdkgo.NewMutationBranch(CreateAdBranchCreated, AdWriteResult{ID: envelope.Data.AdIDs[0]}, nil, sdkgo.Receipt{})
}

// UpdateAdOperation patches an ad name or text.
type UpdateAdOperation struct{ client *Client }

// UpdateAd binds the mutation to this client.
func (client *Client) UpdateAd() UpdateAdOperation {
	return UpdateAdOperation{client}
}

// Definition returns the generated ad update contract.
func (UpdateAdOperation) Definition() sdkgo.MutationDefinition { return UpdateAdDefinition }

// IdempotencyKey derives stable Dex identity for an absolute ad update.
func (UpdateAdOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAdInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected ad settings and repeats unknown outcomes safely.
func (operation UpdateAdOperation) Invoke(call sdkgo.Call, input UpdateAdInput) sdkgo.MutationAttempt[AdWriteResult] {
	if !validDecimalID(input.AdvertiserID) || !validDecimalID(input.AdgroupID) || !validDecimalID(input.AdID) || (input.Name == nil && input.Text == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Text != nil && strings.TrimSpace(*input.Text) == "") {
		return adWriteBranch(UpdateAdBranchDefect, "updateAd", sdkgo.FailureValidation, "ad update input invalid")
	}
	creative := map[string]any{"ad_id": input.AdID}
	if input.Name != nil {
		creative["ad_name"] = *input.Name
	}
	if input.Text != nil {
		creative["ad_text"] = *input.Text
	}
	payload := map[string]any{"advertiser_id": input.AdvertiserID, "adgroup_id": input.AdgroupID, "creatives": []any{creative}, "patch_update": true}
	body, err := json.Marshal(payload)
	if err != nil {
		return adWriteBranch(UpdateAdBranchDefect, "updateAd", sdkgo.FailureLocalDefect, "ad update could not be encoded")
	}
	response := operation.client.post(call, "/ad/update/", body)
	if errors.Is(response.err, errCredential) {
		return adWriteBranch(UpdateAdBranchDefect, "updateAd", sdkgo.FailureAuthentication, "TikTok credentials unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("updateAd", sdkgo.FailureTransport, "TikTok request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("updateAd", sdkgo.FailureRateLimit, "TikTok rate limited update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("updateAd", sdkgo.FailureAvailability, "TikTok update outcome unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adWriteBranch(UpdateAdBranchProviderRejected, "updateAd", sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return adWriteBranch(UpdateAdBranchInvalidResponse, "updateAd", sdkgo.FailureResponseTooLarge, "TikTok response exceeded limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("updateAd", sdkgo.FailureTransport, "TikTok response interrupted"), 0)
	}
	var envelope struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return adWriteBranch(UpdateAdBranchInvalidResponse, "updateAd", sdkgo.FailureProtocol, "TikTok update JSON invalid")
	}
	if *envelope.Code == tiktokCodeRateLimited {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("updateAd", sdkgo.FailureRateLimit, "TikTok rate limited update"), response.retryAfter)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("updateAd", sdkgo.FailureAvailability, "TikTok update outcome unknown"), response.retryAfter)
	}
	if *envelope.Code != 0 {
		return adWriteBranch(UpdateAdBranchProviderRejected, "updateAd", sdkgo.FailureProviderRejection, "TikTok rejected update with code "+strconv.Itoa(*envelope.Code))
	}
	return sdkgo.NewMutationBranch(UpdateAdBranchUpdated, AdWriteResult{ID: input.AdID}, nil, sdkgo.Receipt{})
}

func adWriteFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}
}

func adWriteBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AdWriteResult] {
	failure := adWriteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, AdWriteResult{}, &failure, sdkgo.Receipt{})
}

func uncertainAdCreate(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AdWriteResult] {
	return sdkgo.NewMutationUncertain(AdWriteResult{}, adWriteFailure("createAd", kind, message), sdkgo.Receipt{})
}

func retryOrUncertainAdCreate(call sdkgo.Call, priorSends int32, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[AdWriteResult] {
	if priorSends+1 >= call.MaxSends() {
		return uncertainAdCreate(kind, message)
	}
	return sdkgo.NewMutationRetry[AdWriteResult](adWriteFailure("createAd", kind, message), retryAfter)
}
