// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package webhooktrigger serves a provider webhook endpoint for connector Trigger sources.
//
// A connector supplies only what differs per provider: how to authenticate a request and how to decode
// it into a typed event with a stable ID. The Endpoint owns the rest of the push pipeline, in the order
// that makes delivery durable:
//
//  1. accept only POST and read the body within MaxBodyBytes;
//  2. resolve the connection's credentials and call VerifyRequest;
//  3. call DecodeEvent, acknowledging an event no binding handles;
//  4. call sdkgo.PrepareTriggerDelivery for every active source that accepts the event, so each
//     binding's durable inbox records it;
//  5. answer 200 only after every accepting source recorded the event, then deliver it to each
//     source in arrival order with sdkgo.DeliverTrigger.
//
// A provider therefore retries any event this process could not record: the endpoint answers 503 while
// no source is running, while credentials are unavailable, and when recording fails. It answers 400 for
// a request that fails verification or decoding, 405 for another method, and 413 for an oversized body.
//
// The package contains no provider host, signature format, event type, or credential. A connector keeps
// its Endpoint on its client, returns it as the http.Handler applications mount, and creates one source
// per Trigger binding from its generated source hook:
//
//	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, OrderEvent]{
//		ConnectorID: ConnectorID, TriggerName: "orderUpdated", Connection: reference,
//		Credentials: credentialProvider, MaxBodyBytes: 1 << 20,
//		VerifyRequest: verifyOrderSignature, DecodeEvent: decodeOrderEvent,
//	})
//	// In the Trigger source hook:
//	source := endpoint.NewSource(configuration.acceptsEvent)
package webhooktrigger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// defaultSourceQueueCapacity bounds each source's recorded events awaiting delivery.
const defaultSourceQueueCapacity = 64

// ErrVerificationUnavailable marks a VerifyRequest failure the provider should retry, such as a
// connection whose signing secret is not configured yet. The endpoint answers 503 instead of 400.
var ErrVerificationUnavailable = errors.New("webhook verification is temporarily unavailable")

// Request is one received webhook request after its body was read within the endpoint's limit.
type Request struct {
	// URL is the request URL as received, for providers that sign it.
	URL *url.URL
	// Header holds the request headers.
	Header http.Header
	// Body is the complete request body.
	Body []byte
	// ReceivedAt is when the endpoint read the request, from EndpointConfig.Now.
	ReceivedAt time.Time
}

// EndpointConfig configures the webhook endpoint of one connection.
type EndpointConfig[C any, T any] struct {
	// ConnectorID and TriggerName identify the credential call made for each request.
	ConnectorID string
	// TriggerName is the manifest Trigger name whose bindings this endpoint serves.
	TriggerName string
	// Connection is the connection whose credentials authenticate requests.
	Connection sdkgo.ConnectionRef
	// Credentials resolves the connection's credentials for each request.
	Credentials sdkgo.CredentialProvider[C]
	// MaxBodyBytes is the largest accepted body in bytes. It must be positive.
	MaxBodyBytes int64
	// VerifyRequest authenticates a request. Wrap ErrVerificationUnavailable for a retryable failure.
	// Error text never reaches the provider.
	VerifyRequest func(Request, C) error
	// DecodeEvent decodes a verified request into an event with a stable provider ID. It returns false,
	// without an error, for a valid event that no binding of this Trigger handles.
	DecodeEvent func(Request) (sdkgo.TriggerEvent[T], bool, error)
	// Now returns the receipt time; nil uses time.Now.
	Now func() time.Time
	// SourceQueueCapacity bounds each source's recorded events awaiting delivery; zero uses 64. A full
	// queue answers 503 so the provider retries.
	SourceQueueCapacity int
}

// Endpoint serves one connection's webhook URL and fans each event out to its running sources.
// It is safe for concurrent requests.
type Endpoint[C any, T any] struct {
	config  EndpointConfig[C, T]
	mu      sync.RWMutex
	sources map[*source[C, T]]sdkgo.TriggerTarget[T]
}

type source[C any, T any] struct {
	endpoint    *Endpoint[C, T]
	acceptEvent func(sdkgo.TriggerEvent[T]) bool
	deliveries  chan sdkgo.TriggerEvent[T]
	mu          sync.Mutex
	isRunning   bool
}

type activeSource[C any, T any] struct {
	source *source[C, T]
	target sdkgo.TriggerTarget[T]
}

