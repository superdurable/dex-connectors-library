// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailgun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendMessageInput selects a verified domain and email content for one send.
type SendMessageInput struct {
	// DomainName is the verified sending domain from Mailgun > Send > Domains.
	DomainName string `json:"domainName"`
	// From is an authorized sender email address on that domain.
	From string `json:"from"`
	// To is a non-empty list of recipient email addresses.
	To []string `json:"to"`
	// Cc lists optional visible copy recipients.
	Cc []string `json:"cc,omitempty"`
	// Bcc lists optional blind copy recipients.
	Bcc []string `json:"bcc,omitempty"`
	// ReplyTo is an optional reply address.
	ReplyTo string `json:"replyTo,omitempty"`
	// Subject is the bounded email subject.
	Subject string `json:"subject"`
	// Text is the optional plain-text body; either Text or HTML is required.
	Text string `json:"text,omitempty"`
	// HTML is the optional HTML body; either Text or HTML is required.
	HTML string `json:"html,omitempty"`
	// Tags are optional Mailgun analytics tags.
	Tags []string `json:"tags,omitempty"`
	// CustomHeaders are optional email headers sent with Mailgun's h: prefix.
	CustomHeaders []MessageHeader `json:"customHeaders,omitempty"`
}

// MessageHeader is one validated name and value for an email header.
type MessageHeader struct {
	// Name is the header name without Mailgun's h: prefix.
	Name string `json:"name"`
	// Value is the header value without line breaks.
	Value string `json:"value"`
}

// SentMessage contains Mailgun's confirmed message ID.
type SentMessage struct {
	// ID identifies the queued Mailgun message.
	ID string `json:"id"`
}

// SendMessageOperation queues an email through Mailgun.
type SendMessageOperation struct{ client *Client }

