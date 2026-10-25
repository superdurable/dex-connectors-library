// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// gmailSendOperation maps the outcome of one messages.send POST to an operation's branches.
type gmailSendOperation struct {
	operationID            string
	requestName            string
	rejectionMessage       string
	sentBranch             sdkgo.BranchID
	providerRejectedBranch sdkgo.BranchID
}

var (
	sendMessageOutcomes = gmailSendOperation{
		operationID: "sendMessage", requestName: "send", rejectionMessage: "Gmail rejected the message",
		sentBranch: SendMessageBranchSent, providerRejectedBranch: SendMessageBranchProviderRejected,
	}
	replyToMessageOutcomes = gmailSendOperation{
		operationID: "replyToMessage", requestName: "reply", rejectionMessage: "Gmail rejected the reply",
		sentBranch: ReplyToMessageBranchSent, providerRejectedBranch: ReplyToMessageBranchProviderRejected,
	}
)

// gmailOutgoingMessage is one encoded messages.send request body and the identity it reports.
type gmailOutgoingMessage struct {
	payload    []byte
	recipients []string
	// requestedThreadID is the thread a reply asked for; empty for a new message.
	requestedThreadID string
}

// sendAttemptBudget splits one attempt's Execute time so Invoke returns before the engine times it out.
//
// The time is the operation's StepDefaults Execute timeout, shortened by the Step context deadline when
// the Dex SDK sets one; a shorter StepOptionsOverride is honored only through that deadline. An async
// Step's first attempt can end much sooner than the Execute timeout, and only that deadline shows it.
// With the 30-second default, the POST ends by 17.5 seconds, the read-back by 25, and the last 5 cover
// Worker dispatch and the Result write.
type sendAttemptBudget struct {
	// attemptContext ends at five sixths of the time; every request of the attempt uses it.
	attemptContext context.Context
	cancel         context.CancelFunc
	// sendDeadline ends the POST at seven twelfths, keeping a quarter for the read-back.
	sendDeadline time.Time
	// minimumSendTime is a tenth; with less left, the POST is not started.
	minimumSendTime time.Duration
}

type sentMessageSearchResponse struct {
	Messages []sendResponse `json:"messages"`
}

// requestDispatchObservation records whether a traced connection attempt proved a request never left the process.
type requestDispatchObservation struct {
	mutex                 sync.Mutex
	hasFailedToConnect    bool
	hasObtainedConnection bool
}

func newSendAttemptBudget(stepContext context.Context, executeTimeout time.Duration) sendAttemptBudget {
	start := time.Now()
	available := executeTimeout
	if stepDeadline, hasDeadline := stepContext.Deadline(); hasDeadline {
		available = min(available, stepDeadline.Sub(start))
	}
	attemptContext, cancel := context.WithDeadline(stepContext, start.Add(available*5/6))
	return sendAttemptBudget{
		attemptContext: attemptContext, cancel: cancel,
		sendDeadline: start.Add(available * 7 / 12), minimumSendTime: available / 10,
	}
}

// preSendContext bounds reads before the POST so the POST keeps at least minimumSendTime.
func (budget sendAttemptBudget) preSendContext() (context.Context, context.CancelFunc) {
	return context.WithDeadline(budget.attemptContext, budget.sendDeadline.Add(-budget.minimumSendTime))
}

// sendContext bounds the POST; false means too little time is left to start it.
func (budget sendAttemptBudget) sendContext() (context.Context, context.CancelFunc, bool) {
	if budget.minimumSendTime <= 0 || time.Until(budget.sendDeadline) < budget.minimumSendTime {
		return nil, nil, false
	}
	sendContext, cancel := context.WithDeadline(budget.attemptContext, budget.sendDeadline)
	return sendContext, cancel, true
}

// sendAndConfirm POSTs once. A possibly sent request never returns Retry, since Retry would send it again.
func (client *Client) sendAndConfirm(
	call sdkgo.Call, budget sendAttemptBudget, credentials Credentials, operation gmailSendOperation, message gmailOutgoingMessage,
) sdkgo.MutationAttempt[SendMessageOutput] {
	sendContext, cancelSend, hasTimeToSend := budget.sendContext()
	if !hasTimeToSend {
		return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure(operation.operationID, sdkgo.FailureAvailability,
			"too little of the Execute attempt is left to send safely, so nothing was sent"), 0)
	}
	defer cancelSend()
	target := strings.TrimRight(client.endpoint.String(), "/") + "/users/me/messages/send"
	response, credentials, isPossiblyDispatched, err := client.doAuthenticatedRequestWithDispatch(
		sendContext, call, credentials, http.MethodPost, target, message.payload, "application/json",
	)
	if err != nil {
		if !isPossiblyDispatched {
			return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure(operation.operationID, sdkgo.FailureTransport,
				"Gmail "+operation.requestName+" request was not dispatched"), 0)
		}
		return client.confirmPossiblySentMessage(call, budget, credentials, operation, message, sdkgo.FailureTransport,
			"Gmail "+operation.requestName+" outcome is unknown", client.receipt(call, "", ""))
	}
	receipt := client.receipt(call, googleRequestID(response.Header), "")
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		closeGmailResponse(response)
		return retryAfterGmailRefusal(response.Header, operation.operationID, sdkgo.FailureRateLimit,
			"Gmail temporarily rejected the "+operation.requestName)
	case response.StatusCode == http.StatusServiceUnavailable:
		closeGmailResponse(response)
		// The contract retries a 503 whether the search misses or fails; only a match changes that.
		if sent, isFound, _ := client.findSentMessageByCallID(budget.attemptContext, call, credentials); isFound {
			return client.sentMessage(call, budget, credentials, operation, message, sent, receipt)
		}
		return retryAfterGmailRefusal(response.Header, operation.operationID, sdkgo.FailureAvailability,
			"Gmail "+operation.requestName+" is temporarily unavailable")
	case response.StatusCode >= 500:
		closeGmailResponse(response)
		return client.confirmPossiblySentMessage(call, budget, credentials, operation, message, sdkgo.FailureAvailability,
			"Gmail "+operation.requestName+" outcome is unknown", receipt)
	case response.StatusCode < 200 || response.StatusCode >= 300:
		closeGmailResponse(response)
		return sdkgo.NewMutationBranch(operation.providerRejectedBranch, SendMessageOutput{},
			gmailOperationFailurePointer(operation.operationID, statusFailureKind(response.StatusCode), operation.rejectionMessage), receipt)
	}
	content, err := readAndCloseGmailResponse(response, client.maxResponseBytes)
	if err != nil {
		return client.confirmPossiblySentMessage(call, budget, credentials, operation, message, sdkgo.FailureTransport,
			"Gmail "+operation.requestName+" response could not be confirmed", receipt)
	}
	var sent sendResponse
	if err := json.Unmarshal(content, &sent); err != nil || sent.ID == "" {
		return client.confirmPossiblySentMessage(call, budget, credentials, operation, message, sdkgo.FailureProtocol,
			"Gmail returned an invalid "+operation.requestName+" response", receipt)
	}
	return client.sentMessage(call, budget, credentials, operation, message, sent, receipt)
}