// NewEndpoint validates config and returns an Endpoint with no running sources.
func NewEndpoint[C any, T any](config EndpointConfig[C, T]) (*Endpoint[C, T], error) {
	switch {
	case strings.TrimSpace(config.ConnectorID) == "" || strings.TrimSpace(config.TriggerName) == "":
		return nil, errors.New("webhook endpoint connector ID and Trigger name are required")
	case strings.TrimSpace(config.Connection.Name) == "":
		return nil, errors.New("webhook endpoint connection name is required")
	case config.Credentials == nil || config.VerifyRequest == nil || config.DecodeEvent == nil:
		return nil, errors.New("webhook endpoint credentials, VerifyRequest, and DecodeEvent are required")
	case config.MaxBodyBytes <= 0:
		return nil, errors.New("webhook endpoint MaxBodyBytes must be positive")
	case config.SourceQueueCapacity < 0:
		return nil, errors.New("webhook endpoint SourceQueueCapacity cannot be negative")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.SourceQueueCapacity == 0 {
		config.SourceQueueCapacity = defaultSourceQueueCapacity
	}
	return &Endpoint[C, T]{config: config, sources: make(map[*source[C, T]]sdkgo.TriggerTarget[T])}, nil
}

// NewSource returns a Trigger source that receives the endpoint's events for which acceptEvent returns
// true; nil accepts every event. The source receives events only while its Run is active.
func (endpoint *Endpoint[C, T]) NewSource(acceptEvent func(sdkgo.TriggerEvent[T]) bool) sdkgo.TriggerSource[T] {
	if acceptEvent == nil {
		acceptEvent = func(sdkgo.TriggerEvent[T]) bool { return true }
	}
	return &source[C, T]{
		endpoint: endpoint, acceptEvent: acceptEvent,
		deliveries: make(chan sdkgo.TriggerEvent[T], endpoint.config.SourceQueueCapacity),
	}
}

// RunningSourceCount reports how many sources are receiving events. While it is zero the endpoint
// answers 503 to every handled event, so a readiness check can wait for it to be positive.
func (endpoint *Endpoint[C, T]) RunningSourceCount() int {
	endpoint.mu.RLock()
	defer endpoint.mu.RUnlock()
	return len(endpoint.sources)
}

// ServeHTTP verifies, records, and acknowledges one webhook request.
func (endpoint *Endpoint[C, T]) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	contents, err := io.ReadAll(io.LimitReader(request.Body, endpoint.config.MaxBodyBytes+1))
	if err != nil {
		http.Error(response, "invalid request body", http.StatusBadRequest)
		return
	}
	if int64(len(contents)) > endpoint.config.MaxBodyBytes {
		http.Error(response, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	received := Request{URL: request.URL, Header: request.Header, Body: contents, ReceivedAt: endpoint.config.Now()}
	credentials, err := endpoint.resolveCredentials(request.Context(), contents)
	if err != nil {
		http.Error(response, "webhook temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := endpoint.config.VerifyRequest(received, credentials); err != nil {
		if errors.Is(err, ErrVerificationUnavailable) {
			http.Error(response, "webhook temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		http.Error(response, "invalid webhook signature", http.StatusBadRequest)
		return
	}
	event, isHandled, err := endpoint.config.DecodeEvent(received)
	if err != nil {
		http.Error(response, "invalid webhook event", http.StatusBadRequest)
		return
	}
	if !isHandled {
		response.WriteHeader(http.StatusOK)
		return
	}
	if err := endpoint.recordAndQueue(request.Context(), event); err != nil {
		http.Error(response, "webhook delivery temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusOK)
}

// Run makes the source receive its endpoint's events until cancellation, delivering each in order.
func (source *source[C, T]) Run(ctx context.Context, target sdkgo.TriggerTarget[T]) error {
	if target == nil {
		return errors.New("webhook Trigger target is required")
	}
	source.mu.Lock()
	if source.isRunning {
		source.mu.Unlock()
		return errors.New("webhook Trigger source is already running")
	}
	source.isRunning = true
	source.mu.Unlock()
	source.endpoint.activate(source, target)
	defer func() {
		source.endpoint.deactivate(source)
		source.mu.Lock()
		source.isRunning = false
		source.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-source.deliveries:
			if err := sdkgo.DeliverTrigger(ctx, target, event); err != nil {
				return err
			}
		}
	}
}

func (endpoint *Endpoint[C, T]) activate(source *source[C, T], target sdkgo.TriggerTarget[T]) {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	endpoint.sources[source] = target
}

func (endpoint *Endpoint[C, T]) deactivate(source *source[C, T]) {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	delete(endpoint.sources, source)
}

// recordAndQueue records for every accepting source before queueing; no running source fails for a retry.
func (endpoint *Endpoint[C, T]) recordAndQueue(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
	endpoint.mu.RLock()
	running := make([]activeSource[C, T], 0, len(endpoint.sources))
	for source, target := range endpoint.sources {
		running = append(running, activeSource[C, T]{source: source, target: target})
	}
	endpoint.mu.RUnlock()
	if len(running) == 0 {
		return errors.New("no webhook Trigger source is running")
	}
	accepting := make([]activeSource[C, T], 0, len(running))
	for _, candidate := range running {
		if !candidate.source.acceptEvent(event) {
			continue
		}
		if err := sdkgo.PrepareTriggerDelivery(ctx, candidate.target, event); err != nil {
			return err
		}
		accepting = append(accepting, candidate)
	}
	for _, recipient := range accepting {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case recipient.source.deliveries <- event:
		default:
			return errors.New("webhook Trigger delivery queue is full")
		}
	}
	return nil
}

func (endpoint *Endpoint[C, T]) resolveCredentials(ctx context.Context, contents []byte) (C, error) {
	call := endpoint.credentialCall(contents)
	if contextProvider, isContextProvider := endpoint.config.Credentials.(sdkgo.ContextCredentialProvider[C]); isContextProvider {
		return contextProvider.ResolveContext(ctx, call)
	}
	return endpoint.config.Credentials.Resolve(call)
}

// credentialCall derives a stable call ID from the connection and body, so a redelivery resolves alike.
func (endpoint *Endpoint[C, T]) credentialCall(contents []byte) sdkgo.Call {
	digest := sha256.New()
	for _, part := range []string{endpoint.config.ConnectorID, endpoint.config.Connection.Provider, endpoint.config.Connection.Name} {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	_, _ = digest.Write(contents)
	identity := digest.Sum(nil)[:16]
	identity[6] = (identity[6] & 0x0f) | 0x50
	identity[8] = (identity[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(identity)
	return sdkgo.Call{
		ID:         sdkgo.CallID(fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])),
		Connection: endpoint.config.Connection,
		Operation:  sdkgo.OperationRef{ConnectorID: endpoint.config.ConnectorID, OperationID: endpoint.config.TriggerName},
	}
}
