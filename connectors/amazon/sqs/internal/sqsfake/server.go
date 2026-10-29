// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sqsfake is a stateful, SigV4-verifying fake of the SQS JSON-protocol subset the connector uses:
// ReceiveMessage with visibility timeouts, receive counts, long polling, and FIFO receive-attempt IDs,
// DeleteMessage, and ChangeMessageVisibility. Connector and example tests share it so that both exercise the
// same signed wire format without a real queue.
package sqsfake

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// ErrorMessageSentinel appears in every fake error message, so tests can prove message text never escapes.
const ErrorMessageSentinel = "SENTINEL provider message text"

// QueueOwner is the account ID segment of every fake queue URL.
const QueueOwner = "123456789012"

// defaultVisibilityTimeout is the Amazon SQS default for a new queue.
const defaultVisibilityTimeout = 30 * time.Second

var authorizationPattern = regexp.MustCompile(
	`^AWS4-HMAC-SHA256 Credential=([^/,]+)/(\d{8})/([^/,]+)/sqs/aws4_request, ?SignedHeaders=([a-z0-9;-]+), ?Signature=([0-9a-f]{64})$`)

// Config fixes the credentials, Region, and queues the fake accepts.
type Config struct {
	// AccessKeyID, SecretAccessKey, and SessionToken are the only credentials the fake accepts.
	AccessKeyID, SecretAccessKey, SessionToken string
	// Region is the signing Region the fake accepts.
	Region string
	// Queues lists the queue names that exist; a name ending in .fifo is a FIFO queue. Requests for another
	// queue answer QueueDoesNotExist.
	Queues []string
	// VisibilityTimeout is every queue's visibility timeout; zero uses the 30-second SQS default.
	VisibilityTimeout time.Duration
}

// MessageAttribute is one custom message attribute of a sent message.
type MessageAttribute struct {
	// DataType is String, Number, or Binary, optionally with a custom suffix.
	DataType string
	// StringValue holds a String or Number value.
	StringValue string
	// BinaryValue holds a Binary value.
	BinaryValue []byte
}

// SentMessage is a message a test places on a queue.
type SentMessage struct {
	// Body is the message body.
	Body string
	// MessageAttributes are custom attributes keyed by name.
	MessageAttributes map[string]MessageAttribute
	// MessageGroupID is the FIFO message group; required for a FIFO queue.
	MessageGroupID string
}

// MessageState is the fake's view of one message.
type MessageState struct {
	// MessageID is the message's ID.
	MessageID string
	// Body is the message body.
	Body string
	// ReceiveCount counts receives.
	ReceiveCount int
	// ReceiptHandle is the handle of the latest receive; blank before the first.
	ReceiptHandle string
	// VisibleAt is when the message becomes visible; the zero time means visible now.
	VisibleAt time.Time
	// IsDeleted reports that a DeleteMessage removed the message.
	IsDeleted bool
}

// Request is one request the fake received after its body was read.
type Request struct {
	// Action is the SQS action named by X-Amz-Target, such as ReceiveMessage.
	Action string
	// Header is a copy of the request headers.
	Header http.Header
	// Body is the request body.
	Body []byte
}

// Response overrides the fake's answer to one request.
type Response struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Code is the SQS error shape written in a JSON error body; blank writes no body.
	Code string
	// Message is the error message; blank writes ErrorMessageSentinel.
	Message string
	// Header holds extra response headers.
	Header http.Header
	// RawBody replaces the whole response body when set, for malformed-response cases.
	RawBody []byte
}

// Server is a running fake SQS endpoint. It is safe for concurrent use.
type Server struct {
	*httptest.Server
	config Config
	signer *v4.Signer

	mutex            sync.Mutex
	queues           map[string]*queue
	requests         []Request
	interceptor      func(Request) *Response
	responseDelays   map[string][]time.Duration
	responseHolds    map[string][]chan struct{}
	signatureErrors  int
	nextMessageIndex int
	nextReceiveIndex int
}

type queue struct {
	name           string
	isFIFO         bool
	messages       []*message
	attemptBatches map[string][]*message
	nextSequence   int
}

type message struct {
	state           MessageState
	sentAt          time.Time
	firstReceivedAt time.Time
	attributes      map[string]MessageAttribute
	groupID         string
	sequenceNumber  string
	// staleHandles are earlier receipt handles of this message.
	staleHandles []string
}

