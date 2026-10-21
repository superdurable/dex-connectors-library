// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sqs consumes messages from Amazon SQS standard and FIFO queues, and from SQS-compatible servers
// such as ElasticMQ, as Dex connector Steps.
//
// A consuming Flow receives a batch with ReceiveMessages, processes each message, and then acknowledges it
// with DeleteMessage, or releases it back to the queue, or hides it for longer, with ChangeMessageVisibility.
// A message that the Flow never acknowledges reappears on the queue when its visibility timeout ends, so a
// failed Flow leaves its messages for another consumer or for the queue's dead-letter redrive policy.
//
// Every request uses the SQS JSON protocol and is signed with AWS Signature Version 4 by the official signer
// in github.com/aws/aws-sdk-go-v2/aws/signer/v4, from a static access key, an optional session token, and the
// configured Region. The connector sends each request itself, so it reads every response within its own
// limits, never follows a redirect, and leaves retries to Dex.
//
// Every operation is safe to repeat. A repeated receive of a standard queue may hide further messages,
// which reappear when their visibility timeout ends; a repeated receive of a FIFO queue sends the same
// ReceiveRequestAttemptId and gets the same batch back. A repeated deletion leaves the message deleted, though
// a server that forgets the handle, such as ElasticMQ, answers the repeat with receiptHandleInvalid. Setting a
// visibility timeout again restarts the timeout from the later attempt.
//
// Applications use the generated operation-specific Step factories, such as NewReceiveMessagesStep and
// NewDeleteMessageStep, with a Connection built by NewProjectConnection or NewConnection. The runnable
// example in examples/queue-consumer uses every operation in one Flow.
package sqs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "amazon-sqs"
	// signingServiceName is the SigV4 service name for Amazon SQS and SQS-compatible servers.
	signingServiceName = "sqs"
	// operationDeadline keeps the one request of every Invoke inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// requestTimeout covers a 20-second long poll and the batch transfer after it.
	requestTimeout = operationDeadline
	// maxMessagesPerReceiveLimit is the Amazon SQS ReceiveMessage batch limit.
	maxMessagesPerReceiveLimit = 10
	// maxWaitTimeSeconds is the Amazon SQS long-polling limit.
	maxWaitTimeSeconds = 20
	// maxVisibilityTimeoutSeconds is the Amazon SQS visibility timeout limit of 12 hours.
	maxVisibilityTimeoutSeconds = 43200

	jsonProtocolContentType = "application/x-amz-json-1.0"
	targetHeader            = "X-Amz-Target"
	targetPrefix            = "AmazonSQS."
	requestIDHeader         = "X-Amzn-Requestid"
	// queryErrorHeader carries the legacy query-protocol error code, such as AWS.SimpleQueueService.NonExistentQueue;Sender.
	queryErrorHeader = "X-Amzn-Query-Error"
)

var (
	// errorCodePattern bounds an SQS error code to a machine token, so provider message text never escapes.
	errorCodePattern = regexp.MustCompile(`^[A-Za-z0-9.]{1,96}$`)
	// regionPattern accepts AWS Region codes and the names SQS-compatible servers use, such as elasticmq.
	regionPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	// requestIDPattern accepts the request IDs Amazon SQS and compatible servers return.
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9+/=._-]{1,128}$`)
	// endpointPathPattern keeps a custom endpoint path free of characters that would need escaping.
	endpointPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)*$`)
	// receiptHandleMessagePattern recognizes an InvalidParameterValue that names the receipt handle, such as an
	// expired FIFO handle; the message itself never leaves the connector.
	receiptHandleMessagePattern = regexp.MustCompile(`(?i)receipt ?handle`)
)

// awsPartitionDNSSuffixes copies the AWS SDK partition table; ISO-partition Regions must set endpoint.
var awsPartitionDNSSuffixes = []struct {
	regionPattern *regexp.Regexp
	dnsSuffix     string
}{
	{regexp.MustCompile(`^(us|eu|ap|sa|ca|me|af|il|mx)-\w+-\d+$`), "amazonaws.com"},
	{regexp.MustCompile(`^us-gov-\w+-\d+$`), "amazonaws.com"},
	{regexp.MustCompile(`^cn-\w+-\d+$`), "amazonaws.com.cn"},
	{regexp.MustCompile(`^eusc-de-\w+-\d+$`), "amazonaws.eu"},
}