// SendMessage binds the Mutation to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey correlates retries; Mailgun does not guarantee deduplication by this key.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var sendingDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)
var mailHeaderNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,98}$`)

// Invoke sends one fenced request and bounds repeats when Mailgun's outcome is unknown.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if !validSendMessageInput(input) {
		return sentMessageBranch(SendMessageBranchDefect, sdkgo.FailureValidation, "message domain, addresses, subject, or body is invalid", sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, SendMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sentMessageBranch(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "Mailgun send checkpoint could not be initialized", sdkgo.Receipt{})
	}
	decision := fence.Decide()
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "mailgun"}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SentMessage{}, sentMessageFailure(sdkgo.FailureTransport, "Mailgun may have queued the message at the send cap"), receipt)
	}
	if decision.Marker.ProviderID != "" {
		// A confirmed response ID is durable; avoid sending again even if read-back is unavailable.
		return sdkgo.NewMutationBranch(SendMessageBranchSent, SentMessage{ID: decision.Marker.ProviderID}, nil, sdkgo.Receipt{CallID: call.ID, Provider: "mailgun", ProviderObjectID: decision.Marker.ProviderID})
	}
	payload, contentType, err := encodeMailgunMessage(input)
	if err != nil {
		return sentMessageBranch(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "Mailgun message could not be encoded", receipt)
	}
	var body []byte
	var status int
	var retryAfterDuration time.Duration
	var requestError error
	err = fence.Dispatch(func(requestContext context.Context) error {
		body, status, retryAfterDuration, requestError = operation.client.doWithContext(call, requestContext, http.MethodPost, "/v3/"+input.DomainName+"/messages", nil, payload, contentType)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(sdkgo.FailureAvailability, "Mailgun send was not dispatched after a durable claim"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(sdkgo.FailureAvailability, "Mailgun retry checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(kind, "Mailgun temporarily refused the send"), retryAfterDuration)
	}
	if errors.Is(requestError, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentMessage{}, sentMessageFailure(sdkgo.FailureAuthentication, "Mailgun credentials are unavailable after a possible earlier send"), receipt)
		}
		return sentMessageBranch(SendMessageBranchDefect, sdkgo.FailureAuthentication, "Mailgun credentials are unavailable or invalid", receipt)
	}
	if errors.Is(requestError, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(sdkgo.FailureResponseTooLarge, "Mailgun send response exceeded the limit after a possible send"), 0)
	}
	if errors.Is(requestError, errRequest) || status == http.StatusRequestTimeout || status >= 500 || requestError != nil {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(sdkgo.FailureTransport, "Mailgun send outcome is unknown"), retryAfterDuration)
		}
		return sdkgo.NewMutationUncertain(SentMessage{}, sentMessageFailure(sdkgo.FailureTransport, "Mailgun send outcome is unknown at the send cap"), receipt)
	}
	if status != http.StatusOK && status != http.StatusAccepted {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentMessage{}, sentMessageFailure(sdkgo.FailureProviderRejection, "Mailgun rejected a resend after a possible earlier send"), receipt)
		}
		return sentMessageBranch(SendMessageBranchProviderRejected, sdkgo.FailureProviderRejection, "Mailgun rejected the send", receipt)
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &response); err != nil || !validMailgunMessageID(response.ID) {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(sdkgo.FailureProtocol, "Mailgun did not confirm a message ID"), 0)
		}
		return sdkgo.NewMutationUncertain(SentMessage{}, sentMessageFailure(sdkgo.FailureProtocol, "Mailgun did not confirm a message ID at the send cap"), receipt)
	}
	if err := fence.RecordProviderID(response.ID); err != nil {
		return sdkgo.NewMutationRetry[SentMessage](sentMessageFailure(sdkgo.FailureAvailability, "Mailgun queued the message but its ID checkpoint failed"), 0)
	}
	receipt.ProviderObjectID = response.ID
	return sdkgo.NewMutationBranch(SendMessageBranchSent, SentMessage{ID: response.ID}, nil, receipt)
}

func validSendMessageInput(input SendMessageInput) bool {
	if !sendingDomainPattern.MatchString(input.DomainName) || strings.Contains(input.DomainName, "..") || !validMailAddress(input.From) || len(input.To) == 0 || len(input.To) > 1000 || len(input.Subject) == 0 || len(input.Subject) > 998 || strings.TrimSpace(input.Subject) == "" || (input.Text == "" && input.HTML == "") {
		return false
	}
	for _, address := range input.To {
		if !validMailAddress(address) {
			return false
		}
	}
	for _, address := range input.Cc {
		if !validMailAddress(address) {
			return false
		}
	}
	for _, address := range input.Bcc {
		if !validMailAddress(address) {
			return false
		}
	}
	if input.ReplyTo != "" && !validMailAddress(input.ReplyTo) {
		return false
	}
	for _, tag := range input.Tags {
		if tag == "" || len(tag) > 128 || strings.ContainsAny(tag, "\r\n") {
			return false
		}
	}
	for _, header := range input.CustomHeaders {
		if !mailHeaderNamePattern.MatchString(header.Name) || strings.ContainsAny(header.Value, "\r\n") || len(header.Value) > 998 {
			return false
		}
	}
	return !strings.ContainsAny(input.Subject, "\r\n")
}

func validMailAddress(address string) bool {
	return address != "" && len(address) <= 254 && strings.Contains(address, "@") && !strings.ContainsAny(address, "\r\n<>") && strings.TrimSpace(address) == address
}

func validMailgunMessageID(id string) bool {
	return len(id) >= 3 && len(id) <= 255 && strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") && !strings.ContainsAny(id, "\r\n")
}

func encodeMailgunMessage(input SendMessageInput) ([]byte, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	fields := [][2]string{{"from", input.From}, {"subject", input.Subject}}
	for _, recipient := range input.To {
		fields = append(fields, [2]string{"to", recipient})
	}
	for _, recipient := range input.Cc {
		fields = append(fields, [2]string{"cc", recipient})
	}
	for _, recipient := range input.Bcc {
		fields = append(fields, [2]string{"bcc", recipient})
	}
	if input.ReplyTo != "" {
		fields = append(fields, [2]string{"h:Reply-To", input.ReplyTo})
	}
	if input.Text != "" {
		fields = append(fields, [2]string{"text", input.Text})
	}
	if input.HTML != "" {
		fields = append(fields, [2]string{"html", input.HTML})
	}
	for _, tag := range input.Tags {
		fields = append(fields, [2]string{"o:tag", tag})
	}
	for _, header := range input.CustomHeaders {
		fields = append(fields, [2]string{"h:" + header.Name, header.Value})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func sentMessageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "mailgun", Operation: "sendMessage", Message: message}
}

func sentMessageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[SentMessage] {
	failure := sentMessageFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, SentMessage{}, &failure, receipt)
}
