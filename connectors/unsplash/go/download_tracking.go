// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unsplash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// TrackPhotoDownloadInput identifies a photo chosen for a download-like action.
type TrackPhotoDownloadInput struct {
	// ID is the public photo ID returned by Unsplash.
	ID string `json:"id"`
}

// TrackedPhotoDownload carries Unsplash's response URL for the chosen photo.
type TrackedPhotoDownload struct {
	// URL is the provider's image URL returned after recording the download.
	URL string `json:"url"`
}

// TrackPhotoDownloadOperation records an Unsplash photo download event.
type TrackPhotoDownloadOperation struct{ client *Client }

// TrackPhotoDownload binds the download tracking Mutation to this client.
func (client *Client) TrackPhotoDownload() TrackPhotoDownloadOperation {
	return TrackPhotoDownloadOperation{client}
}

// Definition returns the generated download tracking contract.
func (TrackPhotoDownloadOperation) Definition() sdkgo.MutationDefinition {
	return TrackPhotoDownloadDefinition
}

// IdempotencyKey records the Dex call identity; Unsplash does not deduplicate by it.
func (TrackPhotoDownloadOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackPhotoDownloadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records one download event with a bounded resend budget.
func (operation TrackPhotoDownloadOperation) Invoke(call sdkgo.Call, input TrackPhotoDownloadInput) sdkgo.MutationAttempt[TrackedPhotoDownload] {
	const name = "trackPhotoDownload"
	if !validPathID(input.ID) {
		return sdkgo.NewMutationBranch(TrackPhotoDownloadBranchDefect, TrackedPhotoDownload{}, unsplashFailure(name, sdkgo.FailureValidation, "photo ID is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, TrackPhotoDownloadDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(TrackPhotoDownloadBranchDefect, TrackedPhotoDownload{}, unsplashFailure(name, sdkgo.FailureLocalDefect, "download tracking fence could not start"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[TrackedPhotoDownload](*unsplashFailure(name, sdkgo.FailureAvailability, "download tracking checkpoint unavailable"), 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(TrackedPhotoDownload{}, *unsplashFailure(name, sdkgo.FailureTransport, "download tracking outcome cannot be confirmed"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[TrackedPhotoDownload](*unsplashFailure(name, sdkgo.FailureAuthentication, "earlier download tracking outcome cannot be confirmed"), 0)
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[TrackedPhotoDownload](*unsplashFailure(name, sdkgo.FailureTransport, "download tracking checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationBranch(TrackPhotoDownloadBranchDefect, TrackedPhotoDownload{}, unsplashFailure(name, sdkgo.FailureAuthentication, "Unsplash credentials are unavailable or invalid"), sdkgo.Receipt{})
	}
	response := &downloadTrackingResponse{}
	err = fence.Dispatch(func(requestContext context.Context) error {
		request, buildErr := http.NewRequestWithContext(requestContext, http.MethodGet, operation.client.baseURL+"/photos/"+url.PathEscape(input.ID)+"/download", nil)
		if buildErr != nil {
			return buildErr
		}
		request.Header.Set("Authorization", "Client-ID "+credentials.AccessKey.Reveal())
		request.Header.Set("Accept", "application/json")
		providerResponse, sendErr := operation.client.httpClient.Do(request)
		if sendErr != nil {
			return sendErr
		}
		defer func() { _ = providerResponse.Body.Close() }()
		response.status = providerResponse.StatusCode
		response.retryAfter = providerhttp.ParseRetryAfter(providerResponse.Header.Get("Retry-After"), time.Now())
		response.body, response.readError = providerhttp.ReadBoundedBody(providerResponse.Body, operation.client.maxResponseBytes)
		return nil
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[TrackedPhotoDownload](*unsplashFailure(name, sdkgo.FailureTransport, "download tracking dispatch checkpoint is unavailable"), 0)
		}
		return trackPhotoDownloadUnknown(call, decision.MaximumPriorSends, sdkgo.FailureTransport, "download tracking outcome is unknown", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[TrackedPhotoDownload](*unsplashFailure(name, sdkgo.FailureTransport, "download tracking retry checkpoint is unavailable"), response.retryAfter)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[TrackedPhotoDownload](*unsplashFailure(name, kind, "Unsplash is temporarily unavailable"), response.retryAfter)
	}
	if isDefiniteDownloadRefusal(response) && decision.MaximumPriorSends == 0 {
		// A lost checkpoint cannot resend a terminal refusal.
		_ = fence.RecordCanResend()
		if response.status == http.StatusNotFound {
			return sdkgo.NewMutationBranch(TrackPhotoDownloadBranchNotFound, TrackedPhotoDownload{}, unsplashFailure(name, sdkgo.FailureNotFound, "Unsplash photo was not found"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(TrackPhotoDownloadBranchProviderRejected, TrackedPhotoDownload{}, unsplashFailure(name, unsplashFailureKind(response.status), "Unsplash rejected download tracking with HTTP "+strconv.Itoa(response.status)), sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return trackPhotoDownloadUnknown(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "download tracking outcome is unknown", response.retryAfter)
	}
	if response.readError != nil {
		return trackPhotoDownloadUnknown(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "Unsplash download response could not be read", 0)
	}
	var tracked TrackedPhotoDownload
	if json.Unmarshal(response.body, &tracked) != nil || !validHTTPS(tracked.URL) {
		return trackPhotoDownloadUnknown(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "Unsplash download response has an invalid URL", 0)
	}
	return sdkgo.NewMutationBranch(TrackPhotoDownloadBranchTracked, tracked, nil, sdkgo.Receipt{})
}

type downloadTrackingResponse struct {
	status     int
	body       []byte
	readError  error
	retryAfter time.Duration
}

func isDefiniteDownloadRefusal(response *downloadTrackingResponse) bool {
	if response.readError != nil {
		return false
	}
	switch response.status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var errorResponse struct {
		Errors []string `json:"errors"`
		URL    string   `json:"url"`
	}
	return json.Unmarshal(response.body, &errorResponse) == nil && len(errorResponse.Errors) > 0 && errorResponse.URL == ""
}

func trackPhotoDownloadUnknown(call sdkgo.Call, priorSends int32, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[TrackedPhotoDownload] {
	failure := *unsplashFailure("trackPhotoDownload", kind, message)
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[TrackedPhotoDownload](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(TrackedPhotoDownload{}, failure, sdkgo.Receipt{})
}
