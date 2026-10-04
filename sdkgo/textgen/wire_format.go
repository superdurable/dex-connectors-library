// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import (
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/textproto"
	"regexp"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")

// WireFormat adapts the provider-neutral pipeline to one provider API. It is a
// struct of functions and declarations, never an interface a connector
// implements, so new capabilities can be added as new fields without breaking
// connectors compiled against an older SDK. Construct it with a keyed
// composite literal or with a family package such as openaichat.
//
// Every function must be safe for concurrent use and free of side effects
// beyond its return values. Errors returned by EncodeRequest become defect
// Failure messages, so they must never contain request text, model output, or
// credentials. Errors returned by DecodeResponse and DecodeStream are never
// copied into a Failure; only the bounded tokens of a ProviderReportedError
// are.
type WireFormat struct {
	// ProviderName names the provider in every Failure and Receipt, such as "deepseek".
	ProviderName string
	// ModelIDRule validates the connection's and each request's model, and
	// follows from where the model travels: ModelIDRuleBody for a JSON body
	// field, ModelIDRulePathSegment for a URL path segment. It is required.
	ModelIDRule ModelIDRule
	// Features declares which optional TextGenerationRequest fields the wire
	// format sends. A non-zero request field whose feature is false selects
	// defect with zero provider requests, so a newer request field is never
	// ignored by an older wire format.
	Features RequestFeatures
	// CredentialHeader is the only place the pipeline puts the credential.
	CredentialHeader CredentialHeader
	// RulesForModel returns the sampling and structured-output rules for a
	// validated model ID. It is required and is called once per request.
	RulesForModel func(model string) ModelRequestRules
	// EncodeRequest builds the provider request. It never receives the
	// credential. It is required.
	EncodeRequest func(input EncodeRequestInput) (EncodedRequest, error)
	// DecodeResponse decodes a complete 2xx body that is within the
	// connection's size limit, including a JSON body returned for a streaming
	// request by a gateway that ignored it. It returns a *ProviderReportedError
	// when the body is a provider error object. It is required unless every
	// request streams.
	DecodeResponse func(body []byte) (DecodedResponse, error)
	// DecodeStream decodes a 2xx response whose Content-Type is
	// text/event-stream, calling writeTextDelta with each non-reasoning text
	// delta in order and returning the first error writeTextDelta returns. It
	// must return io.ErrUnexpectedEOF, possibly wrapped, when the stream ends
	// before its terminal event, a *ProviderReportedError for an error event,
	// and reader errors wrapped with %w. It is required when EncodeRequest can
	// set EncodedRequest.IsStreaming.
	DecodeStream func(events *providerhttp.ServerSentEventReader, writeTextDelta func(delta string) error) (DecodedResponse, error)
	// FinishReasons maps each provider finish token that the pipeline accepts to
	// a FinishReason. A token that is missing selects invalidResponse.
	FinishReasons map[string]FinishReason
	// ErrorRules classify non-2xx responses before the defaults documented on ErrorRule.
	ErrorRules []ErrorRule
	// ErrorTokenPointers are RFC 6901 JSON pointers, such as "/error/code",
	// read from a non-2xx body with providerhttp.ReadErrorTokens. Their tokens
	// feed ErrorRules and the Failure message.
	ErrorTokenPointers []string
	// ReadErrorRetryDelay optionally reads a provider-specific retry delay from
	// a non-2xx body, such as Google RetryInfo. A positive result replaces the
	// Retry-After header. Nil uses only Retry-After.
	ReadErrorRetryDelay func(errorBody []byte) time.Duration
	// RequestIDHeaders lists response headers, in preference order, whose first
	// present value becomes the Receipt provider request ID.
	RequestIDHeaders []string
	// RateLimitHeaders lists response headers copied into Receipt metadata.
	// Only listed headers are copied.
	RateLimitHeaders []string
	// StallTimeout cancels an attempt and returns Retry when the provider sends
	// no response byte for this long, counting keep-alive comments and blank
	// lines. Zero disables the rule, so the connection's request timeout bounds
	// the attempt.
	StallTimeout time.Duration
}

