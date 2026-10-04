// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package calendly connects Dex applications to the Calendly API v2.
//
// Queries read scheduled events and their invitees. Mutations cancel a scheduled event, create a
// single-use scheduling link, and register a webhook subscription. The inviteeEventReceived Trigger
// serves one webhook endpoint per connection, verifies each Calendly-Webhook-Signature, and records
// every invitee.created and invitee.canceled event in each accepting binding's durable inbox before
// answering 200.
//
// A connection authenticates with a personal access token or with Calendly OAuth; an OAuth access
// token is refreshed before its two-hour expiry. The runnable examples/invitee-recorder application
// starts one Flow per booked invitee and records the scheduled event it reads back.
package calendly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// PersonalAccessTokenAuthMethodID identifies a connection authorized with a Calendly personal access token.
	PersonalAccessTokenAuthMethodID = "personal-access-token"
	// CalendlyOAuthAuthMethodID identifies a connection authorized through a Calendly OAuth app.
	CalendlyOAuthAuthMethodID = "calendly-oauth"

	// calendlyAPIBaseURL is the only host the connector sends a token to.
	calendlyAPIBaseURL = "https://api.calendly.com"
	// calendlyRequestTimeout leaves time within the 30-second Execute timeout to classify a response.
	calendlyRequestTimeout = 25 * time.Second
	// maximumPageSize is the largest count Calendly accepts on a list endpoint.
	maximumPageSize = 100
	// calendlyTimestampLayout is the UTC form Calendly documents for time filters.
	calendlyTimestampLayout = "2006-01-02T15:04:05.000000Z"
)

