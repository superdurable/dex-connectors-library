// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

type createBranches struct{ success, rejected, defect sdkgo.BranchID }

func invokeLimitedCreate[T any](client *Client, call sdkgo.Call, operation, path string, requestBody []byte, timeout time.Duration, branches createBranches, decode func([]byte) (T, error), providerID func(T) string, readBackPath func(string) string) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return createUncertain[T](call, operation, sdkgo.FailureAvailability, "earlier creation may have succeeded", "")
	case singledispatch.ReadBackRequired:
		return readBackCreated(client, call, operation, decision.Marker.ProviderID, branches.success, decode, providerID, readBackPath)
	case singledispatch.ReadBackThenResendAllowed:
		if decision.Marker.ProviderID != "" {
			return readBackCreated(client, call, operation, decision.Marker.ProviderID, branches.success, decode, providerID, readBackPath)
		}
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPriorSend := decision.MaximumPriorSends > 0
	var body []byte
	var status int
	var delay time.Duration
	var sendErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		body, status, delay, sendErr = client.request(call, requestContext, http.MethodPost, path, requestBody)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return createRetry[T](operation, sdkgo.FailureAvailability, "request was not sent before checkpoint", 0)
	}
	if err != nil {
		return createUncertain[T](call, operation, sdkgo.FailureTransport, "dispatch outcome is unknown", "")
	}
	if errors.Is(sendErr, errCredential) {
		if hasPriorSend {
			return createUncertain[T](call, operation, sdkgo.FailureAuthentication, "earlier creation may have succeeded; reconnect", "")
		}
		if fence.RecordNotSent() != nil {
			return createRetry[T](operation, sdkgo.FailureAvailability, "no-send checkpoint unavailable", 0)
		}
		return createBranch[T](operation, branches.defect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return createRetry[T](operation, sdkgo.FailureAvailability, "resend checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createRetry[T](operation, kind, "Freshsales did not accept the create request", delay)
	}
	if status == http.StatusRequestTimeout || status >= 500 || errors.Is(sendErr, errRequest) {
		if canResendUnknown {
			return createRetry[T](operation, sdkgo.FailureAvailability, "create outcome unknown within send cap", delay)
		}
		return createUncertain[T](call, operation, sdkgo.FailureAvailability, "create outcome unknown after send cap", "")
	}
	if status < 200 || status >= 300 {
		if hasPriorSend {
			return createUncertain[T](call, operation, sdkgo.FailureProviderRejection, "earlier creation may have succeeded", "")
		}
		return createBranch[T](operation, branches.rejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	}
	if sendErr != nil {
		return createUncertain[T](call, operation, sdkgo.FailureProtocol, "created resource identity unavailable", "")
	}
	created, decodeErr := decode(body)
	if decodeErr != nil || providerID(created) == "" {
		return createUncertain[T](call, operation, sdkgo.FailureProtocol, "created resource identity unusable", "")
	}
	_ = fence.RecordProviderID(providerID(created))
	return sdkgo.NewMutationBranch(branches.success, created, nil, createReceipt(call, providerID(created)))
}

func readBackCreated[T any](client *Client, call sdkgo.Call, operation, resourceID string, branch sdkgo.BranchID, decode func([]byte) (T, error), providerID func(T) string, path func(string) string) sdkgo.MutationAttempt[T] {
	if resourceID == "" {
		return createUncertain[T](call, operation, sdkgo.FailureProtocol, "prior create has no resource ID", "")
	}
	body, status, delay, err := client.get(call, path(resourceID))
	if status == http.StatusTooManyRequests || status >= 500 || status == http.StatusNotFound || err != nil {
		return createRetry[T](operation, sdkgo.FailureAvailability, "created resource is not yet readable", delay)
	}
	if status != http.StatusOK {
		return createUncertain[T](call, operation, sdkgo.FailureProviderRejection, "created resource read-back rejected", resourceID)
	}
	value, decodeErr := decode(body)
	if decodeErr != nil || providerID(value) != resourceID {
		return createUncertain[T](call, operation, sdkgo.FailureProtocol, "created resource read-back did not match", resourceID)
	}
	return sdkgo.NewMutationBranch(branch, value, nil, createReceipt(call, resourceID))
}

func createReceipt(call sdkgo.Call, resourceID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "freshsales", ProviderObjectID: resourceID, ObservedAt: time.Now()}
}
func createBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func createRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](queryFailure(operation, kind, message), delay)
}
func createUncertain[T any](call sdkgo.Call, operation string, kind sdkgo.FailureKind, message, resourceID string) sdkgo.MutationAttempt[T] {
	var zero T
	return sdkgo.NewMutationUncertain(zero, queryFailure(operation, kind, message), createReceipt(call, resourceID))
}