// RequestFeatures declares the optional TextGenerationRequest fields a wire
// format sends. Model and Messages are always required. Use a keyed literal:
// a feature added in a later SDK release is false for existing wire formats,
// which then reject the new field instead of ignoring it.
type RequestFeatures struct {
	// SupportsInstructions supports TextGenerationRequest.Instructions.
	SupportsInstructions bool
	// SupportsStructuredOutput supports TextGenerationRequest.StructuredOutput.
	SupportsStructuredOutput bool
	// SupportsMaxOutputTokens supports TextGenerationRequest.MaxOutputTokens.
	SupportsMaxOutputTokens bool
	// SupportsTemperature supports TextGenerationRequest.Temperature.
	SupportsTemperature bool
	// SupportsReasoningEffort supports TextGenerationRequest.ReasoningEffort.
	SupportsReasoningEffort bool
}

// CredentialHeader names the request header that carries the credential. The
// header value is Prefix followed by the credential.
type CredentialHeader struct {
	// Name is the header name, such as "Authorization" or "x-api-key".
	Name string
	// Prefix precedes the credential, such as "Bearer ". Empty sends the credential alone.
	Prefix string
}

type temperaturePolicyKind uint8

const (
	temperatureAccepted temperaturePolicyKind = iota
	temperatureRange
	temperatureNotAccepted
)

// TemperaturePolicy states whether a model accepts TextGenerationRequest.Temperature.
// The zero value is TemperatureAccepted.
type TemperaturePolicy struct {
	kind    temperaturePolicyKind
	minimum float64
	maximum float64
}

// TemperatureAccepted accepts any finite, non-negative temperature and leaves
// range checks to the provider, whose rejection selects providerRejected.
func TemperatureAccepted() TemperaturePolicy { return TemperaturePolicy{kind: temperatureAccepted} }

// TemperatureRange accepts temperatures from minimum through maximum inclusive
// and selects defect for any other value. Use equal bounds for a model that
// accepts only one value. Bounds must be numbers with minimum at most
// maximum; otherwise every temperature selects defect.
func TemperatureRange(minimum float64, maximum float64) TemperaturePolicy {
	return TemperaturePolicy{kind: temperatureRange, minimum: minimum, maximum: maximum}
}

// TemperatureNotAccepted selects defect for any temperature, for models with fixed sampling.
func TemperatureNotAccepted() TemperaturePolicy {
	return TemperaturePolicy{kind: temperatureNotAccepted}
}

// StructuredOutputMode selects how a model is asked for structured output.
type StructuredOutputMode string

const (
	// StructuredOutputModeNone means the model cannot return structured output;
	// a request for it selects defect. It is the zero value.
	StructuredOutputModeNone StructuredOutputMode = ""
	// StructuredOutputModeJSONSchema sends the schema for provider-enforced output.
	StructuredOutputModeJSONSchema StructuredOutputMode = "jsonSchema"
	// StructuredOutputModeJSONObjectWithInstruction requests a JSON object and
	// appends an instruction that names the schema and includes it. It
	// requires the Instructions feature.
	StructuredOutputModeJSONObjectWithInstruction StructuredOutputMode = "jsonObjectWithInstruction"
)

// StructuredOutputRules describes how one model receives a structured-output schema.
// Post-validation always uses the application's original schema.
type StructuredOutputRules struct {
	// Mode selects the request form; the zero value rejects structured output.
	Mode StructuredOutputMode
	// KeywordsMovedToDescription lists constraint keywords the provider rejects:
	// minimum, maximum, minLength, maxLength, minItems, maxItems, or format.
	// They are removed from the provider schema and described in the node's
	// description text instead.
	KeywordsMovedToDescription []string
	// KeywordsRemoved lists keywords removed from the provider schema without a
	// description: any movable keyword, title, or additionalProperties.
	KeywordsRemoved []string
}

