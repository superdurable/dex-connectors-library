// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendEmailDraftInput identifies an existing email draft to send.
type SendEmailDraftInput struct {
	// EmailID is the Close acti_ identifier of a draft email.
	EmailID string `json:"emailId"`
}

// SendSMSDraftInput identifies an existing SMS draft to send.
type SendSMSDraftInput struct {
	// SMSID is the Close acti_ identifier of a draft SMS.
	SMSID string `json:"smsId"`
}

// SendEmailDraftOperation moves a draft email to outbox with bounded possible sends.
type SendEmailDraftOperation struct{ client *Client }

// SendEmailDraft binds this Mutation to the client.
func (client *Client) SendEmailDraft() SendEmailDraftOperation {
	return SendEmailDraftOperation{client}
}

// Definition returns the generated sendEmailDraft contract.
func (SendEmailDraftOperation) Definition() sdkgo.MutationDefinition { return SendEmailDraftDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (SendEmailDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEmailDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back a prior uncertain transition before another send attempt.
func (operation SendEmailDraftOperation) Invoke(call sdkgo.Call, input SendEmailDraftInput) sdkgo.MutationAttempt[EmailActivity] {
	if !validEmailActivityID(input.EmailID) {
		return closeMutationBranch[EmailActivity]("sendEmailDraft", SendEmailDraftBranchDefect, sdkgo.FailureValidation, "emailId is invalid")
	}
	return invokeCloseDraftSend(operation.client, call, "sendEmailDraft", "/activity/email/"+input.EmailID+"/", SendEmailDraftBranchSent, SendEmailDraftBranchProviderRejected, SendEmailDraftBranchDefect, decodeEmailActivity)
}

// SendSMSDraftOperation moves a draft SMS to outbox with bounded possible sends.
type SendSMSDraftOperation struct{ client *Client }

// SendSMSDraft binds this Mutation to the client.
func (client *Client) SendSMSDraft() SendSMSDraftOperation { return SendSMSDraftOperation{client} }

// Definition returns the generated sendSMSDraft contract.
func (SendSMSDraftOperation) Definition() sdkgo.MutationDefinition { return SendSMSDraftDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (SendSMSDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ SendSMSDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back a prior uncertain transition before another send attempt.
func (operation SendSMSDraftOperation) Invoke(call sdkgo.Call, input SendSMSDraftInput) sdkgo.MutationAttempt[SMSActivity] {
	if !validSMSActivityID(input.SMSID) {
		return closeMutationBranch[SMSActivity]("sendSMSDraft", SendSMSDraftBranchDefect, sdkgo.FailureValidation, "smsId is invalid")
	}
	return invokeCloseDraftSend(operation.client, call, "sendSMSDraft", "/activity/sms/"+input.SMSID+"/", SendSMSDraftBranchSent, SendSMSDraftBranchProviderRejected, SendSMSDraftBranchDefect, decodeSMSActivity)
}

func invokeCloseDraftSend[T interface{ EmailActivity | SMSActivity }](client *Client, call sdkgo.Call, operation, path string, sentBranch, rejectedBranch, defectBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, 45*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return closeMutationBranch[T](operation, defectBranch, sdkgo.FailureLocalDefect, "dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return closeCreateUncertain[T](operation, sdkgo.FailureTransport, "earlier send cannot be reconciled")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return closeCreateUncertain[T](operation, sdkgo.FailureAuthentication, "earlier send may have succeeded; credentials unavailable")
		}
		return closeMutationBranch[T](operation, defectBranch, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if decision.MaximumPriorSends > 0 {
		value, found, retry := readBackCloseCreate(client, call.Context, credentials, path, decode)
		if found {
			if isCloseMessageSent(value) {
				return sdkgo.NewMutationBranch(sentBranch, value, nil, sdkgo.Receipt{})
			}
		} else if retry {
			return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "send read-back unavailable", 0)
		} else {
			return closeCreateUncertain[T](operation, sdkgo.FailureNotFound, "draft disappeared after an uncertain send")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return closeCreateUncertain[T](operation, sdkgo.FailureTransport, "earlier send cannot be confirmed")
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		request, requestError := http.NewRequestWithContext(requestContext, http.MethodPut, client.baseURL+path, bytes.NewReader([]byte(`{"status":"outbox"}`)))
		if requestError != nil {
			return requestError
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		request.SetBasicAuth(credentials.APIKey.Reveal(), "")
		if requestError := singledispatch.PrepareHTTPWrite(request); requestError != nil {
			return requestError
		}
		response, requestError = client.httpClient.Do(request)
		return requestError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "send was not dispatched", 0)
	}
	if err != nil {
		return retryOrUncertainCloseCreate[T](operation, sdkgo.FailureTransport, "send outcome is unknown", canResendUnknown)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return closeMutationRetry[T](operation, sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return closeMutationRetry[T](operation, kind, "provider did not process send", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return retryOrUncertainCloseCreate[T](operation, sdkgo.FailureAvailability, "send outcome is unknown", canResendUnknown)
	}
	if response.StatusCode != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return closeCreateUncertain[T](operation, sdkgo.FailureProviderRejection, "earlier send may have succeeded")
		}
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return closeMutationBranch[T](operation, rejectedBranch, kind, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return closeCreateUncertain[T](operation, sdkgo.FailureTransport, "send succeeded but response was incomplete")
	}
	value, err := decode(body)
	if err != nil || !isCloseMessageSent(value) {
		return closeCreateUncertain[T](operation, sdkgo.FailureProtocol, "send response did not confirm outbox status")
	}
	return sdkgo.NewMutationBranch(sentBranch, value, nil, sdkgo.Receipt{})
}

func isCloseMessageSent[T interface{ EmailActivity | SMSActivity }](value T) bool {
	switch message := any(value).(type) {
	case EmailActivity:
		return message.Status == "outbox" || message.Status == "sent"
	case SMSActivity:
		return message.Status == "outbox" || message.Status == "sent"
	default:
		return false
	}
}
