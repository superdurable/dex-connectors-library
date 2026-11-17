// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func invokeLimitedHarvestCreate[T any](client *Client, call sdkgo.Call, operationID string, timeout time.Duration, path string, payload []byte, successBranch, rejectedBranch, defectBranch sdkgo.BranchID, decode func([]byte) (T, error), providerID func(T) string) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return harvestMutationFailure[T](operationID, defectBranch, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return harvestMutationUncertain[T](operationID, sdkgo.FailureTransport, "prior create checkpoint cannot be trusted")
	}
	if decision.Marker.ProviderID != "" {
		confirmed, found := readBackHarvestCreate(client, call, path+"/"+decision.Marker.ProviderID, decode)
		if found {
			return sdkgo.NewMutationBranch(successBranch, confirmed, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return harvestMutationUncertain[T](operationID, sdkgo.FailureTransport, "prior create cannot be confirmed")
		}
	} else if decision.Kind == singledispatch.ReadBackRequired {
		return harvestMutationUncertain[T](operationID, sdkgo.FailureTransport, "prior create cannot be confirmed")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return harvestMutationUncertain[T](operationID, sdkgo.FailureAuthentication, "credentials unavailable after an unconfirmed create")
		}
		return harvestMutationFailure[T](operationID, defectBranch, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := client.newHarvestRequest(call.Context, credentials, http.MethodPost, path, payload)
	if err != nil {
		return harvestMutationFailure[T](operationID, defectBranch, sdkgo.FailureLocalDefect, "create request cannot be built")
	}
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return harvestMutationFailure[T](operationID, defectBranch, sdkgo.FailureLocalDefect, "create request is invalid")
	}
	var response *http.Response
	var didGetConnection atomic.Bool
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		response, err = client.httpClient.Do(request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
		return err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return harvestMutationRetry[T](operationID, sdkgo.FailureAvailability, "create dispatch did not start", 0)
	}
	if err != nil {
		if !didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return harvestMutationRetry[T](operationID, sdkgo.FailureTransport, "create request was not sent", 0)
			}
		}
		return retryOrUncertainHarvestCreate[T](call, decision, operationID, sdkgo.FailureTransport, "create outcome is unknown", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return harvestMutationRetry[T](operationID, sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return harvestMutationRetry[T](operationID, kind, "Harvest deferred the create", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return retryOrUncertainHarvestCreate[T](call, decision, operationID, sdkgo.FailureAvailability, "create outcome is unknown", 0)
	}
	if response.StatusCode >= 400 {
		if decision.MaximumPriorSends > 0 {
			return harvestMutationUncertain[T](operationID, sdkgo.FailureProviderRejection, "a prior create remains unconfirmed")
		}
		return harvestMutationFailure[T](operationID, rejectedBranch, sdkgo.FailureProviderRejection, "Harvest rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusCreated {
		return retryOrUncertainHarvestCreate[T](call, decision, operationID, sdkgo.FailureProtocol, "unexpected create status", 0)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainHarvestCreate[T](call, decision, operationID, sdkgo.FailureTransport, "create response was interrupted", 0)
	}
	created, err := decode(body)
	if err != nil {
		return retryOrUncertainHarvestCreate[T](call, decision, operationID, sdkgo.FailureProtocol, "create response was invalid", 0)
	}
	_ = fence.RecordProviderID(providerID(created))
	return sdkgo.NewMutationBranch(successBranch, created, nil, sdkgo.Receipt{})
}

func readBackHarvestCreate[T any](client *Client, call sdkgo.Call, path string, decode func([]byte) (T, error)) (T, bool) {
	var zero T
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return zero, false
	}
	request, err := client.newHarvestRequest(call.Context, credentials, http.MethodGet, path, nil)
	if err != nil {
		return zero, false
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return zero, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return zero, false
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return zero, false
	}
	value, err := decode(body)
	return value, err == nil
}

func retryOrUncertainHarvestCreate[T any](call sdkgo.Call, decision singledispatch.Decision, operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return harvestMutationRetry[T](operationID, kind, message, after)
	}
	return harvestMutationUncertain[T](operationID, kind, message)
}

func harvestMutationFailure[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func harvestMutationRetry[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewMutationRetry[T](failure, after)
}

func harvestMutationUncertain[T any](operationID string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
}
