// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex/sdk-go/dex"
)

// heartbeatInterval is half of Dex's 10-second minimum heartbeat timeout, so a silent attempt beats twice per timeout.
const heartbeatInterval = 5 * time.Second

// TextGenerationOperationID is the operation ID every lab connector gives its
// text-generation Query, so applications find the same operation, branches,
// and types on every connector.
const TextGenerationOperationID = "generateText"

// TextGenerationResult is the Result every generateText Step passes to its
// branch target. It is the same Go type on every lab connector, so one
// application Step can handle the Result of any lab's generateText.
type TextGenerationResult = sdkgo.QueryResult[TextGenerationResponse]

// TextGenerationQueryConfig configures one connection's generateText Query.
// NewTextGenerationQuery copies it, including the WireFormat's map and slices,
// so later changes to them have no effect. The functions the WireFormat holds,
// and any state they capture, stay shared.
type TextGenerationQueryConfig struct {
	// Definition is the connector's generated generateText definition. Its
	// operation ID must be TextGenerationOperationID, it must declare exactly
	// the branches returned by TextGenerationBranchDefinitions, and its Step
	// defaults must use sync Execute durability, a heartbeat timeout of zero
	// (the one-minute Dex default) or at least 10 seconds, and an Execute
	// timeout of zero or longer than the request timeout.
	Definition sdkgo.QueryDefinition
	// WireFormat adapts the pipeline to the provider API, including the model
	// ID rule that validates the connection's and each request's model.
	WireFormat WireFormat
	// BaseURL is the provider base URL that EncodedRequest.Path is appended
	// to. It is checked with providerhttp.ValidateBaseURL.
	BaseURL string
	// ConnectionModel is the connection's model after configuration defaults.
	// Surrounding whitespace is trimmed. It may be blank, in which case every
	// request must name a model.
	ConnectionModel string
	// HTTPClient is the caller's client. The Query uses a hardened copy from
	// providerhttp.NewProviderHTTPClient; nil uses http.DefaultTransport.
	HTTPClient *http.Client
	// RequestTimeout bounds one HTTP exchange, including reading the body,
	// when HTTPClient sets no timeout. Keep it below the Step's Execute timeout
	// so a stalled exchange returns Retry before Dex abandons the attempt.
	RequestTimeout time.Duration
	// ResolveCredential returns the connection's credential for one call. The
	// pipeline calls it once per attempt, after local validation.
	ResolveCredential func(call sdkgo.Call) (sdkgo.SecretString, error)
	// MaxResponseBytes caps a 2xx body or event stream. A larger response selects invalidResponse.
	MaxResponseBytes int64
	// MaxStreamEventBytes caps one server-sent event. It is required when the
	// wire format has DecodeStream.
	MaxStreamEventBytes int
}

// TextGenerationQuery is the shared generateText pipeline. It implements
// sdkgo.Query[TextGenerationRequest, TextGenerationResponse], so a connector's
// hand-written client returns it from GenerateText and the generated Step
// factory runs it. Its methods never modify it, so concurrent calls are safe
// when the wire format and credential resolver are.
//
// Every Invoke runs the same fixed order:
//
//  1. Resolve the model: the trimmed request Model, else ConnectionModel; a
//     blank or invalid model selects defect.
//  2. Validate the request: declared features, messages, MaxOutputTokens,
//     and the model's temperature and reasoning-effort rules.
//  3. Check the structured-output schema against the portable subset, apply
//     the model's transforms, and add the JSON instruction when the mode needs it.
//  4. Resolve the credential and require it to be header-safe.
//  5. Build the request through the wire format; the credential goes only in
//     its CredentialHeader.
//  6. Dispatch with the hardened client. While the attempt is in flight, a
//     nil Dex heartbeat is recorded every 5 seconds and the optional stall
//     rule counts every received byte.
//  7. Classify a non-2xx status through the wire format's ErrorRules and the
//     defaults documented on ErrorRule.
//  8. Decode a 2xx event stream, or any other 2xx body with DecodeResponse,
//     within the size limits; classify a ProviderReportedError through the
//     status-independent ErrorRules; map the finish reason; join non-reasoning
//     text; and validate structured output against the original schema.
//  9. Return the branch with a Receipt holding the Call ID, provider, response
//     ID, request ID, and the listed rate-limit headers.
//
// Steps 1 through 5 select defect without any provider request. Transport
// failures, a stall, an interrupted stream, and retryable statuses return
// Retry. Failures, Receipts, and heartbeats never contain prompts, model
// output, provider message text, or the credential.
//
// Streamed text is written to the Step's text Stream as it arrives, before
// the finish reason is known, and Dex does not remove it on retry. The text
// Stream can therefore hold text from an attempt that returned Retry followed
// by the whole text of the next attempt, or text of an attempt that selected
// blocked or invalidResponse. Only the Result's Text is authoritative.
//
// The nil heartbeat clears any heartbeat checkpoint the enclosing Step
// recorded, so a Step that calls sdkgo.RunQuery with this Query must not rely
// on heartbeat checkpoints. The Query must run with sync Execute durability:
// under async durability, a call that outlasts the local phase is sent to the
// provider again by the fallback attempt.
//
// The portable structured-output subset: the root has type "object"; every
// object has "properties", sets "additionalProperties" to false, and lists
// every property in "required" (use type [T, "null"] for an optional value);
// "type" is one JSON type or a pair of one type and "null"; arrays have
// "items"; the other allowed keywords are "enum" (non-empty scalars),
// "const", "description", "title", "minimum", "maximum", "minLength",
// "maxLength", "minItems", "maxItems", and "format" (date-time, date, time,
// or uuid); nesting is at most 10 levels. Anything else, including "$ref",
// "$defs", and "anyOf", selects defect naming the keyword. Every number in
// the schema and in the returned object must have at most 256 characters and
// a decimal exponent of magnitude at most 400; a longer or larger number in
// the returned object selects invalidResponse.
type TextGenerationQuery struct {
	definition          sdkgo.QueryDefinition
	wireFormat          WireFormat
	baseURL             string
	connectionModel     string
	httpClient          *http.Client
	resolveCredential   func(sdkgo.Call) (sdkgo.SecretString, error)
	maxResponseBytes    int64
	maxStreamEventBytes int
	heartbeatInterval   time.Duration
}