var (
	calendlyResourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	calendlyPageTokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_\-.~=+/]{1,512}$`)

	errCalendlyRequestInvalid    = errors.New("Calendly request could not be built")
	errCalendlyResponseTooLarge  = errors.New("Calendly response exceeds the configured maxResponseBytes")
	errCalendlyResponseMalformed = errors.New("Calendly returned a malformed response")

	errCalendlyReauthorizationRequired = errors.New("the Calendly connection needs reauthorization; authorize it again in Dex Web Connectors")
	errCalendlyCredentialsUnavailable  = errors.New("the Calendly access token could not be loaded or refreshed yet")
	errCalendlyAccessTokenUnusable     = errors.New("the Calendly access token is blank or contains characters a header cannot carry")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient replaces the HTTP client used for Calendly API and token requests. The connector copies
// it, never follows a redirect, and applies a 25-second timeout when the client sets none.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for receipts, credential expiry, and webhook signature tolerance.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the records of the durable inboxes that NewProjectInviteeEventReceivedEndpointRunner
// creates to logger. Without it, those records go to slog.Default(). Records carry event IDs only.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Calendly API v2 calls and receives signed Calendly webhooks for one
// connection configuration. It is safe for concurrent use.
type Client struct {
	credentials               CredentialSource
	refreshDriver             *CredentialRefreshDriver
	httpClient                *http.Client
	now                       func() time.Time
	logger                    *slog.Logger
	maxResponseBytes          int64
	webhookMaxBodyBytes       int64
	webhookSignatureTolerance time.Duration

	inviteeEventEndpointsMu sync.Mutex
	inviteeEventEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, InviteeEvent]
}

// calendlyResponse is one bounded Calendly answer. The body is read only up to maxResponseBytes.
type calendlyResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// calendlyErrorBody holds the documented machine-readable fields of a Calendly error object.
type calendlyErrorBody struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// calendlyCurrentUser is the part of GET /users/me that scopes requests to the connected user.
type calendlyCurrentUser struct {
	userURI         string
	organizationURI string
}

// New validates config and constructs a Client. Blank configuration fields take their manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("Calendly credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Calendly connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("Calendly connector clock is required")
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, calendlyRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		credentials: credentials, refreshDriver: refreshDriver, httpClient: httpClient,
		now: dependencies.now, logger: dependencies.logger,
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		webhookSignatureTolerance: config.WebhookSignatureTolerance,
		inviteeEventEndpoints:     make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, InviteeEvent]),
	}, nil
}

// ListScheduledEvents returns the listScheduledEvents Query bound to this client.
func (client *Client) ListScheduledEvents() ListScheduledEventsOperation {
	return ListScheduledEventsOperation{client: client}
}

// GetScheduledEvent returns the getScheduledEvent Query bound to this client.
func (client *Client) GetScheduledEvent() GetScheduledEventOperation {
	return GetScheduledEventOperation{client: client}
}

// ListEventInvitees returns the listEventInvitees Query bound to this client.
func (client *Client) ListEventInvitees() ListEventInviteesOperation {
	return ListEventInviteesOperation{client: client}
}

// CancelScheduledEvent returns the cancelScheduledEvent Mutation bound to this client.
func (client *Client) CancelScheduledEvent() CancelScheduledEventOperation {
	return CancelScheduledEventOperation{client: client}
}

// CreateSchedulingLink returns the createSchedulingLink Mutation bound to this client.
func (client *Client) CreateSchedulingLink() CreateSchedulingLinkOperation {
	return CreateSchedulingLinkOperation{client: client}
}

// CreateWebhookSubscription returns the createWebhookSubscription Mutation bound to this client.
func (client *Client) CreateWebhookSubscription() CreateWebhookSubscriptionOperation {
	return CreateWebhookSubscriptionOperation{client: client}
}

// resolveCredentials refreshes an expiring OAuth token before use; a failure never names the cause.
func (client *Client) resolveCredentials(call sdkgo.Call) (Credentials, error) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, errCalendlyReauthorizationRequired
	case err != nil:
		return Credentials{}, errCalendlyCredentialsUnavailable
	case !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()):
		return Credentials{}, errCalendlyAccessTokenUnusable
	}
	return credentials, nil
}

// webhookCredentialProvider refreshes an expired OAuth token, which the credential provider otherwise refuses to
// return, so the endpoint can still read the signing key.
type webhookCredentialProvider struct{ client *Client }

// Resolve resolves the endpoint's credentials without a request context.
func (provider webhookCredentialProvider) Resolve(call sdkgo.Call) (Credentials, error) {
	return provider.ResolveContext(context.Background(), call)
}

// ResolveContext resolves the endpoint's credentials through the refresh driver.
func (provider webhookCredentialProvider) ResolveContext(ctx context.Context, call sdkgo.Call) (Credentials, error) {
	return sdkgo.ResolveCredential(ctx, provider.client.credentials, call, provider.client.refreshDriver)
}

// sendCalendlyRequest repeats a request once after an OAuth 401 and a forced refresh; Calendly ignored it.
func (client *Client) sendCalendlyRequest(
	call sdkgo.Call, credentials *Credentials, method string, path string, query url.Values, body any, isDispatched *atomic.Bool,
) (calendlyResponse, error) {
	var encodedBody []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return calendlyResponse{}, errCalendlyRequestInvalid
		}
		encodedBody = encoded
	}
	target := calendlyAPIBaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.sendCalendlyRequestOnce(call.Context, credentials.AccessToken.Reveal(), method, target, encodedBody, isDispatched)
		if err != nil || response.statusCode != http.StatusUnauthorized || attempt > 0 || !client.canRefreshAfterRejection(*credentials) {
			return response, err
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if refreshErr != nil || !providerhttp.IsHeaderSafeCredential(replacement.AccessToken.Reveal()) {
			return response, nil
		}
		*credentials = replacement
		if isDispatched != nil {
			// Calendly refused the first request unprocessed, so only the repeat can leave an unknown outcome.
			isDispatched.Store(false)
		}
	}
	return calendlyResponse{}, errors.New("Calendly authenticated request retry was exhausted")
}

func (client *Client) sendCalendlyRequestOnce(
	requestContext context.Context, accessToken string, method string, target string, body []byte, isDispatched *atomic.Bool,
) (calendlyResponse, error) {
	if requestContext == nil {
		requestContext = context.Background()
	}
	tracedContext := httptrace.WithClientTrace(requestContext, &httptrace.ClientTrace{
		WroteRequest: func(written httptrace.WroteRequestInfo) {
			if written.Err == nil && isDispatched != nil {
				isDispatched.Store(true)
			}
		},
	})
	var requestBody io.Reader
	if body != nil {
		requestBody = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(tracedContext, method, target, requestBody)
	if err != nil {
		return calendlyResponse{}, errCalendlyRequestInvalid
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return calendlyResponse{}, err
	}
	defer response.Body.Close()
	result := calendlyResponse{statusCode: response.StatusCode, header: response.Header.Clone()}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Only the error title is read, so a truncated or unreadable error body keeps its status.
		result.body, _ = io.ReadAll(io.LimitReader(response.Body, providerhttp.MaxErrorBodyBytes))
		return result, nil
	}
	result.body, err = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return result, errCalendlyResponseTooLarge
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

// canRefreshAfterRejection is false for a personal access token, which Calendly never refreshes.
func (client *Client) canRefreshAfterRejection(credentials Credentials) bool {
	if credentials.AuthMethodID == PersonalAccessTokenAuthMethodID {
		return false
	}
	_, isRefreshing := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return isRefreshing
}

// resolveCurrentUser reads GET /users/me for a request whose input names no user or organization.
func (client *Client) resolveCurrentUser(call sdkgo.Call, credentials *Credentials) (calendlyCurrentUser, calendlyResponse, error) {
	response, err := client.sendCalendlyRequest(call, credentials, http.MethodGet, "/users/me", nil, nil, nil)
	if err != nil || response.statusCode != http.StatusOK {
		return calendlyCurrentUser{}, response, err
	}
	var decoded struct {
		Resource struct {
			URI                 string `json:"uri"`
			CurrentOrganization string `json:"current_organization"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		return calendlyCurrentUser{}, response, errCalendlyResponseMalformed
	}
	if _, err := parseUserURI(decoded.Resource.URI); err != nil {
		return calendlyCurrentUser{}, response, errCalendlyResponseMalformed
	}
	if _, err := parseOrganizationURI(decoded.Resource.CurrentOrganization); err != nil {
		return calendlyCurrentUser{}, response, errCalendlyResponseMalformed
	}
	return calendlyCurrentUser{userURI: decoded.Resource.URI, organizationURI: decoded.Resource.CurrentOrganization}, response, nil
}

