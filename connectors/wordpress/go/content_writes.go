// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

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

type contentWriteBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

func contentWriteBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func contentWriteRetry[T any](operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operation, Message: message}, after)
}

func contentWriteUncertain[T any](operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationUncertain(*new(T), sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operation, Message: message}, sdkgo.Receipt{})
}

func (client *Client) contentCredentials(call sdkgo.Call) (Credentials, bool) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return Credentials{}, false
	}
	password := strings.ReplaceAll(credentials.ApplicationPassword.Reveal(), " ", "")
	if !validWordPressUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(password) {
		return Credentials{}, false
	}
	credentials.ApplicationPassword = sdkgo.NewSecretString(password)
	return credentials, true
}

func writeContent[T any](client *Client, call sdkgo.Call, operation, method, path, resourcePath string, payload []byte, timeout time.Duration,
	branches contentWriteBranches, decode func([]byte) (T, error), identity func(T) int64) sdkgo.MutationAttempt[T] {
	isLimited := method == http.MethodPost && strings.TrimSuffix(path, "/") == strings.TrimSuffix(resourcePath, "/")
	var fence *singledispatch.Fence
	var priorSends int32
	if isLimited {
		var err error
		fence, err = singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
		if err != nil {
			return contentWriteBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
		}
		decision := fence.Decide()
		priorSends = decision.MaximumPriorSends
		if decision.Kind == singledispatch.FailClosed {
			return contentWriteUncertain[T](operation, sdkgo.FailureTransport, "an earlier create outcome is unknown")
		}
		if decision.Marker.ProviderID != "" {
			return readBackContent(client, call, operation, resourcePath, decision.Marker.ProviderID, branches.success, decode, identity)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return contentWriteUncertain[T](operation, sdkgo.FailureTransport, "the create send cap was reached")
		}
	}
	credentials, hasCredentials := client.contentCredentials(call)
	if !hasCredentials {
		if isLimited && priorSends > 0 {
			return contentWriteUncertain[T](operation, sdkgo.FailureAuthentication, "an earlier create may have succeeded and credentials are unavailable")
		}
		if fence != nil && fence.RecordNotSent() != nil {
			return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return contentWriteBranch[T](operation, branches.defect, sdkgo.FailureAuthentication, "WordPress credentials are unavailable")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.siteURL+path, bytes.NewReader(payload))
	if err != nil {
		return contentWriteBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "request could not be built")
	}
	request.SetBasicAuth(credentials.Username, credentials.ApplicationPassword.Reveal())
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	var response *http.Response
	if fence != nil {
		if err := singledispatch.PrepareHTTPWrite(request); err != nil {
			return contentWriteBranch[T](operation, branches.defect, sdkgo.FailureLocalDefect, "request is invalid")
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
			return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "provider request was not sent", 0)
		}
		if isLimited && priorSends+1 >= call.MaxSends() {
			return contentWriteUncertain[T](operation, sdkgo.FailureTransport, "the create send cap was reached after a transport failure")
		}
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "provider write failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence != nil && fence.RecordCanResend() != nil {
			return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return contentWriteRetry[T](operation, kind, "provider asked to retry the write", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		if isLimited && priorSends+1 >= call.MaxSends() {
			return contentWriteUncertain[T](operation, sdkgo.FailureAvailability, "the create send cap was reached after an unknown response")
		}
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "provider write outcome is unknown", 0)
	}
	if isLimited && priorSends > 0 && response.StatusCode >= 400 {
		return contentWriteUncertain[T](operation, sdkgo.FailureProviderRejection, "an earlier create may have succeeded")
	}
	if response.StatusCode == http.StatusNotFound && branches.notFound != "" {
		return contentWriteBranch[T](operation, branches.notFound, sdkgo.FailureNotFound, "resource was not found")
	}
	if response.StatusCode == http.StatusUnauthorized {
		return contentWriteBranch[T](operation, branches.rejected, sdkgo.FailureAuthentication, "provider rejected credentials")
	}
	if response.StatusCode == http.StatusForbidden {
		return contentWriteBranch[T](operation, branches.rejected, sdkgo.FailureAuthorization, "provider denied access")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contentWriteBranch[T](operation, branches.rejected, sdkgo.FailureProviderRejection, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return contentWriteBranch[T](operation, branches.invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	value, err := decode(body)
	if err != nil || identity(value) <= 0 {
		return contentWriteBranch[T](operation, branches.invalid, sdkgo.FailureProtocol, "provider returned invalid resource JSON")
	}
	if fence != nil && fence.RecordProviderID(strconv.FormatInt(identity(value), 10)) != nil {
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "resource identity checkpoint is unavailable", 0)
	}
	return sdkgo.NewMutationBranch(branches.success, value, nil, sdkgo.Receipt{})
}

func readBackContent[T any](client *Client, call sdkgo.Call, operation, resourcePath, providerID string, success sdkgo.BranchID,
	decode func([]byte) (T, error), identity func(T) int64) sdkgo.MutationAttempt[T] {
	id, err := strconv.ParseInt(providerID, 10, 64)
	if err != nil || id <= 0 {
		return contentWriteUncertain[T](operation, sdkgo.FailureProtocol, "saved resource identity is invalid")
	}
	credentials, ok := client.contentCredentials(call)
	if !ok {
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "read-back credentials are unavailable", 0)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.siteURL+resourcePath+providerID, nil)
	if err != nil {
		return contentWriteUncertain[T](operation, sdkgo.FailureLocalDefect, "read-back request could not be built")
	}
	request.SetBasicAuth(credentials.Username, credentials.ApplicationPassword.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "read-back is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "read-back is unavailable", 0)
	}
	if response.StatusCode != http.StatusOK {
		return contentWriteUncertain[T](operation, sdkgo.FailureProtocol, "created resource could not be confirmed")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return contentWriteRetry[T](operation, sdkgo.FailureAvailability, "read-back response is unavailable", 0)
	}
	value, err := decode(body)
	if err != nil || identity(value) != id {
		return contentWriteUncertain[T](operation, sdkgo.FailureProtocol, "created resource could not be confirmed")
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func encodeContentPayload(value any) ([]byte, error) { return json.Marshal(value) }
