// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

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

type catalogWriteBranches struct {
	success, notFound, rejected, invalid, defect sdkgo.BranchID
}

func catalogMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationBranch(branch, *new(T), failureFor(operation, kind, message), sdkgo.Receipt{})
}

func catalogMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](*failureFor(operation, kind, message), after)
}

func catalogMutationUncertain[T any](operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationUncertain(*new(T), *failureFor(operation, kind, message), sdkgo.Receipt{})
}

func writeCatalogResource[T any](client *Client, call sdkgo.Call, operation, method, path, resourcePath string, payload []byte, timeout time.Duration,
	branches catalogWriteBranches, decode func([]byte) (T, error), identity func(T) int64) sdkgo.MutationAttempt[T] {
	isLimited := method == http.MethodPost
	var fence *singledispatch.Fence
	var priorSends int32
	if isLimited {
		var err error
		fence, err = singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
		if err != nil {
			return catalogMutationBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
		}
		decision := fence.Decide()
		priorSends = decision.MaximumPriorSends
		if decision.Kind == singledispatch.FailClosed {
			return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "dispatch checkpoint is unavailable", 0)
		}
		if decision.Marker.ProviderID != "" {
			return readBackCatalogResource(client, call, operation, resourcePath, decision.Marker.ProviderID, branches.success, decode, identity)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return catalogMutationUncertain[T](operation, sdkgo.FailureTransport, "the create send cap was reached")
		}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ConsumerKey.Reveal()) || !providerhttp.IsHeaderSafeCredential(credentials.ConsumerSecret.Reveal()) {
		if isLimited && priorSends > 0 {
			return catalogMutationRetry[T](operation, sdkgo.FailureAuthentication, "credentials are unavailable for read-back", 0)
		}
		if fence != nil && fence.RecordNotSent() != nil {
			return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return catalogMutationBranch[T](operation, branches.defect, sdkgo.FailureAuthentication, "WooCommerce credentials are unavailable")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		if isLimited && priorSends > 0 {
			return catalogMutationRetry[T](operation, sdkgo.FailureLocalDefect, "provider request could not be built", 0)
		}
		return catalogMutationBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "provider request could not be built")
	}
	request.SetBasicAuth(credentials.ConsumerKey.Reveal(), credentials.ConsumerSecret.Reveal())
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	var response *http.Response
	if fence != nil {
		if err := singledispatch.PrepareHTTPWrite(request); err != nil {
			if priorSends > 0 {
				return catalogMutationRetry[T](operation, sdkgo.FailureLocalDefect, "provider request is invalid", 0)
			}
			return catalogMutationBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "provider request is invalid")
		}
		err = fence.Dispatch(func(requestContext context.Context) error {
			response, err = client.httpClient.Do(request.WithContext(requestContext))
			return err
		})
	} else {
		response, err = client.httpClient.Do(request)
	}
	if err != nil {
		if fence != nil && (errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime)) {
			return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "provider request was not sent", 0)
		}
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "provider write failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence != nil && fence.RecordCanResend() != nil {
			return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return catalogMutationRetry[T](operation, kind, "provider asked to retry the write", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "provider write outcome is unknown", 0)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if !isDefiniteWooCommerceRefusal(response, client.maxResponseBytes) || isLimited && priorSends > 0 {
			return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "provider write outcome is unknown", 0)
		}
		if fence != nil && fence.RecordCanResend() != nil {
			return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "refusal checkpoint is unavailable", 0)
		}
	}
	if response.StatusCode == http.StatusNotFound && branches.notFound != "" {
		return catalogMutationBranch[T](operation, branches.notFound, sdkgo.FailureProviderRejection, "resource was not found")
	}
	if response.StatusCode == http.StatusUnauthorized {
		return catalogMutationBranch[T](operation, branches.rejected, sdkgo.FailureAuthentication, "provider rejected credentials")
	}
	if response.StatusCode == http.StatusForbidden {
		return catalogMutationBranch[T](operation, branches.rejected, sdkgo.FailureAuthorization, "provider denied access")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return catalogMutationBranch[T](operation, branches.rejected, sdkgo.FailureProviderRejection, "provider rejected write with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return catalogMutationRetry[T](operation, sdkgo.FailureResponseTooLarge, "provider write response could not be read within the configured limit", 0)
	}
	value, err := decode(body)
	if err != nil || identity(value) <= 0 {
		return catalogMutationRetry[T](operation, sdkgo.FailureProtocol, "provider returned invalid resource JSON", 0)
	}
	if fence != nil && fence.RecordProviderID(strconv.FormatInt(identity(value), 10)) != nil {
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "created resource identity checkpoint is unavailable", 0)
	}
	return sdkgo.NewMutationBranch(branches.success, value, nil, sdkgo.Receipt{})
}

func readBackCatalogResource[T any](client *Client, call sdkgo.Call, operation, resourcePath, providerID string, successBranch sdkgo.BranchID,
	decode func([]byte) (T, error), identity func(T) int64) sdkgo.MutationAttempt[T] {
	id, err := strconv.ParseInt(providerID, 10, 64)
	if err != nil || id <= 0 {
		return catalogMutationRetry[T](operation, sdkgo.FailureProtocol, "saved resource identity is invalid", 0)
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "read-back credentials are unavailable", 0)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+resourcePath+providerID, nil)
	if err != nil {
		return catalogMutationRetry[T](operation, sdkgo.FailureLocalDefect, "read-back request could not be built", 0)
	}
	request.SetBasicAuth(credentials.ConsumerKey.Reveal(), credentials.ConsumerSecret.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "read-back is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "read-back is unavailable", 0)
	}
	if response.StatusCode != http.StatusOK {
		return catalogMutationRetry[T](operation, sdkgo.FailureProtocol, "created resource could not be confirmed", 0)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return catalogMutationRetry[T](operation, sdkgo.FailureAvailability, "read-back response is unavailable", 0)
	}
	value, err := decode(body)
	if err != nil || identity(value) != id {
		return catalogMutationRetry[T](operation, sdkgo.FailureProtocol, "created resource could not be confirmed", 0)
	}
	return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func encodeCatalogPayload(payload any) ([]byte, error) { return json.Marshal(payload) }
func validCatalogText(value string) bool               { return strings.TrimSpace(value) != "" }

func isDefiniteWooCommerceRefusal(response *http.Response, maximumResponseBytes int64) bool {
	switch response.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, maximumResponseBytes)
	if err != nil {
		return false
	}
	var rejection struct {
		ID json.RawMessage `json:"id"`
		Data json.RawMessage `json:"data"`
		Code string `json:"code"`
		Message string `json:"message"`
	}
	return json.Unmarshal(body, &rejection) == nil && len(rejection.ID) == 0 && len(rejection.Data) == 0 && rejection.Code != "" && rejection.Message != ""
}