// Validate returns an error when Mode is unknown or a keyword list names a
// keyword it cannot hold. The pipeline also runs it for every structured
// request and selects defect on failure.
func (rules StructuredOutputRules) Validate() error {
	switch rules.Mode {
	case StructuredOutputModeNone, StructuredOutputModeJSONSchema, StructuredOutputModeJSONObjectWithInstruction:
	default:
		return fmt.Errorf("structured output mode %q is invalid", rules.Mode)
	}
	for _, keyword := range rules.KeywordsMovedToDescription {
		if !movableSchemaKeywords[keyword] {
			return fmt.Errorf("schema keyword %q cannot be moved to a description", keyword)
		}
	}
	for _, keyword := range rules.KeywordsRemoved {
		if !removableSchemaKeywords[keyword] {
			return fmt.Errorf("schema keyword %q cannot be removed", keyword)
		}
	}
	return nil
}

// ModelRequestRules are the request rules for one model.
type ModelRequestRules struct {
	// Temperature states whether the model accepts a temperature.
	Temperature TemperaturePolicy
	// ReasoningEfforts maps each effort the model accepts to its wire value.
	// Nil or empty rejects every effort.
	ReasoningEfforts map[ReasoningEffort]string
	// StructuredOutput describes how the model receives a schema.
	StructuredOutput StructuredOutputRules
}

// EncodeRequestInput is the validated request the wire format encodes.
type EncodeRequestInput struct {
	// Request is the application request after validation. Model is the
	// canonical model ID; Instructions include any JSON instruction the
	// structured-output mode requires; StructuredOutput.Schema is the
	// provider schema after RulesForModel transforms.
	Request TextGenerationRequest
	// Rules are the rules RulesForModel returned for Request.Model.
	Rules ModelRequestRules
}

// EncodedRequest is one provider HTTP request without its credential.
type EncodedRequest struct {
	// Path is appended to the connection's base URL. It starts with "/", may
	// hold a query, and never holds the credential.
	Path string
	// Body is the JSON request body sent as application/json.
	Body []byte
	// Header holds extra request headers. It must not set the credential
	// header, Content-Type, Content-Length, Accept, or Host.
	Header http.Header
	// IsStreaming asks for a text/event-stream response, which DecodeStream
	// decodes. A 2xx response with any other Content-Type is decoded by
	// DecodeResponse, as when a gateway ignores the streaming request, and
	// selects invalidResponse when the wire format has no DecodeResponse.
	IsStreaming bool
}

// DecodedResponse is what a wire format reads from one 2xx response.
type DecodedResponse struct {
	// Parts holds the output in order. The pipeline joins parts whose
	// IsReasoning is false into the response text.
	Parts []ResponsePart
	// ServedModel is the provider's model echo, or empty.
	ServedModel string
	// ResponseID is the provider's response identifier, or empty.
	ResponseID string
	// ProviderFinishReason is the provider's finish token, mapped through
	// WireFormat.FinishReasons. It is ignored when IsRefusal is true.
	ProviderFinishReason string
	// IsRefusal reports that the model refused instead of answering.
	IsRefusal bool
	// Usage is the reported token usage.
	Usage Usage
}

// ResponsePart is one piece of model output.
type ResponsePart struct {
	// Text is the part's text.
	Text string
	// IsReasoning marks reasoning or thinking output, which is never returned as text.
	IsReasoning bool
}

// cloneWireFormat copies the map and slices, so later changes to the caller's value cannot bypass validation.
func cloneWireFormat(wireFormat WireFormat) WireFormat {
	wireFormat.FinishReasons = maps.Clone(wireFormat.FinishReasons)
	wireFormat.ErrorRules = slices.Clone(wireFormat.ErrorRules)
	wireFormat.ErrorTokenPointers = slices.Clone(wireFormat.ErrorTokenPointers)
	wireFormat.RequestIDHeaders = slices.Clone(wireFormat.RequestIDHeaders)
	wireFormat.RateLimitHeaders = slices.Clone(wireFormat.RateLimitHeaders)
	return wireFormat
}

