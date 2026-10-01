// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package webhook connects Dex applications to any service that sends or receives webhooks.
//
// The requestReceived Trigger serves one HTTPS endpoint per connection. It authenticates each request
// with HMAC-SHA256, Standard Webhooks, or a shared token, derives a stable event ID, and records the
// event in every accepting binding's durable inbox before answering 200. The sendEvent Mutation POSTs
// one JSON event to the connection's delivery URL, signed with the Standard Webhooks scheme.
//
// The runnable examples/form-submission application mounts NewLocalRequestReceivedEndpointRunner and
// starts one Flow per verified submission.
package webhook

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// sendEventRequestTimeout leaves time within the 30-second Execute timeout to classify the response.
const sendEventRequestTimeout = 25 * time.Second

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient replaces the HTTP client that sendEvent uses. The connector copies it, never follows a
// redirect, and applies a 25-second timeout when the client sets none.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for receipt times, Standard Webhooks timestamps, and their tolerance check.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the endpoint's delivery records and those of the durable inboxes that
// NewLocalRequestReceivedEndpointRunner creates to logger. Without it, those records go to slog.Default().
// Records carry event IDs, never bodies or secrets.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client verifies incoming webhook requests and sends signed webhook events for one connection
// configuration. It is safe for concurrent use.
type Client struct {
	credentials         sdkgo.CredentialProvider[Credentials]
	httpClient          *http.Client
	now                 func() time.Time
	logger              *slog.Logger
	maxBodyBytes        int64
	deliveryURL         string
	requestVerifier     requestVerifier
	requestEventDecoder requestEventDecoder

	requestReceivedEndpointsMu sync.Mutex
	requestReceivedEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, WebhookRequestEvent]
}

// New validates config and constructs a Client. Blank fields take their manifest defaults. It returns an
// error for an invalid header name, event ID pointer, forwarded header, or a deliveryUrl that is not HTTPS.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("webhook credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("webhook connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("webhook connector clock is required")
	}
	deliveryURL, err := validateDeliveryURL(config.DeliveryURL)
	if err != nil {
		return nil, err
	}
	verifier, err := newRequestVerifier(&config)
	if err != nil {
		return nil, err
	}
	decoder, err := newRequestEventDecoder(&config)
	if err != nil {
		return nil, err
	}
	return &Client{
		credentials: credentials, now: dependencies.now, logger: dependencies.logger,
		httpClient:   providerhttp.NewProviderHTTPClient(dependencies.httpClient, sendEventRequestTimeout),
		maxBodyBytes: config.MaxBodyBytes, deliveryURL: deliveryURL,
		requestVerifier: verifier, requestEventDecoder: decoder,
		requestReceivedEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, WebhookRequestEvent]),
	}, nil
}

// SendEvent returns the sendEvent Mutation bound to this client.
func (client *Client) SendEvent() SendEventOperation {
	return SendEventOperation{client: client}
}

// RequestReceivedWebhookHandler returns the connection's webhook endpoint for an application to mount at
// the public HTTPS URL configured in the sender. Every requestReceived Trigger built from this Connection
// feeds from it, and it answers 503 while none of them runs, so the sender retries.
func (connection Connection) RequestReceivedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.requestReceivedWebhookEndpoint(connection.reference)
}

func (client *Client) requestReceivedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration RequestReceivedTriggerConfiguration,
) sdkgo.TriggerSource[WebhookRequestEvent] {
	matcher, err := newRequestMatcher(configuration)
	if err != nil {
		panic(err)
	}
	endpoint, err := client.requestReceivedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(matcher.acceptsEvent)
}

// requestReceivedWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) requestReceivedWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, WebhookRequestEvent], error) {
	client.requestReceivedEndpointsMu.Lock()
	defer client.requestReceivedEndpointsMu.Unlock()
	if endpoint, found := client.requestReceivedEndpoints[connection]; found {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, WebhookRequestEvent]{
		ConnectorID: ConnectorID, TriggerName: RequestReceivedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: client.maxBodyBytes,
		VerifyRequest: client.requestVerifier.verifyRequest, DecodeEvent: client.requestEventDecoder.decodeEvent,
		Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("webhook endpoint: %w", err)
	}
	client.requestReceivedEndpoints[connection] = endpoint
	return endpoint, nil
}