// NewServer starts a fake that closes when the test ends.
func NewServer(t testing.TB, config Config) *Server {
	t.Helper()
	if config.VisibilityTimeout == 0 {
		config.VisibilityTimeout = defaultVisibilityTimeout
	}
	server := &Server{config: config, signer: v4.NewSigner(), queues: map[string]*queue{}, responseDelays: map[string][]time.Duration{}, responseHolds: map[string][]chan struct{}{}}
	for _, name := range config.Queues {
		server.queues[name] = &queue{name: name, isFIFO: strings.HasSuffix(name, ".fifo"), attemptBatches: map[string][]*message{}}
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(server.Close)
	return server
}

// QueueURL is the URL of the named queue on this fake.
func (server *Server) QueueURL(name string) string { return server.URL + "/" + QueueOwner + "/" + name }

// Send places a message on a queue, as a producer would, and returns its message ID.
func (server *Server) Send(queueName string, sent SentMessage) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	target := server.queues[queueName]
	if target == nil {
		panic("sqsfake: unknown queue " + queueName)
	}
	server.nextMessageIndex++
	created := &message{
		state:      MessageState{MessageID: fmt.Sprintf("00000000-0000-4000-8000-%012d", server.nextMessageIndex), Body: sent.Body},
		sentAt:     time.Now().UTC(),
		attributes: sent.MessageAttributes,
		groupID:    sent.MessageGroupID,
	}
	if target.isFIFO {
		target.nextSequence++
		created.sequenceNumber = fmt.Sprintf("1862793400000000%04d", target.nextSequence)
	}
	target.messages = append(target.messages, created)
	return created.state.MessageID
}

// Message returns the state of one message.
func (server *Server) Message(queueName string, messageID string) (MessageState, bool) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	for _, candidate := range server.queues[queueName].messages {
		if candidate.state.MessageID == messageID {
			return candidate.state, true
		}
	}
	return MessageState{}, false
}

// RemainingMessageCount counts the messages of a queue that are not deleted.
func (server *Server) RemainingMessageCount(queueName string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	count := 0
	for _, candidate := range server.queues[queueName].messages {
		if !candidate.state.IsDeleted {
			count++
		}
	}
	return count
}

// ExpireVisibility makes every in-flight message of a queue visible again, as if its timeout ended.
func (server *Server) ExpireVisibility(queueName string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	for _, candidate := range server.queues[queueName].messages {
		candidate.state.VisibleAt = time.Time{}
	}
}

// Requests returns every request received so far.
func (server *Server) Requests() []Request {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Request(nil), server.requests...)
}

// RequestCount counts received requests for action; a blank action counts every request.
func (server *Server) RequestCount(action string) int {
	count := 0
	for _, request := range server.Requests() {
		if action == "" || request.Action == action {
			count++
		}
	}
	return count
}

// SignatureErrorCount is the number of requests whose signature did not verify.
func (server *Server) SignatureErrorCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.signatureErrors
}

// Intercept answers matching requests with a fixed response before the fake applies them; a nil result
// lets the fake answer normally.
func (server *Server) Intercept(interceptor func(Request) *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.interceptor = interceptor
}

// DelayResponses delays the response to the next requests for action by the given durations after each
// action is applied, as a server whose success response arrives late.
func (server *Server) DelayResponses(action string, delays ...time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.responseDelays[action] = append(server.responseDelays[action], delays...)
}

// HoldNextResponse blocks one applied action until release or request cancellation.
func (server *Server) HoldNextResponse(action string, release chan struct{}) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.responseHolds[action] = append(server.responseHolds[action], release)
}

func (server *Server) serveHTTP(response http.ResponseWriter, httpRequest *http.Request) {
	body, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		writeError(response, Response{StatusCode: http.StatusBadRequest, Code: "InvalidParameterValue"})
		return
	}
	action, _ := strings.CutPrefix(httpRequest.Header.Get("X-Amz-Target"), "AmazonSQS.")
	request := Request{Action: action, Header: httpRequest.Header.Clone(), Body: body}
	server.mutex.Lock()
	server.requests = append(server.requests, request)
	interceptor := server.interceptor
	server.mutex.Unlock()
	if code := server.verifySignature(httpRequest, body); code != "" {
		server.mutex.Lock()
		server.signatureErrors++
		server.mutex.Unlock()
		writeError(response, Response{StatusCode: http.StatusBadRequest, Code: code})
		return
	}
	if interceptor != nil {
		if override := interceptor(request); override != nil {
			writeError(response, *override)
			return
		}
	}
	if httpRequest.Method != http.MethodPost || httpRequest.URL.Path != "/" ||
		httpRequest.Header.Get("Content-Type") != "application/x-amz-json-1.0" {
		writeError(response, Response{StatusCode: http.StatusBadRequest, Code: "InvalidAction"})
		return
	}
	var answer any
	var failure *Response
	switch action {
	case "ReceiveMessage":
		answer, failure = server.receiveMessage(httpRequest.Context(), body)
	case "DeleteMessage":
		answer, failure = server.deleteMessage(body)
	case "ChangeMessageVisibility":
		answer, failure = server.changeMessageVisibility(body)
	default:
		failure = &Response{StatusCode: http.StatusBadRequest, Code: "InvalidAction"}
	}
	if !server.delayResponse(httpRequest.Context(), action) {
		return
	}
	if failure != nil {
		writeError(response, *failure)
		return
	}
	response.Header().Set("Content-Type", "application/x-amz-json-1.0")
	response.Header().Set("X-Amzn-Requestid", fmt.Sprintf("fake-request-%04d", server.RequestCount("")))
	if err := json.NewEncoder(response).Encode(answer); err != nil {
		panic(err)
	}
}

