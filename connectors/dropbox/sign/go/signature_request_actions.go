// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropboxsign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SignatureRequestSigner identifies one person asked to sign a document.
type SignatureRequestSigner struct {
	// EmailAddress is the signer's email address.
	EmailAddress string `json:"emailAddress"`
	// Name is the signer's display name.
	Name string `json:"name"`
	// Role is the exact template signer role; leave blank for a direct request.
	Role string `json:"role,omitempty"`
}

// SendSignatureRequestInput supplies public document URLs and signers.
type SendSignatureRequestInput struct {
	// FileURLs are public HTTPS document URLs without credentials or query tokens.
	FileURLs []string `json:"fileUrls"`
	// Signers are the people who must sign.
	Signers []SignatureRequestSigner `json:"signers"`
	// Title is the request title, up to 255 characters.
	Title string `json:"title"`
	// Subject is the optional signer email subject, up to 255 characters.
	Subject string `json:"subject,omitempty"`
	// Message is the optional signer email text, up to 5000 characters.
	Message string `json:"message,omitempty"`
	// TestMode sends a nonbinding test request when true.
	TestMode bool `json:"testMode,omitempty"`
}

// SendSignatureRequestWithTemplateInput selects templates and signer roles.
type SendSignatureRequestWithTemplateInput struct {
	// TemplateIDs are accessible template IDs in document order.
	TemplateIDs []string `json:"templateIds"`
	// Signers assign one name and email to each required template role.
	Signers []SignatureRequestSigner `json:"signers"`
	// Title is the optional request title, up to 255 characters.
	Title string `json:"title,omitempty"`
	// Subject is the optional signer email subject, up to 255 characters.
	Subject string `json:"subject,omitempty"`
	// Message is the optional signer email text, up to 5000 characters.
	Message string `json:"message,omitempty"`
	// TestMode sends a nonbinding test request when true.
	TestMode bool `json:"testMode,omitempty"`
}

// RemindSignatureRequestInput selects one request and signer for an email reminder.
type RemindSignatureRequestInput struct {
	// SignatureRequestID is the open request ID.
	SignatureRequestID string `json:"signatureRequestId"`
	// EmailAddress is the signer's email address.
	EmailAddress string `json:"emailAddress"`
}

// CancellationQueued reports that Dropbox Sign accepted a cancellation job.
type CancellationQueued struct {
	// SignatureRequestID is the selected request ID.
	SignatureRequestID string `json:"signatureRequestId"`
	// Queued means the provider accepted the job, not that cancellation completed.
	Queued bool `json:"queued"`
}

// SendSignatureRequestOperation sends a document URL to signers.
type SendSignatureRequestOperation struct{ client *Client }

// SendSignatureRequestWithTemplateOperation sends an existing template.
type SendSignatureRequestWithTemplateOperation struct{ client *Client }

// CancelIncompleteSignatureRequestOperation queues cancellation.
type CancelIncompleteSignatureRequestOperation struct{ client *Client }

// RemindSignatureRequestOperation emails one signer a reminder.
type RemindSignatureRequestOperation struct{ client *Client }

// SendSignatureRequest binds the direct send Mutation to this client.
func (client *Client) SendSignatureRequest() SendSignatureRequestOperation {
	return SendSignatureRequestOperation{client}
}

// SendSignatureRequestWithTemplate binds the template send Mutation to this client.
func (client *Client) SendSignatureRequestWithTemplate() SendSignatureRequestWithTemplateOperation {
	return SendSignatureRequestWithTemplateOperation{client}
}

// CancelIncompleteSignatureRequest binds the cancellation Mutation to this client.
func (client *Client) CancelIncompleteSignatureRequest() CancelIncompleteSignatureRequestOperation {
	return CancelIncompleteSignatureRequestOperation{client}
}

// RemindSignatureRequest binds the reminder Mutation to this client.
func (client *Client) RemindSignatureRequest() RemindSignatureRequestOperation {
	return RemindSignatureRequestOperation{client}
}

// Definition returns the generated direct send contract.
func (SendSignatureRequestOperation) Definition() sdkgo.MutationDefinition {
	return SendSignatureRequestDefinition
}

// Definition returns the generated template send contract.
func (SendSignatureRequestWithTemplateOperation) Definition() sdkgo.MutationDefinition {
	return SendSignatureRequestWithTemplateDefinition
}

// Definition returns the generated cancellation contract.
func (CancelIncompleteSignatureRequestOperation) Definition() sdkgo.MutationDefinition {
	return CancelIncompleteSignatureRequestDefinition
}

