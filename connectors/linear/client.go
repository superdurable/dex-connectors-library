// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package linear connects Dex applications to Linear issue tracking through Linear's GraphQL API at
// https://api.linear.app/graphql.
//
// A Client exposes seven operations over Linear's Team, workflow state, Issue, Comment, and User
// records. SearchIssues lists one bounded page of issue summaries for a typed filter, including issues
// changed after a time, and GetIssue reads one issue. CreateIssue and AddComment send a client-supplied
// UUID that Linear stores as the new record's ID: every attempt of one Step execution sends the same
// UUID, so a retried or re-dispatched attempt reads the record an earlier attempt created instead of
// writing a second one. UpdateIssue sets a workflow state, assignee, labels, priority, due date, or title
// with absolute values and label set operations, so repeating it changes nothing. FindUserByEmail and
// ListWorkflowStates resolve an assignee and a team's workflow states.
//
// The issueEventReceived Trigger serves one webhook endpoint per connection, verifies each delivery's
// Linear-Signature and signed webhookTimestamp, and records every Issue event in each accepting binding's
// durable inbox before answering 200.
//
// A connection authenticates with a personal API key, sent as the raw Authorization header, or with
// Linear OAuth, whose 24-hour access tokens CredentialRefreshDriver refreshes before they expire and once
// after Linear rejects one.
package linear

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// PersonalAPIKeyAuthMethodID identifies a connection that uses a Linear personal API key.
	PersonalAPIKeyAuthMethodID = "personal-api-key"
	// OAuthAuthMethodID identifies a connection authorized through a Linear OAuth application.
	OAuthAuthMethodID = "linear-oauth"

	providerName  = "linear"
	defaultAPIURL = "https://api.linear.app/graphql"

	// defaultRequestTimeout lets a write and its read-back both fit inside operationTimeout.
	defaultRequestTimeout = 12 * time.Second
	// operationTimeout bounds every request of one operation below the 30-second Execute timeout.
	operationTimeout = 25 * time.Second
	requestIDHeader  = "X-Request-Id"
)

var (
	requestIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	errRequestNotBuilt = errors.New("Linear request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiURL     string
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 12 seconds per request. The caller
// retains ownership of the client and its transport. The connector uses a copy that never follows
// redirects, so a key or token is never replayed to another host. The same client also carries OAuth
// token refresh requests.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIURL replaces https://api.linear.app/graphql for a local Linear-compatible fake. The URL must use
// HTTPS unless its host is loopback, and it must not carry user information, a query, or a fragment.
// Production connections leave it unset.
func WithAPIURL(apiURL string) Option {
	return func(options *clientOptions) { options.apiURL = apiURL }
}

// WithClock overrides the clock used for Receipts, token expiry, and webhook timestamp tolerance. Tests
// use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the records of the durable inboxes that NewProjectIssueEventReceivedEndpointRunner
// creates, and of each binding's delivery retries, to logger. Without it, those records go to
// slog.Default(). Records carry event IDs only.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Linear GraphQL requests for connector operations and receives signed
// Linear webhooks for one connection configuration. A Client is safe for concurrent use by several Steps.
type Client struct {
	apiURL                    string
	httpClient                *http.Client
	credentials               CredentialSource
	refreshDriver             *CredentialRefreshDriver
	maxResponseBytes          int64
	webhookMaxBodyBytes       int64
	webhookSignatureTolerance time.Duration
	now                       func() time.Time
	logger                    *slog.Logger

	issueEventEndpointsMu sync.Mutex
	issueEventEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, IssueEvent]
}

// graphQLRequest is one named GraphQL document with its variables, so input never becomes query text.
type graphQLRequest struct {
	operationName string
	document      string
	variables     map[string]any
}

type graphQLRequestBody struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
}

// linearResponse holds the safe parts of one response; data is set only for a credential-free JSON body.
type linearResponse struct {
	statusCode int
	data       json.RawMessage
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Linear request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	// exchangeRetry is safe for reads, for absolute updates, and for writes that carry a client-supplied ID.
	exchangeRetry
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// linearExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type linearExchange struct {
	outcome    exchangeOutcome
	response   linearResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// New validates configuration and constructs a Linear client. Credentials are resolved before every
// provider call and every webhook delivery, so a replaced API key or a refreshed OAuth token takes effect
// without a restart; the limits are startup configuration. Construction never contacts Linear.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	switch {
	case config.MaxResponseBytes < 1 || config.WebhookMaxBodyBytes < 1:
		return nil, errors.New("Linear response and webhook body limits must be positive")
	case config.WebhookSignatureTolerance < time.Second:
		return nil, errors.New("Linear webhookSignatureTolerance must be at least one second")
	case credentials == nil:
		return nil, errors.New("Linear credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Linear connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Linear connector clock is required")
	}
	apiURL := defaultAPIURL
	if dependencies.apiURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiURL)
		if err != nil {
			return nil, fmt.Errorf("Linear API URL: %w", err)
		}
		apiURL = validated
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		apiURL: apiURL, httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver,
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		webhookSignatureTolerance: config.WebhookSignatureTolerance, now: dependencies.now, logger: dependencies.logger,
		issueEventEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, IssueEvent]),
	}, nil
}

// SearchIssues returns the searchIssues Query bound to this client.
func (client *Client) SearchIssues() SearchIssuesOperation {
	return SearchIssuesOperation{client: client}
}

// GetIssue returns the getIssue Query bound to this client.
func (client *Client) GetIssue() GetIssueOperation { return GetIssueOperation{client: client} }

// CreateIssue returns the createIssue Mutation bound to this client.
func (client *Client) CreateIssue() CreateIssueOperation { return CreateIssueOperation{client: client} }

// UpdateIssue returns the updateIssue Mutation bound to this client.
func (client *Client) UpdateIssue() UpdateIssueOperation { return UpdateIssueOperation{client: client} }

// AddComment returns the addComment Mutation bound to this client.
func (client *Client) AddComment() AddCommentOperation { return AddCommentOperation{client: client} }

// FindUserByEmail returns the findUserByEmail Query bound to this client.
func (client *Client) FindUserByEmail() FindUserByEmailOperation {
	return FindUserByEmailOperation{client: client}
}

// ListWorkflowStates returns the listWorkflowStates Query bound to this client.
func (client *Client) ListWorkflowStates() ListWorkflowStatesOperation {
	return ListWorkflowStatesOperation{client: client}
}