var _ sdkgo.Query[TextGenerationRequest, TextGenerationResponse] = (*TextGenerationQuery)(nil)

// NewTextGenerationQuery validates config and returns the connection's
// generateText Query. It returns an error when config is nil, the definition,
// its Step defaults, or the wire format is invalid, the base URL or
// connection model is invalid, a size limit or the request timeout is not
// positive, or ResolveCredential is nil. Error messages never repeat
// configuration values.
func NewTextGenerationQuery(config *TextGenerationQueryConfig) (*TextGenerationQuery, error) {
	if config == nil {
		return nil, fmt.Errorf("text generation query config is required")
	}
	if err := config.Definition.Validate(); err != nil {
		return nil, fmt.Errorf("text generation query definition: %w", err)
	}
	if config.Definition.Operation.OperationID != TextGenerationOperationID {
		return nil, fmt.Errorf("text generation query definition must use operation ID %q", TextGenerationOperationID)
	}
	if err := validateTextGenerationBranches(config.Definition); err != nil {
		return nil, fmt.Errorf("text generation query definition: %w", err)
	}
	wireFormat := cloneWireFormat(config.WireFormat)
	if err := validateWireFormat(&wireFormat); err != nil {
		return nil, fmt.Errorf("text generation query: %w", err)
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("text generation query: %w", err)
	}
	connectionModel := strings.TrimSpace(config.ConnectionModel)
	if connectionModel != "" {
		if connectionModel, err = wireFormat.ModelIDRule.ValidateModelID(connectionModel); err != nil {
			return nil, fmt.Errorf("text generation query connection %w", err)
		}
	}
	if config.RequestTimeout <= 0 {
		return nil, fmt.Errorf("text generation query request timeout must be positive")
	}
	if config.ResolveCredential == nil {
		return nil, fmt.Errorf("text generation query credential resolver is required")
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("text generation query maxResponseBytes must be positive")
	}
	if wireFormat.DecodeStream != nil && config.MaxStreamEventBytes < 1 {
		return nil, fmt.Errorf("text generation query maxStreamEventBytes must be positive for a streaming wire format")
	}
	httpClient := providerhttp.NewProviderHTTPClient(config.HTTPClient, config.RequestTimeout)
	if err := validateTextGenerationStepDefaults(config.Definition.StepDefaults, httpClient.Timeout); err != nil {
		return nil, fmt.Errorf("text generation query definition: %w", err)
	}
	return &TextGenerationQuery{
		definition: config.Definition, wireFormat: wireFormat, baseURL: baseURL, connectionModel: connectionModel,
		httpClient: httpClient, resolveCredential: config.ResolveCredential, maxResponseBytes: config.MaxResponseBytes,
		maxStreamEventBytes: config.MaxStreamEventBytes, heartbeatInterval: heartbeatInterval,
	}, nil
}

// Definition returns the connector's generateText definition.
func (query *TextGenerationQuery) Definition() sdkgo.QueryDefinition { return query.definition }

// RequestFeatures returns the optional request fields the wire format
// declares. The llmtest conformance suite uses it to prove that every other
// field selects defect without a provider request.
func (query *TextGenerationQuery) RequestFeatures() RequestFeatures {
	return query.wireFormat.Features
}

// ModelIDRule returns the wire format's rule that validates each request's model.
func (query *TextGenerationQuery) ModelIDRule() ModelIDRule { return query.wireFormat.ModelIDRule }

// Invoke runs one provider call through the fixed pipeline and classifies the
// attempt. See TextGenerationQuery for the order and outcomes.
func (query *TextGenerationQuery) Invoke(call sdkgo.Call, request TextGenerationRequest) sdkgo.QueryAttempt[TextGenerationResponse] {
	attempt := &textGenerationAttempt{query: query, call: call}
	return attempt.run(request)
}

// validateTextGenerationStepDefaults rejects defaults that repeat provider calls or race the heartbeat.
func validateTextGenerationStepDefaults(defaults sdkgo.StepDefaults, requestTimeout time.Duration) error {
	if defaults.ExecuteDurability != dex.StepDurabilitySync {
		return fmt.Errorf("generateText must use sync Execute durability, because an async fallback attempt calls the provider again")
	}
	if defaults.HeartbeatTimeout != 0 && defaults.HeartbeatTimeout < 2*heartbeatInterval {
		return fmt.Errorf("generateText heartbeat timeout must be zero or at least %s", 2*heartbeatInterval)
	}
	if defaults.ExecuteMethodTimeout != 0 && defaults.ExecuteMethodTimeout <= requestTimeout {
		return fmt.Errorf("generateText Execute timeout must exceed the request timeout, so a stalled exchange returns Retry first")
	}
	return nil
}