// Definition returns the generated reminder contract.
func (RemindSignatureRequestOperation) Definition() sdkgo.MutationDefinition {
	return RemindSignatureRequestDefinition
}

// IdempotencyKey records call identity; Dropbox Sign does not deduplicate direct sends.
func (SendSignatureRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ SendSignatureRequestInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records call identity; Dropbox Sign does not deduplicate template sends.
func (SendSignatureRequestWithTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ SendSignatureRequestWithTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records call identity for cancellation.
func (CancelIncompleteSignatureRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ GetSignatureRequestInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records call identity; Dropbox Sign does not deduplicate reminders.
func (RemindSignatureRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ RemindSignatureRequestInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one signature request from public document URLs.
func (operation SendSignatureRequestOperation) Invoke(call sdkgo.Call, input SendSignatureRequestInput) sdkgo.MutationAttempt[SignatureRequestStatus] {
	if len(input.FileURLs) == 0 || len(input.FileURLs) > 10 || !validSignatureRequestSigners(input.Signers, false) || !validRequestText(input.Title, 255, true) || !validRequestText(input.Subject, 255, false) || !validRequestText(input.Message, 5000, false) {
		return actionFailure("sendSignatureRequest", SendSignatureRequestBranchDefect, sdkgo.FailureValidation, "document, signer, or message input is invalid")
	}
	for _, fileURL := range input.FileURLs {
		if !validPublicDocumentURL(fileURL) {
			return actionFailure("sendSignatureRequest", SendSignatureRequestBranchDefect, sdkgo.FailureValidation, "document URL must be public HTTPS without credentials")
		}
	}
	requestBody, err := json.Marshal(struct {
		FileURLs []string            `json:"file_urls"`
		Signers  []map[string]string `json:"signers"`
		Title    string              `json:"title"`
		Subject  string              `json:"subject,omitempty"`
		Message  string              `json:"message,omitempty"`
		TestMode bool                `json:"test_mode,omitempty"`
	}{input.FileURLs, signerWire(input.Signers, false), input.Title, input.Subject, input.Message, input.TestMode})
	if err != nil {
		return actionFailure("sendSignatureRequest", SendSignatureRequestBranchDefect, sdkgo.FailureValidation, "send input could not be encoded")
	}
	return invokeLimitedSignatureAction(call, operation.client, SendSignatureRequestDefinition, "sendSignatureRequest", "/signature_request/send", requestBody, "application/json", "", true, SendSignatureRequestBranchSent, SendSignatureRequestBranchProviderRejected, SendSignatureRequestBranchDefect)
}

// Invoke sends one signature request from accessible templates.
func (operation SendSignatureRequestWithTemplateOperation) Invoke(call sdkgo.Call, input SendSignatureRequestWithTemplateInput) sdkgo.MutationAttempt[SignatureRequestStatus] {
	if len(input.TemplateIDs) == 0 || len(input.TemplateIDs) > 10 || !validSignatureRequestSigners(input.Signers, true) || !validRequestText(input.Title, 255, false) || !validRequestText(input.Subject, 255, false) || !validRequestText(input.Message, 5000, false) {
		return actionFailure("sendSignatureRequestWithTemplate", SendSignatureRequestWithTemplateBranchDefect, sdkgo.FailureValidation, "template, signer, or message input is invalid")
	}
	for _, templateID := range input.TemplateIDs {
		if !validSignatureRequestID(templateID) {
			return actionFailure("sendSignatureRequestWithTemplate", SendSignatureRequestWithTemplateBranchDefect, sdkgo.FailureValidation, "template ID is invalid")
		}
	}
	requestBody, err := json.Marshal(struct {
		TemplateIDs []string            `json:"template_ids"`
		Signers     []map[string]string `json:"signers"`
		Title       string              `json:"title,omitempty"`
		Subject     string              `json:"subject,omitempty"`
		Message     string              `json:"message,omitempty"`
		TestMode    bool                `json:"test_mode,omitempty"`
	}{input.TemplateIDs, signerWire(input.Signers, true), input.Title, input.Subject, input.Message, input.TestMode})
	if err != nil {
		return actionFailure("sendSignatureRequestWithTemplate", SendSignatureRequestWithTemplateBranchDefect, sdkgo.FailureValidation, "template send input could not be encoded")
	}
	return invokeLimitedSignatureAction(call, operation.client, SendSignatureRequestWithTemplateDefinition, "sendSignatureRequestWithTemplate", "/signature_request/send_with_template", requestBody, "application/json", "", true, SendSignatureRequestWithTemplateBranchSent, SendSignatureRequestWithTemplateBranchProviderRejected, SendSignatureRequestWithTemplateBranchDefect)
}

// Invoke queues cancellation; completion is confirmed later by a provider event.
func (operation CancelIncompleteSignatureRequestOperation) Invoke(call sdkgo.Call, input GetSignatureRequestInput) sdkgo.MutationAttempt[CancellationQueued] {
	if !validSignatureRequestID(input.SignatureRequestID) {
		return cancelFailure(CancelIncompleteSignatureRequestBranchDefect, sdkgo.FailureValidation, "signature request ID is invalid")
	}
	response := operation.client.send(call, http.MethodPost, "/signature_request/cancel/"+url.PathEscape(input.SignatureRequestID), nil, "")
	if errors.Is(response.err, errCredential) {
		return cancelFailure(CancelIncompleteSignatureRequestBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return cancelRetry(sdkgo.FailureTransport, "cancellation request failed", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return cancelRetry(sdkgo.FailureAvailability, "cancellation outcome is unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return cancelFailure(CancelIncompleteSignatureRequestBranchNotFound, sdkgo.FailureNotFound, "signature request is unavailable")
	}
	if response.status != http.StatusOK {
		return cancelFailure(CancelIncompleteSignatureRequestBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected cancellation with HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return cancelRetry(sdkgo.FailureTransport, "cancellation response was interrupted", 0)
	}
	return sdkgo.NewMutationBranch(CancelIncompleteSignatureRequestBranchQueued, CancellationQueued{SignatureRequestID: input.SignatureRequestID, Queued: true}, nil, sdkgo.Receipt{})
}

// Invoke sends one reminder to the selected signer.
func (operation RemindSignatureRequestOperation) Invoke(call sdkgo.Call, input RemindSignatureRequestInput) sdkgo.MutationAttempt[SignatureRequestStatus] {
	if !validSignatureRequestID(input.SignatureRequestID) || !validSignerEmail(input.EmailAddress) {
		return actionFailure("remindSignatureRequest", RemindSignatureRequestBranchDefect, sdkgo.FailureValidation, "request ID or signer email is invalid")
	}
	requestBody, err := json.Marshal(map[string]string{"email_address": input.EmailAddress})
	if err != nil {
		return actionFailure("remindSignatureRequest", RemindSignatureRequestBranchDefect, sdkgo.FailureValidation, "reminder input could not be encoded")
	}
	return invokeLimitedSignatureAction(call, operation.client, RemindSignatureRequestDefinition, "remindSignatureRequest", "/signature_request/remind/"+url.PathEscape(input.SignatureRequestID), requestBody, "application/json", input.SignatureRequestID, false, RemindSignatureRequestBranchReminded, RemindSignatureRequestBranchProviderRejected, RemindSignatureRequestBranchDefect)
}

func invokeLimitedSignatureAction(call sdkgo.Call, client *Client, definition sdkgo.MutationDefinition, operation, path string, body []byte, contentType, expectedID string, canConfirmByRequestID bool, success, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[SignatureRequestStatus] {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return actionFailure(operation, defect, sdkgo.FailureLocalDefect, "send fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return actionUncertain(operation, "send checkpoint cannot be trusted")
	}
	if canConfirmByRequestID && decision.Marker.ProviderID != "" {
		response := client.get(call, "/signature_request/"+url.PathEscape(decision.Marker.ProviderID))
		if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
			return actionRetry(operation, sdkgo.FailureAvailability, "signature request read-back is unavailable", response.retryAfter)
		}
		if response.status == http.StatusOK {
			status, decodeErr := decodeSignatureRequest(response.body, decision.Marker.ProviderID)
			if decodeErr == nil {
				return sdkgo.NewMutationBranch(success, status, nil, sdkgo.Receipt{})
			}
			return actionRetry(operation, sdkgo.FailureProtocol, "signature request read-back is invalid", 0)
		}
		if response.status != http.StatusNotFound {
			return actionRetry(operation, sdkgo.FailureProtocol, "signature request read-back is inconclusive", 0)
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return actionUncertain(operation, "send count reached its cap without confirmation")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.sendWithContext(call, requestContext, http.MethodPost, path, body, contentType)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return actionRetry(operation, sdkgo.FailureAvailability, "send was not dispatched", 0)
	}
	if err != nil {
		return actionUncertain(operation, "send dispatch was not allowed")
	}
	if errors.Is(response.err, errCredential) {
		return actionFailure(operation, defect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if recordErr := fence.RecordCanResend(); recordErr != nil {
			return actionRetry(operation, sdkgo.FailureAvailability, "send retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return actionRetry(operation, kind, "Dropbox Sign is temporarily unavailable", response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return actionRetry(operation, sdkgo.FailureAvailability, "send outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return actionFailure(operation, rejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	status, decodeErr := decodeSentSignatureRequest(response.body, expectedID)
	if decodeErr != nil {
		return actionRetry(operation, sdkgo.FailureProtocol, "Dropbox Sign send confirmation is invalid", 0)
	}
	if canConfirmByRequestID {
		if recordErr := fence.RecordProviderID(status.ID); recordErr != nil {
			return actionRetry(operation, sdkgo.FailureAvailability, "request ID checkpoint is unavailable", 0)
		}
	}
	return sdkgo.NewMutationBranch(success, status, nil, sdkgo.Receipt{})
}

func decodeSentSignatureRequest(body []byte, expectedID string) (SignatureRequestStatus, error) {
	var wire struct {
		SignatureRequest struct {
			ID         string `json:"signature_request_id"`
			Title      string `json:"title"`
			IsComplete *bool  `json:"is_complete"`
			IsDeclined *bool  `json:"is_declined"`
			HasError   *bool  `json:"has_error"`
		} `json:"signature_request"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SignatureRequestStatus{}, err
	}
	request := wire.SignatureRequest
	if !validSignatureRequestID(request.ID) || expectedID != "" && request.ID != expectedID || strings.TrimSpace(request.Title) == "" || request.IsComplete == nil || request.IsDeclined == nil || request.HasError == nil {
		return SignatureRequestStatus{}, fmt.Errorf("signature request confirmation is incomplete")
	}
	return SignatureRequestStatus{ID: request.ID, Title: request.Title, IsComplete: *request.IsComplete, IsDeclined: *request.IsDeclined, HasError: *request.HasError}, nil
}

func signerWire(signers []SignatureRequestSigner, needsRole bool) []map[string]string {
	values := make([]map[string]string, 0, len(signers))
	for _, signer := range signers {
		value := map[string]string{"email_address": signer.EmailAddress, "name": signer.Name}
		if needsRole {
			value["role"] = signer.Role
		}
		values = append(values, value)
	}
	return values
}

func validSignatureRequestSigners(signers []SignatureRequestSigner, needsRole bool) bool {
	if len(signers) == 0 || len(signers) > 20 {
		return false
	}
	for _, signer := range signers {
		if !validSignerEmail(signer.EmailAddress) || !validSignerName(signer.Name) || needsRole && !validSignerName(signer.Role) || !needsRole && signer.Role != "" {
			return false
		}
	}
	return true
}

func validSignerEmail(email string) bool {
	return len(email) <= 320 && strings.Count(email, "@") == 1 && !strings.ContainsAny(email, " \r\n\x00")
}
func validRequestText(value string, maximum int, required bool) bool {
	return (!required || value != "") && len(value) <= maximum && !strings.ContainsAny(value, "\x00")
}
func validPublicDocumentURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return false
	}
	host := parsed.Hostname()
	return strings.Contains(host, ".") && net.ParseIP(host) == nil && !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal")
}

func actionFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SignatureRequestStatus] {
	failure := signatureExpansionFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, SignatureRequestStatus{}, &failure, sdkgo.Receipt{})
}
func actionRetry(operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SignatureRequestStatus] {
	return sdkgo.NewMutationRetry[SignatureRequestStatus](signatureExpansionFailure(operation, kind, message), retryAfter)
}
func actionUncertain(operation, message string) sdkgo.MutationAttempt[SignatureRequestStatus] {
	return sdkgo.NewMutationUncertain(SignatureRequestStatus{}, signatureExpansionFailure(operation, sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}
func cancelFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CancellationQueued] {
	failure := signatureExpansionFailure("cancelIncompleteSignatureRequest", kind, message)
	return sdkgo.NewMutationBranch(branch, CancellationQueued{}, &failure, sdkgo.Receipt{})
}
func cancelRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CancellationQueued] {
	return sdkgo.NewMutationRetry[CancellationQueued](signatureExpansionFailure("cancelIncompleteSignatureRequest", kind, message), retryAfter)
}