func (server *Server) delayResponse(ctx context.Context, action string) bool {
	server.mutex.Lock()
	delays := server.responseDelays[action]
	holds := server.responseHolds[action]
	var delay time.Duration
	var hold chan struct{}
	if len(delays) > 0 {
		delay, server.responseDelays[action] = delays[0], delays[1:]
	}
	if len(holds) > 0 {
		hold, server.responseHolds[action] = holds[0], holds[1:]
	}
	server.mutex.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return false
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// verifySignature recomputes the SigV4 signature from the request as received; a blank code means valid.
func (server *Server) verifySignature(request *http.Request, body []byte) string {
	match := authorizationPattern.FindStringSubmatch(request.Header.Get("Authorization"))
	if match == nil || match[1] != server.config.AccessKeyID {
		return "InvalidClientTokenId"
	}
	if match[3] != server.config.Region {
		return "SignatureDoesNotMatch"
	}
	if request.Header.Get("X-Amz-Security-Token") != server.config.SessionToken {
		return "InvalidClientTokenId"
	}
	signingTime, err := time.Parse("20060102T150405Z", request.Header.Get("X-Amz-Date"))
	if err != nil || signingTime.Format("20060102") != match[2] {
		return "IncompleteSignature"
	}
	signedHeaders := strings.Split(match[4], ";")
	for _, required := range []string{"content-type", "host", "x-amz-date", "x-amz-target"} {
		if !contains(signedHeaders, required) {
			return "AccessDenied"
		}
	}
	for name := range request.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-") && !contains(signedHeaders, strings.ToLower(name)) {
			return "AccessDenied"
		}
	}
	rebuilt := &http.Request{
		Method: request.Method, Host: request.Host, Header: http.Header{},
		URL: &url.URL{Scheme: "http", Host: request.Host, Path: request.URL.Path, RawPath: request.URL.EscapedPath(), RawQuery: request.URL.RawQuery},
	}
	for _, name := range signedHeaders {
		switch name {
		case "host":
		case "content-length":
			rebuilt.ContentLength = request.ContentLength
		default:
			rebuilt.Header[http.CanonicalHeaderKey(name)] = request.Header.Values(name)
		}
	}
	digest := sha256.Sum256(body)
	credentials := aws.Credentials{
		AccessKeyID: server.config.AccessKeyID, SecretAccessKey: server.config.SecretAccessKey, SessionToken: server.config.SessionToken,
	}
	if err := server.signer.SignHTTP(context.Background(), credentials, rebuilt, hex.EncodeToString(digest[:]), "sqs", server.config.Region, signingTime); err != nil {
		return "SignatureDoesNotMatch"
	}
	if rebuilt.Header.Get("Authorization") != request.Header.Get("Authorization") {
		return "SignatureDoesNotMatch"
	}
	return ""
}

type receiveMessageRequest struct {
	QueueURL                    string   `json:"QueueUrl"`
	MaxNumberOfMessages         int      `json:"MaxNumberOfMessages"`
	WaitTimeSeconds             int      `json:"WaitTimeSeconds"`
	VisibilityTimeout           *int     `json:"VisibilityTimeout"`
	MessageSystemAttributeNames []string `json:"MessageSystemAttributeNames"`
	MessageAttributeNames       []string `json:"MessageAttributeNames"`
	ReceiveRequestAttemptID     string   `json:"ReceiveRequestAttemptId"`
}

type wireMessage struct {
	MessageID         string                        `json:"MessageId"`
	ReceiptHandle     string                        `json:"ReceiptHandle"`
	MD5OfBody         string                        `json:"MD5OfBody"`
	Body              string                        `json:"Body"`
	Attributes        map[string]string             `json:"Attributes,omitempty"`
	MessageAttributes map[string]wireAttributeValue `json:"MessageAttributes,omitempty"`
}

