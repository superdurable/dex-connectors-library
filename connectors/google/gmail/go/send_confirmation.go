// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
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

const (
	// dexMessageIDHeader repeats the Message-ID the connector sets, because Gmail replaces Message-ID on send.
	dexMessageIDHeader = "X-Dex-Message-ID"
	// sentMailCandidateWindow covers the Execute retry window of a send with a wide margin.
	sentMailCandidateWindow = "newer_than:2d"
	sentMailCandidateLimit  = 25
)

// searchableRecipientAddress admits only characters that Gmail search reads literally in a to: term.
var searchableRecipientAddress = regexp.MustCompile(`^[A-Za-z0-9._+-]+@[A-Za-z0-9.-]+$`)

// gmailOutgoingMessage is one encoded messages.send request body and the identity it reports.
type gmailOutgoingMessage struct {
	payload    []byte
	recipients []string
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

// sentMailReadBack matches recent Sent Mail candidates by X-Dex-Message-ID, because Gmail replaces Message-ID.
type sentMailReadBack struct {
	client *Client
	call   sdkgo.Call
	// credentials is the latest credential, which a refresh after a 401 replaces.
	credentials  Credentials
	searchQuery  string
	dexMessageID string
	// mismatchedCandidateIDs skips candidates already read; a message's headers never change.
	mismatchedCandidateIDs map[string]bool
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

func (client *Client) newSentMailReadBack(call sdkgo.Call, credentials Credentials, recipients []string) *sentMailReadBack {
	return &sentMailReadBack{
		client: client, call: call, credentials: credentials, searchQuery: sentMailCandidateQuery(recipients),
		dexMessageID: "<" + rfcMessageIDAddress(call) + ">", mismatchedCandidateIDs: map[string]bool{},
	}
}

// poll repeats find with a short backoff, because Sent Mail search can lag an accepted send.
func (readBack *sentMailReadBack) poll(ctx context.Context) (sendResponse, bool, error) {
	var lastError error
	for lookup := 0; lookup < 4; lookup++ {
		if lookup > 0 {
			backoff := time.Duration(75*(1<<(lookup-1))) * time.Millisecond
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return sendResponse{}, false, ctx.Err()
			case <-timer.C:
			}
		}
		sent, isFound, err := readBack.find(ctx)
		if isFound {
			return sent, true, nil
		}
		lastError = err
		if ctx.Err() != nil {
			return sendResponse{}, false, ctx.Err()
		}
	}
	return sendResponse{}, false, lastError
}

// find reads candidate headers; an unreadable candidate leaves a miss inconclusive.
func (readBack *sentMailReadBack) find(ctx context.Context) (sendResponse, bool, error) {
	candidates, err := readBack.listCandidates(ctx)
	if err != nil {
		return sendResponse{}, false, err
	}
	var candidateError error
	for _, candidate := range candidates {
		if candidate.ID == "" || readBack.mismatchedCandidateIDs[candidate.ID] {
			continue
		}
		sent, isMatch, err := readBack.readCandidate(ctx, candidate.ID)
		if err != nil {
			candidateError = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if isMatch {
			return sent, true, nil
		}
		readBack.mismatchedCandidateIDs[candidate.ID] = true
	}
	return sendResponse{}, false, candidateError
}

func (readBack *sentMailReadBack) listCandidates(ctx context.Context) ([]sendResponse, error) {
	query := url.Values{"q": {readBack.searchQuery}, "maxResults": {strconv.Itoa(sentMailCandidateLimit)}, "includeSpamTrash": {"true"}}
	target := strings.TrimRight(readBack.client.endpoint.String(), "/") + "/users/me/messages?" + query.Encode()
	response, credentials, err := readBack.client.doAuthenticatedRequest(ctx, readBack.call, readBack.credentials, http.MethodGet, target, nil, "")
	readBack.credentials = credentials
	if err != nil {
		return nil, fmt.Errorf("Gmail sent-message lookup failed: %w", err)
	}
	content, err := readAndCloseGmailResponse(response, readBack.client.maxResponseBytes)
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gmail sent-message lookup did not return a usable response")
	}
	var matches sentMessageSearchResponse
	if err := json.Unmarshal(content, &matches); err != nil {
		return nil, fmt.Errorf("Gmail sent-message lookup returned an invalid response")
	}
	return matches.Messages, nil
}

// readCandidate matches X-Dex-Message-ID, or a Message-ID that a compatible gateway kept.
func (readBack *sentMailReadBack) readCandidate(ctx context.Context, messageID string) (sendResponse, bool, error) {
	query := url.Values{"format": {"metadata"}, "metadataHeaders": {"Message-ID", dexMessageIDHeader}}
	target := strings.TrimRight(readBack.client.endpoint.String(), "/") + "/users/me/messages/" + url.PathEscape(messageID) + "?" + query.Encode()
	response, credentials, err := readBack.client.doAuthenticatedRequest(ctx, readBack.call, readBack.credentials, http.MethodGet, target, nil, "")
	readBack.credentials = credentials
	if err != nil {
		return sendResponse{}, false, fmt.Errorf("Gmail sent-message header read failed: %w", err)
	}
	content, err := readAndCloseGmailResponse(response, readBack.client.maxResponseBytes)
	if response.StatusCode == http.StatusNotFound && err == nil {
		// A candidate deleted since the search cannot confirm the send.
		return sendResponse{}, false, nil
	}
	if err != nil || response.StatusCode != http.StatusOK {
		return sendResponse{}, false, fmt.Errorf("Gmail sent-message header read did not return a usable response")
	}
	var resource gmailMessageResource
	if err := json.Unmarshal(content, &resource); err != nil || resource.ID != messageID {
		return sendResponse{}, false, fmt.Errorf("Gmail sent-message header read returned an invalid response")
	}
	for _, header := range resource.Payload.Headers {
		if (strings.EqualFold(header.Name, dexMessageIDHeader) || strings.EqualFold(header.Name, "Message-ID")) &&
			strings.TrimSpace(header.Value) == readBack.dexMessageID {
			return sendResponse{ID: resource.ID, ThreadID: resource.ThreadID}, true, nil
		}
	}
	return sendResponse{}, false, nil
}

// sentMailCandidateQuery adds the first recipient when its address is safe in a to: term.
func sentMailCandidateQuery(recipients []string) string {
	query := "in:sent " + sentMailCandidateWindow
	if len(recipients) == 0 {
		return query
	}
	// Parse only the addr-spec.
	mailbox := recipients[0]
	if start := strings.LastIndex(mailbox, "<"); start >= 0 {
		mailbox = mailbox[start:]
	}
	address, err := mail.ParseAddress(mailbox)
	if err != nil || !searchableRecipientAddress.MatchString(address.Address) {
		return query
	}
	return query + " to:" + address.Address
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
