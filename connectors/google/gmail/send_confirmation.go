// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
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
	payload          []byte
	recipients       []string
	hasPreSendSearch bool
	// requestedThreadID is the thread a reply asked for; empty for a new message.
	requestedThreadID string
}

// sendAttemptBudget reserves time for read-back and the Result write before Execute expires.
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

// requestDispatchObservation records whether a traced connection failure proves the request was never sent.
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

// sentMessageSearchQuery uses Gmail's documented rfc822msgid syntax, which omits the angle brackets.
func sentMessageSearchQuery(call sdkgo.Call) string {
	return "in:sent rfc822msgid:" + rfcMessageIDAddress(call)
}

// rfcMessageIDAddress includes the run start to separate reused Flow IDs.
func rfcMessageIDAddress(call sdkgo.Call) string {
	runIdentity := call.Context.RunID() + "\x00" + strconv.FormatInt(call.Context.FlowStartedAt().Unix(), 10)
	digest := sha256.Sum256([]byte(string(call.IdempotencyKey) + "\x00" + runIdentity))
	return hex.EncodeToString(digest[:]) + "@dex.superdurable.dev"
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
	if response.Body == nil {
		return nil, fmt.Errorf("Gmail response has no body")
	}
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
	if response.Body == nil {
		return
	}
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