// errRequestNotBuilt reports a request that could not be constructed or signed, so nothing was sent.
var errRequestNotBuilt = errors.New("SQS request could not be built")

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for SQS requests. The caller keeps ownership of client and its
// transport. Requests use a copy that never follows redirects, and each exchange is bounded by 25 seconds so
// a 20-second long poll fits inside Dex's 30-second Execute timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client signs and sends SQS JSON-protocol requests for one connection. It is immutable after New and safe
// for concurrent use by several Steps.
type Client struct {
	endpoint              *url.URL
	isAmazonEndpoint      bool
	region                string
	defaultQueueURL       string
	maxMessagesPerReceive int
	receiveWaitSeconds    int
	maxResponseBytes      int64
	httpClient            *http.Client
	credentials           sdkgo.CredentialProvider[Credentials]
	signer                *v4.Signer
	now                   func() time.Time
}

// operationSession carries the resolved credential and deadline of one Invoke.
type operationSession struct {
	context     context.Context
	call        sdkgo.Call
	credentials Credentials
}

// providerErrorResponse is the safe part of one non-2xx SQS response.
type providerErrorResponse struct {
	statusCode int
	code       string
	requestID  string
	retryAfter time.Duration
	// isAboutReceiptHandle reports that an InvalidParameterValue message named the receipt handle.
	isAboutReceiptHandle bool
}

// requestOutcome is the safe classification of one failed exchange, before an operation picks its branch.
type requestOutcome uint8

const (
	outcomeRetry requestOutcome = iota + 1
	outcomeReceiptHandleInvalid
	outcomeRejected
	outcomeDefect
)

// classifiedFailure is one failed exchange mapped onto a retry or a branch category.
type classifiedFailure struct {
	outcome    requestOutcome
	failure    sdkgo.Failure
	retryAfter time.Duration
	requestID  string
}

// sqsErrorDocument is the JSON error body SQS returns for every failed request.
type sqsErrorDocument struct {
	Type    string `json:"__type"`
	Message string `json:"message"`
}

// New validates configuration and constructs a client. It fails when the Region does not fit the endpoint,
// the endpoint is not HTTPS (loopback HTTP is accepted for a local server), a limit is outside its documented
// range, the default queue URL is invalid, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	if !regionPattern.MatchString(config.Region) {
		return nil, errors.New("SQS region must be a Region code such as eu-west-1")
	}
	endpoint, isAmazonEndpoint, err := resolveEndpoint(config.Endpoint, config.Region)
	if err != nil {
		return nil, err
	}
	if config.DefaultQueueURL != "" {
		if _, err := parseQueueURL(config.DefaultQueueURL); err != nil {
			return nil, fmt.Errorf("SQS defaultQueueUrl: %w", err)
		}
	}
	if config.MaxMessagesPerReceive < 1 || config.MaxMessagesPerReceive > maxMessagesPerReceiveLimit {
		return nil, fmt.Errorf("SQS maxMessagesPerReceive must be from 1 to %d", maxMessagesPerReceiveLimit)
	}
	if config.ReceiveWaitSeconds < 1 || config.ReceiveWaitSeconds > maxWaitTimeSeconds {
		return nil, fmt.Errorf("SQS receiveWaitSeconds must be from 1 to %d", maxWaitTimeSeconds)
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("SQS maxResponseBytes must be positive")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("SQS connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		endpoint: endpoint, isAmazonEndpoint: isAmazonEndpoint, region: config.Region, defaultQueueURL: config.DefaultQueueURL,
		maxMessagesPerReceive: int(config.MaxMessagesPerReceive), receiveWaitSeconds: int(config.ReceiveWaitSeconds),
		maxResponseBytes: config.MaxResponseBytes,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials:      credentials,
		signer:           v4.NewSigner(),
		now:              time.Now,
	}, nil
}

// ReceiveMessages returns the receiveMessages Mutation bound to this client.
func (client *Client) ReceiveMessages() ReceiveMessagesOperation {
	return ReceiveMessagesOperation{client: client}
}

// DeleteMessage returns the deleteMessage Mutation bound to this client.
func (client *Client) DeleteMessage() DeleteMessageOperation {
	return DeleteMessageOperation{client: client}
}

