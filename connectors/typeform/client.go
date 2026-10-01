// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package typeform connects Dex applications to the Typeform Create, Responses, and Webhooks APIs.
//
// Queries list and read forms and list a form's completed responses. The upsertWebhook Mutation creates
// or replaces a form's webhook by tag. The responseSubmitted Trigger serves one webhook endpoint per
// connection, verifies each Typeform-Signature, decodes the submission into typed answers keyed by field
// ref, and records it in each accepting binding's durable inbox before answering 200.
//
// A connection authenticates with a Typeform personal access token. The runnable
// examples/response-recorder application starts one Flow per submitted response, records its answers,
// and reads the form back with getForm to pair every question with its answer.
package typeform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// PersonalAccessTokenAuthMethodID identifies a connection authorized with a Typeform personal access token.
	PersonalAccessTokenAuthMethodID = "personal-access-token"

	// typeformRequestTimeout leaves time within the 30-second Execute timeout to classify a response.
	typeformRequestTimeout = 25 * time.Second
	// typeformTimestampLayout is the UTC form, to the second, that Typeform documents for since and until.
	typeformTimestampLayout = "2006-01-02T15:04:05"
)

// typeformAPIBaseURLs are the only hosts the connector sends a token to, one per Responses Data Center.
var typeformAPIBaseURLs = map[DataCenter]string{
	DataCenterUs:    "https://api.typeform.com",
	DataCenterEu:    "https://api.eu.typeform.com",
	DataCenterNewEu: "https://api.typeform.eu",
}

var (
	// typeformIDPattern matches form IDs, such as u6nXL7, and response tokens.
	typeformIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

	errTypeformRequestInvalid    = errors.New("Typeform request could not be built")
	errTypeformResponseTooLarge  = errors.New("Typeform response exceeds the configured maxResponseBytes")
	errTypeformResponseMalformed = errors.New("Typeform returned a malformed response")

	errTypeformCredentialsUnavailable = errors.New("the Typeform connection could not be loaded yet")
	errTypeformAccessTokenUnusable    = errors.New("the Typeform access token is blank or contains characters a header cannot carry")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient replaces the HTTP client used for Typeform API requests. The connector copies it, never
// follows a redirect, and applies a 25-second timeout when the client sets none.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for receipts and webhook receipt times.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the records of the webhook endpoint and of the durable inboxes that
// NewLocalResponseSubmittedEndpointRunner creates to logger. Without it, those records go to
// slog.Default(). Records carry event IDs only.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Typeform API calls and receives signed Typeform webhooks for one
// connection configuration. It is safe for concurrent use.
type Client struct {
	credentials         sdkgo.CredentialProvider[Credentials]
	httpClient          *http.Client
	now                 func() time.Time
	logger              *slog.Logger
	apiBaseURL          string
	maxResponseBytes    int64
	webhookMaxBodyBytes int64

	responseSubmittedEndpointsMu sync.Mutex
	responseSubmittedEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, FormResponseEvent]
}

// typeformResponse is one bounded Typeform answer. A 2xx body is read only up to maxResponseBytes.
type typeformResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// typeformErrorBody holds the documented machine-readable code of a Typeform error object.
type typeformErrorBody struct {
	Code string `json:"code"`
}

// New validates config and constructs a Client. Blank configuration fields take their manifest defaults.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("Typeform credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Typeform connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("Typeform connector clock is required")
	}
	return &Client{
		credentials: credentials, httpClient: providerhttp.NewProviderHTTPClient(dependencies.httpClient, typeformRequestTimeout),
		now: dependencies.now, logger: dependencies.logger, apiBaseURL: typeformAPIBaseURLs[config.DataCenter],
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		responseSubmittedEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, FormResponseEvent]),
	}, nil
}

// ListForms returns the listForms Query bound to this client.
func (client *Client) ListForms() ListFormsOperation {
	return ListFormsOperation{client: client}
}

// GetForm returns the getForm Query bound to this client.
func (client *Client) GetForm() GetFormOperation {
	return GetFormOperation{client: client}
}

// ListResponses returns the listResponses Query bound to this client.
func (client *Client) ListResponses() ListResponsesOperation {
	return ListResponsesOperation{client: client}
}

// UpsertWebhook returns the upsertWebhook Mutation bound to this client.
func (client *Client) UpsertWebhook() UpsertWebhookOperation {
	return UpsertWebhookOperation{client: client}
}

// resolveCredentials loads the connection's token; a failure never names the cause.
func (client *Client) resolveCredentials(call sdkgo.Call) (Credentials, error) {
	credentials, err := client.credentials.Resolve(call)
	switch {
	case err != nil:
		return Credentials{}, errTypeformCredentialsUnavailable
	case !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()):
		return Credentials{}, errTypeformAccessTokenUnusable
	}
	return credentials, nil
}