type wireAttributeValue struct {
	DataType    string `json:"DataType"`
	StringValue string `json:"StringValue,omitempty"`
	BinaryValue []byte `json:"BinaryValue,omitempty"`
}

// receiveMessage long-polls until a message is visible or the wait ends.
func (server *Server) receiveMessage(ctx context.Context, body []byte) (any, *Response) {
	var request receiveMessageRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "InvalidParameterValue"}
	}
	if request.MaxNumberOfMessages < 1 || request.MaxNumberOfMessages > 10 || request.WaitTimeSeconds < 0 || request.WaitTimeSeconds > 20 {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "InvalidParameterValue"}
	}
	deadline := time.Now().Add(time.Duration(request.WaitTimeSeconds) * time.Second)
	for {
		messages, failure := server.receiveVisibleMessages(request)
		if failure != nil || len(messages) > 0 || !time.Now().Before(deadline) {
			return map[string]any{"Messages": messages}, failure
		}
		select {
		case <-ctx.Done():
			return nil, &Response{StatusCode: http.StatusInternalServerError, Code: "InternalError"}
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (server *Server) receiveVisibleMessages(request receiveMessageRequest) ([]wireMessage, *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	target, failure := server.queueFor(request.QueueURL)
	if failure != nil {
		return nil, failure
	}
	if target.isFIFO && request.ReceiveRequestAttemptID != "" {
		if batch, isRepeated := target.attemptBatches[request.ReceiveRequestAttemptID]; isRepeated {
			return server.wireMessages(batch, request), nil
		}
	}
	visibility := server.config.VisibilityTimeout
	if request.VisibilityTimeout != nil {
		visibility = time.Duration(*request.VisibilityTimeout) * time.Second
	}
	now := time.Now()
	var batch []*message
	blockedGroups := map[string]bool{}
	for _, candidate := range target.messages {
		if candidate.state.IsDeleted {
			continue
		}
		isInFlight := now.Before(candidate.state.VisibleAt)
		// A FIFO group with a message in flight delivers nothing until that message is deleted or visible.
		if target.isFIFO && (isInFlight || blockedGroups[candidate.groupID]) {
			blockedGroups[candidate.groupID] = true
			continue
		}
		if isInFlight || len(batch) == request.MaxNumberOfMessages {
			continue
		}
		server.nextReceiveIndex++
		if candidate.state.ReceiptHandle != "" {
			candidate.staleHandles = append(candidate.staleHandles, candidate.state.ReceiptHandle)
		}
		candidate.state.ReceiptHandle = fmt.Sprintf("AQEB%s+receipt/%06d=", candidate.state.MessageID, server.nextReceiveIndex)
		candidate.state.ReceiveCount++
		candidate.state.VisibleAt = now.Add(visibility)
		if candidate.firstReceivedAt.IsZero() {
			candidate.firstReceivedAt = now.UTC()
		}
		batch = append(batch, candidate)
	}
	if target.isFIFO && request.ReceiveRequestAttemptID != "" {
		target.attemptBatches[request.ReceiveRequestAttemptID] = batch
	}
	return server.wireMessages(batch, request), nil
}

func (server *Server) wireMessages(batch []*message, request receiveMessageRequest) []wireMessage {
	wire := make([]wireMessage, 0, len(batch))
	for _, received := range batch {
		digest := md5.Sum([]byte(received.state.Body))
		attributes := map[string]string{
			"ApproximateReceiveCount":          strconv.Itoa(received.state.ReceiveCount),
			"SentTimestamp":                    strconv.FormatInt(received.sentAt.UnixMilli(), 10),
			"ApproximateFirstReceiveTimestamp": strconv.FormatInt(received.firstReceivedAt.UnixMilli(), 10),
		}
		if received.sequenceNumber != "" {
			attributes["MessageGroupId"] = received.groupID
			attributes["MessageDeduplicationId"] = fmt.Sprintf("%x", sha256.Sum256([]byte(received.state.Body)))
			attributes["SequenceNumber"] = received.sequenceNumber
		}
		var messageAttributes map[string]wireAttributeValue
		if contains(request.MessageAttributeNames, "All") {
			for name, value := range received.attributes {
				if messageAttributes == nil {
					messageAttributes = map[string]wireAttributeValue{}
				}
				messageAttributes[name] = wireAttributeValue{DataType: value.DataType, StringValue: value.StringValue, BinaryValue: value.BinaryValue}
			}
		}
		wire = append(wire, wireMessage{
			MessageID: received.state.MessageID, ReceiptHandle: received.state.ReceiptHandle, MD5OfBody: hex.EncodeToString(digest[:]),
			Body: received.state.Body, Attributes: attributes, MessageAttributes: messageAttributes,
		})
	}
	return wire
}

type acknowledgementRequest struct {
	QueueURL          string `json:"QueueUrl"`
	ReceiptHandle     string `json:"ReceiptHandle"`
	VisibilityTimeout *int   `json:"VisibilityTimeout"`
}

// deleteMessage deletes by the current handle. As Amazon SQS documents, a standard queue accepts an outdated
// handle without deleting the message; a repeat with the current handle of a deleted message succeeds.
func (server *Server) deleteMessage(body []byte) (any, *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	var request acknowledgementRequest
	if err := json.Unmarshal(body, &request); err != nil || request.ReceiptHandle == "" {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "MissingParameter"}
	}
	target, failure := server.queueFor(request.QueueURL)
	if failure != nil {
		return nil, failure
	}
	found, isCurrent := target.messageByHandle(request.ReceiptHandle)
	switch {
	case found == nil:
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "ReceiptHandleIsInvalid"}
	case !isCurrent && target.isFIFO && !found.state.IsDeleted:
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "InvalidParameterValue",
			Message: "Value " + ErrorMessageSentinel + " for parameter ReceiptHandle is invalid. Reason: The receipt handle has expired."}
	}
	if isCurrent {
		found.state.IsDeleted = true
	}
	return struct{}{}, nil
}