// ChangeMessageVisibility returns the changeMessageVisibility Mutation bound to this client.
func (client *Client) ChangeMessageVisibility() ChangeMessageVisibilityOperation {
	return ChangeMessageVisibilityOperation{client: client}
}

// startSession resolves the credential, reread before every call; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string) (*operationSession, context.CancelFunc, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return nil, nil, failurePointer(operationID, sdkgo.FailureAuthentication,
			"SQS connection credentials are unavailable; save an access key ID and secret access key for the connection")
	}
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	return &operationSession{context: operationContext, call: call, credentials: credentials}, cancel, nil
}

// resolveQueue returns the input queue URL, or the connection's default queue when the input leaves it blank.
func (client *Client) resolveQueue(queueURL string) (queueAddress, error) {
	if queueURL == "" {
		queueURL = client.defaultQueueURL
	}
	if queueURL == "" {
		return queueAddress{}, errors.New("queueUrl is required because the connection has no defaultQueueUrl")
	}
	queue, err := parseQueueURL(queueURL)
	if err != nil {
		return queueAddress{}, err
	}
	if client.isAmazonEndpoint && queue.amazonRegion != "" && queue.amazonRegion != client.region {
		return queueAddress{}, fmt.Errorf("queueUrl is in Region %s but the connection signs for %s; set region to %s",
			queue.amazonRegion, client.region, queue.amazonRegion)
	}
	return queue, nil
}

// exchange signs and sends one JSON-protocol action. The caller owns and must close the returned response body.
func (client *Client) exchange(session *operationSession, action string, requestBody any) (*http.Response, error) {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest, err := http.NewRequestWithContext(session.context, http.MethodPost, client.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Content-Type", jsonProtocolContentType)
	httpRequest.Header.Set(targetHeader, targetPrefix+action)
	digest := sha256.Sum256(body)
	if err := client.signRequest(httpRequest, session.credentials, hex.EncodeToString(digest[:]), client.now()); err != nil {
		return nil, errRequestNotBuilt
	}
	return client.httpClient.Do(httpRequest)
}

// signRequest adds the SigV4 Authorization header, and X-Amz-Security-Token for temporary credentials.
func (client *Client) signRequest(request *http.Request, credentials Credentials, payloadHash string, signingTime time.Time) error {
	signingCredentials := aws.Credentials{
		AccessKeyID: credentials.AccessKeyID.Reveal(), SecretAccessKey: credentials.SecretAccessKey.Reveal(),
		SessionToken: credentials.SessionToken.Reveal(),
	}
	return client.signer.SignHTTP(request.Context(), signingCredentials, request, payloadHash, signingServiceName, client.region, signingTime.UTC())
}

// readErrorResponse reads at most 64 KiB of a non-2xx body and keeps only its validated error code.
func (client *Client) readErrorResponse(response *http.Response, credentials Credentials) providerErrorResponse {
	errorResponse := providerErrorResponse{
		statusCode: response.StatusCode, requestID: safeRequestID(response.Header),
		retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.now()),
	}
	// The query-protocol code, when present, names the same error in the form older SDKs report.
	if queryCode, _, _ := strings.Cut(response.Header.Get(queryErrorHeader), ";"); errorCodePattern.MatchString(queryCode) &&
		!containsCredential(queryCode, credentials) {
		errorResponse.code = queryCode
	}
	// An oversized or unreadable error body carries no usable code, so the status alone classifies it.
	contents, err := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
	if err != nil || len(contents) == 0 {
		return errorResponse
	}
	var document sqsErrorDocument
	if json.Unmarshal(contents, &document) != nil {
		return errorResponse
	}
	if code := errorTypeCode(document.Type); errorCodePattern.MatchString(code) && !containsCredential(code, credentials) {
		errorResponse.code = code
	}
	errorResponse.isAboutReceiptHandle = receiptHandleMessagePattern.MatchString(document.Message)
	return errorResponse
}