// sendTypeformRequest sends one request to the data center's API host with the bearer token.
func (client *Client) sendTypeformRequest(
	requestContext context.Context, credentials Credentials, method string, path string, query url.Values, body any,
) (typeformResponse, error) {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return typeformResponse{}, errTypeformRequestInvalid
		}
		requestBody = bytes.NewReader(encoded)
	}
	target := client.apiBaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	if requestContext == nil {
		requestContext = context.Background()
	}
	request, err := http.NewRequestWithContext(requestContext, method, target, requestBody)
	if err != nil {
		return typeformResponse{}, errTypeformRequestInvalid
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return typeformResponse{}, err
	}
	defer response.Body.Close()
	result := typeformResponse{statusCode: response.StatusCode, header: response.Header.Clone()}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Only the error code is read, so a truncated or unreadable error body keeps its status.
		result.body, _ = io.ReadAll(io.LimitReader(response.Body, providerhttp.MaxErrorBodyBytes))
		return result, nil
	}
	result.body, err = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return result, errTypeformResponseTooLarge
	}
	return result, err
}

// typeformOutcome classifies a non-2xx answer: a Retry, or a conclusive failure the caller maps to a branch.
type typeformOutcome struct {
	isRetry    bool
	retryAfter time.Duration
	failure    sdkgo.Failure
}

// classifyTypeformFailure retries 408, 429, and 5xx; messages are the connector's, never Typeform's.
func (client *Client) classifyTypeformFailure(operationID string, response typeformResponse) typeformOutcome {
	status := response.statusCode
	switch {
	case status == http.StatusTooManyRequests:
		return typeformOutcome{
			isRetry: true, retryAfter: providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()),
			failure: typeformFailure(operationID, sdkgo.FailureRateLimit, "Typeform rate limited the request; it allows two requests per second per account"),
		}
	case status == http.StatusRequestTimeout || status >= 500:
		return typeformOutcome{
			isRetry: true,
			failure: typeformFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Typeform answered HTTP %d", status)),
		}
	case status == http.StatusUnauthorized:
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureAuthentication,
			"Typeform rejected the access token; generate a new personal access token")}
	case status == http.StatusForbidden:
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureAuthorization,
			"Typeform denied the request; check that the token is valid and has the scopes in the connector guide")}
	case status == http.StatusPaymentRequired:
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureProviderRejection,
			"the Typeform account's plan does not include a feature this request uses")}
	case status == http.StatusNotFound:
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureNotFound, "the Typeform form was not found")}
	case status == http.StatusConflict:
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureConflict, "Typeform reported a conflicting change")}
	case status == http.StatusBadRequest && typeformErrorCode(response.body) == "webhook_url_https_required":
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureValidation, "Typeform requires an HTTPS webhook URL")}
	default:
		return typeformOutcome{failure: typeformFailure(operationID, sdkgo.FailureProviderRejection,
			fmt.Sprintf("Typeform rejected the request with HTTP %d", status))}
	}
}

// typeformErrorCode returns the documented snake_case code of an error body, never its description.
func typeformErrorCode(body []byte) string {
	var decoded typeformErrorBody
	if json.Unmarshal(body, &decoded) != nil {
		return ""
	}
	return decoded.Code
}

func (client *Client) typeformReceipt(objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{Provider: ConnectorID, ProviderObjectID: objectID, ObservedAt: client.now().UTC()}
}

func typeformFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operationID, Message: message}
}

func typeformFailurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := typeformFailure(operationID, kind, message)
	return &failure
}

// responseFailureKind names a read or decode failure: an oversized body or a malformed one.
func responseFailureKind(err error) sdkgo.FailureKind {
	if errors.Is(err, errTypeformResponseTooLarge) {
		return sdkgo.FailureResponseTooLarge
	}
	return sdkgo.FailureProtocol
}

func responseFailureMessage(err error) string {
	if errors.Is(err, errTypeformResponseTooLarge) {
		return errTypeformResponseTooLarge.Error()
	}
	return errTypeformResponseMalformed.Error()
}

// decodeTypeformJSON decodes a 2xx body; unknown fields are ignored because Typeform adds fields over time.
func decodeTypeformJSON(body []byte, destination any) error {
	if len(body) == 0 || json.Unmarshal(body, destination) != nil {
		return errTypeformResponseMalformed
	}
	return nil
}

// validateFormID accepts a Typeform form ID, the part after /to/ in a form link.
func validateFormID(formID string) error {
	if !typeformIDPattern.MatchString(formID) {
		return fmt.Errorf("formId must be a Typeform form ID such as u6nXL7, the part after /to/ in the form link")
	}
	return nil
}

// parseTypeformTimestamp reads an RFC 3339 timestamp and returns it in UTC.
func parseTypeformTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errTypeformResponseMalformed
	}
	return parsed.UTC(), nil
}

// parseOptionalTypeformTimestamp treats an absent value and Typeform's 0001-01-01 placeholder as unset.
func parseOptionalTypeformTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := parseTypeformTimestamp(value)
	if err != nil || parsed.Year() <= 1 {
		return time.Time{}, err
	}
	return parsed, nil
}