// calendlyOutcome classifies a non-2xx answer: a Retry, or a conclusive failure the caller maps to a branch.
type calendlyOutcome struct {
	isRetry    bool
	retryAfter time.Duration
	failure    sdkgo.Failure
}

// classifyCalendlyFailure retries 429 and 5xx; other messages are the connector's, never Calendly's.
func (client *Client) classifyCalendlyFailure(operationID string, response calendlyResponse) calendlyOutcome {
	status := response.statusCode
	switch {
	case status == http.StatusTooManyRequests:
		return calendlyOutcome{
			isRetry: true, retryAfter: calendlyRateLimitDelay(response.header, client.now()),
			failure: calendlyFailure(operationID, sdkgo.FailureRateLimit, "Calendly rate limited the request"),
		}
	case status >= 500:
		return calendlyOutcome{
			isRetry: true,
			failure: calendlyFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Calendly answered HTTP %d", status)),
		}
	case status == http.StatusUnauthorized:
		return calendlyOutcome{failure: calendlyFailure(operationID, sdkgo.FailureAuthentication,
			"Calendly rejected the access token; generate a new personal access token or authorize the connection again")}
	case status == http.StatusForbidden:
		if isCalendlyInsufficientScope(response.body) {
			return calendlyOutcome{failure: calendlyFailure(operationID, sdkgo.FailureAuthorization,
				"the Calendly token lacks a scope this operation needs; grant the scopes listed in the connector guide")}
		}
		return calendlyOutcome{failure: calendlyFailure(operationID, sdkgo.FailureAuthorization,
			"Calendly denied permission for this user, organization, or resource")}
	case status == http.StatusNotFound:
		return calendlyOutcome{failure: calendlyFailure(operationID, sdkgo.FailureNotFound, "the Calendly resource was not found")}
	case status == http.StatusConflict:
		return calendlyOutcome{failure: calendlyFailure(operationID, sdkgo.FailureConflict, "Calendly reports that the resource already exists")}
	default:
		return calendlyOutcome{failure: calendlyFailure(operationID, sdkgo.FailureProviderRejection,
			fmt.Sprintf("Calendly rejected the request with HTTP %d", status))}
	}
}

// isCalendlyInsufficientScope matches the documented InsufficientScopeError title exactly.
func isCalendlyInsufficientScope(body []byte) bool {
	var decoded calendlyErrorBody
	return json.Unmarshal(body, &decoded) == nil && decoded.Title == "Insufficient scope"
}