// classifyErrorResponse maps a non-2xx response onto Retry or a branch category, by status and error code.
func classifyErrorResponse(operationID string, subject string, response providerErrorResponse) classifiedFailure {
	classified := classifiedFailure{requestID: response.requestID, retryAfter: response.retryAfter}
	status, code := response.statusCode, response.code
	switch {
	case status == http.StatusTooManyRequests || isThrottlingErrorCode(code):
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureRateLimit, "SQS throttled the "+subject)
	case code == "RequestTimeout" || status == http.StatusRequestTimeout:
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureTransport, "SQS timed out the "+subject)
	case status >= 500 && status != http.StatusNotImplemented:
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureAvailability, statusMessage("SQS is temporarily unable to serve the "+subject, response))
	case isReceiptHandleErrorCode(code) || (isInvalidParameterErrorCode(code) && response.isAboutReceiptHandle):
		classified.outcome, classified.failure = outcomeReceiptHandleInvalid, newFailure(operationID, sdkgo.FailureNotFound,
			statusMessage("SQS no longer accepts the receipt handle for the "+subject, response)+"; the message may be delivered again")
	case isQueueMissingErrorCode(code):
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureNotFound,
			statusMessage("SQS rejected the "+subject, response)+" because the queue does not exist; check queueUrl and region")
	case code == "OverLimit" || code == "AWS.SimpleQueueService.OverLimit":
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureProviderRejection,
			statusMessage("SQS rejected the "+subject, response)+" because the queue holds too many in-flight messages; delete or release received messages")
	case code == "RequestTimeTooSkewed" || code == "SignatureExpired":
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureAuthentication,
			"SQS rejected the "+subject+" because the Worker clock differs from AWS by more than 5 minutes")
	case isCredentialErrorCode(code) || status == http.StatusUnauthorized:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureAuthentication,
			statusMessage("SQS rejected the connection credentials for the "+subject, response)+"; replace the access key or session token")
	case isAuthorizationErrorCode(code) || status == http.StatusForbidden:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureAuthorization, statusMessage("SQS denied the "+subject, response))
	default:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureProviderRejection, statusMessage("SQS rejected the "+subject, response))
	}
	return classified
}

func (client *Client) receipt(call sdkgo.Call, requestID string, providerObjectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: providerObjectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

// mutationAttemptForFailure maps a classified failure to Retry or a branch; blank receiptHandleInvalid means rejected.
func mutationAttemptForFailure[T any](classified classifiedFailure, receipt sdkgo.Receipt, receiptHandleInvalid, rejected, defect sdkgo.BranchID, value T) sdkgo.MutationAttempt[T] {
	failure := classified.failure
	switch {
	case classified.outcome == outcomeRetry:
		return sdkgo.NewMutationRetry[T](failure, classified.retryAfter)
	case classified.outcome == outcomeReceiptHandleInvalid && receiptHandleInvalid != "":
		return sdkgo.NewMutationBranch(receiptHandleInvalid, value, &failure, receipt)
	case classified.outcome == outcomeDefect:
		return sdkgo.NewMutationBranch(defect, value, &failure, receipt)
	default:
		return sdkgo.NewMutationBranch(rejected, value, &failure, receipt)
	}
}

// sendAcknowledgement sends one DeleteMessage or ChangeMessageVisibility action, whose success has no body.
func (client *Client) sendAcknowledgement(session *operationSession, operationID string, subject string, action string, requestBody any) (string, *classifiedFailure) {
	response, err := client.exchange(session, action, requestBody)
	if errors.Is(err, errRequestNotBuilt) {
		return "", &classifiedFailure{outcome: outcomeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, err.Error())}
	}
	if err != nil {
		// Repeating either action converges, so an unknown outcome is safe to retry.
		return "", &classifiedFailure{outcome: outcomeRetry, failure: transportFailure(operationID, subject)}
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(operationID, subject, client.readErrorResponse(response, session.credentials))
		return classified.requestID, &classified
	}
	// A 2xx status alone confirms the action; its body is empty or {} and is not read.
	return safeRequestID(response.Header), nil
}

// closeResponseBody releases the connection after the body was read or deliberately abandoned.
func closeResponseBody(response *http.Response) {
	// A close failure cannot change an outcome that was already classified.
	_ = response.Body.Close()
}