func validateWireFormat(wireFormat *WireFormat) error {
	if wireFormat.ProviderName == "" {
		return fmt.Errorf("wire format provider name is required")
	}
	if wireFormat.ModelIDRule != ModelIDRuleBody && wireFormat.ModelIDRule != ModelIDRulePathSegment {
		return fmt.Errorf("wire format model ID rule is invalid")
	}
	if wireFormat.RulesForModel == nil || wireFormat.EncodeRequest == nil {
		return fmt.Errorf("wire format RulesForModel and EncodeRequest are required")
	}
	if wireFormat.DecodeResponse == nil && wireFormat.DecodeStream == nil {
		return fmt.Errorf("wire format requires DecodeResponse, DecodeStream, or both")
	}
	if !headerNamePattern.MatchString(wireFormat.CredentialHeader.Name) {
		return fmt.Errorf("wire format credential header name is invalid")
	}
	for index := 0; index < len(wireFormat.CredentialHeader.Prefix); index++ {
		if character := wireFormat.CredentialHeader.Prefix[index]; character < ' ' || character > '~' {
			return fmt.Errorf("wire format credential header prefix must be printable ASCII")
		}
	}
	for token, reason := range wireFormat.FinishReasons {
		if !providerTokenPattern.MatchString(token) {
			return fmt.Errorf("wire format finish token must match %s", providerTokenPattern)
		}
		switch reason {
		case FinishReasonStop, FinishReasonLength, FinishReasonContentPolicy, FinishReasonRefusal:
		default:
			return fmt.Errorf("wire format finish token %q maps to an invalid finish reason", token)
		}
	}
	if err := validateErrorRules(wireFormat.ErrorRules); err != nil {
		return err
	}
	for _, pointer := range wireFormat.ErrorTokenPointers {
		if pointer != "" && pointer[0] != '/' {
			return fmt.Errorf("wire format error-token pointer must be empty or start with /")
		}
	}
	for _, name := range append(append([]string(nil), wireFormat.RequestIDHeaders...), wireFormat.RateLimitHeaders...) {
		if !headerNamePattern.MatchString(name) {
			return fmt.Errorf("wire format response header name is invalid")
		}
	}
	if wireFormat.StallTimeout < 0 {
		return fmt.Errorf("wire format stall timeout cannot be negative")
	}
	return nil
}

func validateTemperature(temperature float64, policy TemperaturePolicy) error {
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) || temperature < 0 {
		return fmt.Errorf("temperature must be a finite, non-negative number")
	}
	switch policy.kind {
	case temperatureNotAccepted:
		return fmt.Errorf("the model does not accept a temperature; leave Temperature nil")
	case temperatureRange:
		if math.IsNaN(policy.minimum) || math.IsNaN(policy.maximum) || policy.minimum > policy.maximum {
			return fmt.Errorf("the wire format's temperature range is invalid")
		}
		if temperature < policy.minimum || temperature > policy.maximum {
			return fmt.Errorf("the model accepts a temperature from %g through %g", policy.minimum, policy.maximum)
		}
	}
	return nil
}

func validateEncodedRequest(encoded EncodedRequest, credentialHeaderName string) error {
	if len(encoded.Path) == 0 || encoded.Path[0] != '/' {
		return fmt.Errorf("encoded request path must start with /")
	}
	for index := 0; index < len(encoded.Path); index++ {
		if character := encoded.Path[index]; character <= ' ' || character > '~' || character == '#' {
			return fmt.Errorf("encoded request path must be printable ASCII without spaces or fragments")
		}
	}
	reserved := map[string]bool{
		textproto.CanonicalMIMEHeaderKey(credentialHeaderName): true, "Content-Type": true,
		"Content-Length": true, "Accept": true, "Host": true,
	}
	for name, values := range encoded.Header {
		if !headerNamePattern.MatchString(name) || reserved[textproto.CanonicalMIMEHeaderKey(name)] {
			return fmt.Errorf("encoded request header name is invalid or reserved")
		}
		for _, value := range values {
			for index := 0; index < len(value); index++ {
				if character := value[index]; character < ' ' || character > '~' {
					return fmt.Errorf("encoded request header value must be printable ASCII")
				}
			}
		}
	}
	return nil
}