func (server *Server) changeMessageVisibility(body []byte) (any, *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	var request acknowledgementRequest
	if err := json.Unmarshal(body, &request); err != nil || request.ReceiptHandle == "" || request.VisibilityTimeout == nil {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "MissingParameter"}
	}
	if *request.VisibilityTimeout < 0 || *request.VisibilityTimeout > 43200 {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "InvalidParameterValue"}
	}
	target, failure := server.queueFor(request.QueueURL)
	if failure != nil {
		return nil, failure
	}
	found, isCurrent := target.messageByHandle(request.ReceiptHandle)
	if found == nil {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "ReceiptHandleIsInvalid"}
	}
	if !isCurrent || found.state.IsDeleted || !time.Now().Before(found.state.VisibleAt) {
		return nil, &Response{StatusCode: http.StatusBadRequest, Code: "MessageNotInflight"}
	}
	found.state.VisibleAt = time.Now().Add(time.Duration(*request.VisibilityTimeout) * time.Second)
	return struct{}{}, nil
}

// queueFor resolves a queue URL on this fake.
func (server *Server) queueFor(queueURL string) (*queue, *Response) {
	name, isOwnQueue := strings.CutPrefix(queueURL, server.URL+"/"+QueueOwner+"/")
	if target := server.queues[name]; isOwnQueue && target != nil {
		return target, nil
	}
	return nil, &Response{StatusCode: http.StatusBadRequest, Code: "QueueDoesNotExist"}
}

// messageByHandle finds the message a current or stale handle belongs to.
func (target *queue) messageByHandle(receiptHandle string) (*message, bool) {
	for _, candidate := range target.messages {
		if candidate.state.ReceiptHandle == receiptHandle {
			return candidate, true
		}
		if contains(candidate.staleHandles, receiptHandle) {
			return candidate, false
		}
	}
	return nil, false
}

// writeError writes an SQS JSON-protocol error with the matching legacy query-protocol header.
func writeError(response http.ResponseWriter, override Response) {
	for name, values := range override.Header {
		response.Header()[name] = values
	}
	if override.RawBody != nil {
		response.WriteHeader(override.StatusCode)
		if _, err := response.Write(override.RawBody); err != nil {
			panic(err)
		}
		return
	}
	if override.Code == "" {
		response.WriteHeader(override.StatusCode)
		return
	}
	message := override.Message
	if message == "" {
		message = ErrorMessageSentinel
	}
	response.Header().Set("Content-Type", "application/x-amz-json-1.0")
	response.Header().Set("X-Amzn-Query-Error", queryErrorCode(override.Code)+";Sender")
	response.WriteHeader(override.StatusCode)
	if err := json.NewEncoder(response).Encode(map[string]string{"__type": "com.amazonaws.sqs#" + override.Code, "message": message}); err != nil {
		panic(err)
	}
}

// queryErrorCode is the legacy query-protocol code SQS sends beside a JSON-protocol shape name.
func queryErrorCode(code string) string {
	switch code {
	case "QueueDoesNotExist":
		return "AWS.SimpleQueueService.NonExistentQueue"
	case "MessageNotInflight":
		return "AWS.SimpleQueueService.MessageNotInflight"
	default:
		return code
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