// calendlyRateLimitDelay prefers Retry-After and otherwise uses X-RateLimit-Reset, which Calendly
// documents as the seconds until the limit resets.
func calendlyRateLimitDelay(header http.Header, now time.Time) time.Duration {
	if delay := providerhttp.ParseRetryAfter(header.Get("Retry-After"), now); delay > 0 {
		return delay
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(header.Get("X-RateLimit-Reset")), 10, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return min(time.Duration(seconds)*time.Second, providerhttp.MaxRetryAfterDelay)
}

func (client *Client) calendlyReceipt(objectURI string) sdkgo.Receipt {
	return sdkgo.Receipt{Provider: ConnectorID, ProviderObjectID: objectURI, ObservedAt: client.now().UTC()}
}

func calendlyFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operationID, Message: message}
}

func calendlyFailurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := calendlyFailure(operationID, kind, message)
	return &failure
}

// responseFailureKind names a read or decode failure: an oversized body or a malformed one.
func responseFailureKind(err error) sdkgo.FailureKind {
	if errors.Is(err, errCalendlyResponseTooLarge) {
		return sdkgo.FailureResponseTooLarge
	}
	return sdkgo.FailureProtocol
}

func responseFailureMessage(err error) string {
	if errors.Is(err, errCalendlyResponseTooLarge) {
		return errCalendlyResponseTooLarge.Error()
	}
	return errCalendlyResponseMalformed.Error()
}

func parseUserURI(value string) (string, error) {
	return parseCalendlyResourceURI(value, "/users/", "user")
}

func parseOrganizationURI(value string) (string, error) {
	return parseCalendlyResourceURI(value, "/organizations/", "organization")
}

func parseEventTypeURI(value string) (string, error) {
	return parseCalendlyResourceURI(value, "/event_types/", "event type")
}

func parseScheduledEventURI(value string) (string, error) {
	return parseCalendlyResourceURI(value, "/scheduled_events/", "scheduled event")
}

// parseInviteeURI returns the scheduled event and invitee identifiers of an invitee URI.
func parseInviteeURI(value string) (string, string, error) {
	rest, isCut := strings.CutPrefix(value, calendlyAPIBaseURL+"/scheduled_events/")
	eventID, inviteeID, hasInvitees := strings.Cut(rest, "/invitees/")
	if !isCut || !hasInvitees || !calendlyResourceIDPattern.MatchString(eventID) || !calendlyResourceIDPattern.MatchString(inviteeID) {
		return "", "", fmt.Errorf("invitee URI must look like %s/scheduled_events/<id>/invitees/<id>", calendlyAPIBaseURL)
	}
	return eventID, inviteeID, nil
}

// parseCalendlyResourceURI accepts exactly https://api.calendly.com<collection><id> and returns the ID.
func parseCalendlyResourceURI(value string, collection string, noun string) (string, error) {
	identifier, isCut := strings.CutPrefix(value, calendlyAPIBaseURL+collection)
	if !isCut || !calendlyResourceIDPattern.MatchString(identifier) {
		return "", fmt.Errorf("%s URI must look like %s%s<id>", noun, calendlyAPIBaseURL, collection)
	}
	return identifier, nil
}

// validatePageRequest checks a list page size and the opaque page token Calendly returned earlier.
func validatePageRequest(pageSize int, pageToken string) error {
	if pageSize < 0 || pageSize > maximumPageSize {
		return fmt.Errorf("pageSize must be from 1 through %d, or 0 for Calendly's default of 20", maximumPageSize)
	}
	if pageToken != "" && !calendlyPageTokenPattern.MatchString(pageToken) {
		return fmt.Errorf("pageToken must be the nextPageToken value of an earlier page")
	}
	return nil
}

func formatCalendlyTimestamp(value time.Time) string {
	return value.UTC().Format(calendlyTimestampLayout)
}

// parseCalendlyTimestamp reads a required RFC 3339 timestamp and returns it in UTC.
func parseCalendlyTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errCalendlyResponseMalformed
	}
	return parsed.UTC(), nil
}

// parseOptionalCalendlyTimestamp reads a timestamp that Calendly may omit or send as null.
func parseOptionalCalendlyTimestamp(value *string) (time.Time, error) {
	if value == nil || *value == "" {
		return time.Time{}, nil
	}
	return parseCalendlyTimestamp(*value)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