// confirmPossiblySentMessage returns sent when the read-back finds the message, otherwise uncertain.
func (client *Client) confirmPossiblySentMessage(
	call sdkgo.Call, budget sendAttemptBudget, credentials Credentials, operation gmailSendOperation, message gmailOutgoingMessage,
	kind sdkgo.FailureKind, failureMessage string, receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[SendMessageOutput] {
	sent, isFound, err := client.findSentMessageByCallID(budget.attemptContext, call, credentials)
	if isFound {
		return client.sentMessage(call, budget, credentials, operation, message, sent, receipt)
	}
	if err != nil {
		failureMessage += "; the sent-mail search did not answer"
	} else {
		failureMessage += "; the sent-mail search found no message"
	}
	return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailOperationFailure(operation.operationID, kind, failureMessage), receipt)
}

// sentMessage reads a missing thread ID; if that fails, the message ID still proves the send.
func (client *Client) sentMessage(
	call sdkgo.Call, budget sendAttemptBudget, credentials Credentials, operation gmailSendOperation, message gmailOutgoingMessage,
	sent sendResponse, receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[SendMessageOutput] {
	threadID := sent.ThreadID
	if threadID == "" {
		resource, _, _, err := client.readMessage(budget.attemptContext, call, credentials, sent.ID, "minimal")
		threadID = resource.ThreadID
		if err != nil || threadID == "" {
			threadID = message.requestedThreadID
		}
	}
	receipt.ProviderObjectID = sent.ID
	return sdkgo.NewMutationBranch(operation.sentBranch, SendMessageOutput{
		Sender: credentials.PrimaryEmail, Recipients: message.recipients, MessageID: sent.ID, ThreadID: threadID,
	}, nil, receipt)
}

// findSentMessageByCallID searches Sent Mail for the call's Message-ID; any message found proves the send.
func (client *Client) findSentMessageByCallID(
	ctx context.Context, call sdkgo.Call, credentials Credentials,
) (sendResponse, bool, error) {
	query := url.Values{"q": {sentMessageSearchQuery(call)}, "maxResults": {"1"}}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/users/me/messages?" + query.Encode()
	response, _, err := client.doAuthenticatedRequest(ctx, call, credentials, http.MethodGet, target, nil, "")
	if err != nil {
		return sendResponse{}, false, fmt.Errorf("Gmail sent-message lookup failed: %w", err)
	}
	content, err := readAndCloseGmailResponse(response, client.maxResponseBytes)
	if err != nil || response.StatusCode != http.StatusOK {
		return sendResponse{}, false, fmt.Errorf("Gmail sent-message lookup did not return a usable response")
	}
	var matches sentMessageSearchResponse
	if err := json.Unmarshal(content, &matches); err != nil {
		return sendResponse{}, false, fmt.Errorf("Gmail sent-message lookup returned an invalid response")
	}
	for _, match := range matches.Messages {
		if match.ID != "" {
			return match, true, nil
		}
	}
	return sendResponse{}, false, nil
}

// sentMessageSearchQuery uses Gmail's documented rfc822msgid syntax, which omits the angle brackets.
func sentMessageSearchQuery(call sdkgo.Call) string {
	return "in:sent rfc822msgid:" + rfcMessageIDAddress(call)
}

// rfcMessageIDAddress is the call-stable Message-ID without angle brackets.
func rfcMessageIDAddress(call sdkgo.Call) string {
	return string(call.IdempotencyKey) + "@dex.superdurable.dev"
}

func retryAfterGmailRefusal(header http.Header, operationID string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SendMessageOutput] {
	delay, err := retryAfter(header)
	if err != nil {
		return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure(operationID, sdkgo.FailureProtocol, "Gmail returned an invalid Retry-After header"), 0)
	}
	return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure(operationID, kind, message), delay)
}

// readAndCloseGmailResponse reads at most limit bytes and releases the connection before any read-back.
func readAndCloseGmailResponse(response *http.Response, limit int64) ([]byte, error) {
	defer closeGmailResponse(response)
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("Gmail response exceeds the configured limit")
	}
	return content, nil
}

func closeGmailResponse(response *http.Response) {
	// Closing is best effort; the lookup must not wait for a held send connection.
	_ = response.Body.Close()
}

func (observation *requestDispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure and no obtained connection.
func (observation *requestDispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *requestDispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *requestDispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *requestDispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *requestDispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *requestDispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}