// resolveEndpoint validates a custom endpoint or derives the Amazon SQS Regional endpoint from region.
func resolveEndpoint(configured string, region string) (*url.URL, bool, error) {
	isAmazonEndpoint := configured == ""
	if isAmazonEndpoint {
		dnsSuffix := ""
		for _, partition := range awsPartitionDNSSuffixes {
			if partition.regionPattern.MatchString(region) {
				dnsSuffix = partition.dnsSuffix
				break
			}
		}
		if dnsSuffix == "" {
			return nil, false, errors.New("SQS region is not an Amazon SQS Region; set endpoint for an SQS-compatible server")
		}
		configured = "https://sqs." + region + "." + dnsSuffix
	}
	validated, err := providerhttp.ValidateBaseURL(configured)
	if err != nil {
		return nil, false, fmt.Errorf("SQS endpoint: %w", err)
	}
	parsed, err := url.Parse(validated)
	if err != nil || !endpointPathPattern.MatchString(parsed.Path) || parsed.RawPath != "" {
		return nil, false, errors.New("SQS endpoint path may contain only letters, digits, and - . _ ~ segments")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed, isAmazonEndpoint, nil
}

// errorTypeCode keeps the shape name of a JSON-protocol __type, such as QueueDoesNotExist from
// com.amazonaws.sqs#QueueDoesNotExist, without a trailing documentation URL.
func errorTypeCode(errorType string) string {
	if _, shapeName, isQualified := strings.Cut(errorType, "#"); isQualified {
		errorType = shapeName
	}
	code, _, _ := strings.Cut(errorType, ":")
	return code
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func isThrottlingErrorCode(code string) bool {
	switch code {
	case "RequestThrottled", "ThrottlingException", "Throttling", "KmsThrottled", "AWS.SimpleQueueService.RequestThrottled":
		return true
	default:
		return false
	}
}

func isReceiptHandleErrorCode(code string) bool {
	switch code {
	case "ReceiptHandleIsInvalid", "InvalidIdFormat", "MessageNotInflight", "AWS.SimpleQueueService.MessageNotInflight":
		return true
	default:
		return false
	}
}

func isInvalidParameterErrorCode(code string) bool {
	return code == "InvalidParameterValue" || code == "InvalidParameterValueException"
}

func isQueueMissingErrorCode(code string) bool {
	return code == "QueueDoesNotExist" || code == "AWS.SimpleQueueService.NonExistentQueue"
}

// isCredentialErrorCode reports codes for an unknown key, a bad signature, or an expired or invalid token.
func isCredentialErrorCode(code string) bool {
	switch code {
	case "InvalidClientTokenId", "UnrecognizedClientException", "SignatureDoesNotMatch", "IncompleteSignature", "ExpiredToken",
		"InvalidSecurity", "MissingAuthenticationToken", "InvalidAccessKeyId", "AWS.SimpleQueueService.InvalidSecurity":
		return true
	default:
		return false
	}
}

// isAuthorizationErrorCode reports a missing IAM permission or a KMS key the queue cannot use.
func isAuthorizationErrorCode(code string) bool {
	switch code {
	case "AccessDenied", "AccessDeniedException", "KmsAccessDenied", "KmsDisabled", "KmsInvalidState", "KmsNotFound",
		"KmsOptInRequired", "KmsInvalidKeyUsage":
		return true
	default:
		return false
	}
}

// containsCredential reports whether value repeats the secret access key or session token.
func containsCredential(value string, credentials Credentials) bool {
	for _, secret := range []string{credentials.SecretAccessKey.Reveal(), credentials.SessionToken.Reveal()} {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

// statusMessage names the HTTP status and, when SQS sent one, its validated error code, never its message text.
func statusMessage(prefix string, response providerErrorResponse) string {
	if response.code != "" {
		return fmt.Sprintf("%s with HTTP %d %s", prefix, response.statusCode, response.code)
	}
	return fmt.Sprintf("%s with HTTP %d", prefix, response.statusCode)
}

func safeRequestID(header http.Header) string {
	if requestID := header.Get(requestIDHeader); requestIDPattern.MatchString(requestID) {
		return requestID
	}
	return ""
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operationID, kind, message)
	return &failure
}

func transportFailure(operationID string, subject string) sdkgo.Failure {
	return newFailure(operationID, sdkgo.FailureTransport, "SQS could not be reached or did not finish the "+subject)
}

func defectFailure(operationID string, err error) *sdkgo.Failure {
	return failurePointer(operationID, sdkgo.FailureValidation, err.Error())
}
