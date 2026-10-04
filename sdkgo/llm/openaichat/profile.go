// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openaichat is the OpenAI-compatible Chat Completions wire format for
// llm.TextGenerationQuery. A connector describes its provider declaratively in
// a Profile and passes the Profile to NewWireFormat; it writes no encoding or
// decoding code. The package contains no provider host, model ID, error code,
// or credential: those live in the connector's Profile and manifest.
//
// The wire format sends POST {base URL}{ChatCompletionsPath} with a JSON body
// holding only model, messages, the token-limit field, temperature,
// reasoning_effort, response_format, stream, and stream_options, each only
// when the request or Profile needs it. It decodes a chat.completion body or a
// chat.completion.chunk event stream, joins content text, and skips
// reasoning_content and thinking parts.
//
// A Profile cannot add other provider-specific body fields, such as a
// per-model thinking switch. Such a field needs a new Profile or ModelRule
// field in a later SDK release, not a wrapped EncodeRequest, because a wrapper
// runs after the AllowedRequestFields check and bypasses it.
package openaichat

import (
	"fmt"
	"net/textproto"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// InstructionsRole is the chat role that carries TextGenerationRequest.Instructions.
type InstructionsRole string

const (
	// InstructionsRoleSystem sends instructions as the first "system" message. It is the default.
	InstructionsRoleSystem InstructionsRole = "system"
	// InstructionsRoleDeveloper sends instructions as the first "developer" message.
	InstructionsRoleDeveloper InstructionsRole = "developer"
)

// MaxTokensField is the body field that carries TextGenerationRequest.MaxOutputTokens.
type MaxTokensField string

const (
	// MaxTokensFieldMaxTokens sends "max_tokens". It is the default.
	MaxTokensFieldMaxTokens MaxTokensField = "max_tokens"
	// MaxTokensFieldMaxCompletionTokens sends "max_completion_tokens".
	MaxTokensFieldMaxCompletionTokens MaxTokensField = "max_completion_tokens"
)

// StreamingPolicy selects whether requests ask for a server-sent event stream.
type StreamingPolicy string

const (
	// StreamingPolicyNever sends non-streaming requests. It is the default.
	StreamingPolicyNever StreamingPolicy = "never"
	// StreamingPolicyAlways sends "stream": true and decodes chat.completion.chunk
	// events, writing each content delta to the Step's text Stream when one is
	// configured. Prefer it for providers whose gateways time out silent
	// requests.
	StreamingPolicyAlways StreamingPolicy = "always"
)

var (
	requestFieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	headerNamePattern   = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")
)

// Profile declares one provider's Chat Completions dialect. Zero values select
// the documented defaults, so a Profile lists only what differs. Pass it to
// NewWireFormat by pointer, and do not modify it, or the maps and slices it
// references, afterwards: the wire format keeps using them.
type Profile struct {
	// ProviderName names the provider in Failures and Receipts, such as "deepseek". It is required.
	ProviderName string
	// ChatCompletionsPath is appended to the connection's base URL. It starts
	// with "/". Empty uses "/chat/completions".
	ChatCompletionsPath string
	// CredentialHeader is where the credential travels. An empty Name uses
	// "Authorization" with the "Bearer " prefix.
	CredentialHeader llm.CredentialHeader
	// FixedHeaders are sent with every request, such as an API version. They
	// must not name the credential header, Content-Type, Content-Length,
	// Accept, or Host, and must not hold secrets.
	FixedHeaders map[string]string
	// InstructionsRole carries instructions. Empty uses InstructionsRoleSystem.
	InstructionsRole InstructionsRole
	// MaxTokensField carries MaxOutputTokens. Empty uses MaxTokensFieldMaxTokens.
	MaxTokensField MaxTokensField
	// ReasoningEfforts maps each accepted effort to its "reasoning_effort" wire
	// value. Nil or empty rejects every effort with defect.
	ReasoningEfforts map[llm.ReasoningEffort]string
	// Temperature states whether models accept a temperature. The zero value accepts one.
	Temperature llm.TemperaturePolicy
	// StructuredOutput states how models receive a schema. The zero value
	// rejects structured output. StructuredOutputModeJSONSchema sends
	// response_format type json_schema; StructuredOutputModeJSONObjectWithInstruction
	// sends type json_object and appends the schema instruction.
	StructuredOutput llm.StructuredOutputRules
	// ShouldSendStrictJSONSchema sends "strict": true inside json_schema.
	ShouldSendStrictJSONSchema bool
	// Streaming selects streamed or complete responses. Empty uses StreamingPolicyNever.
	Streaming StreamingPolicy
	// ShouldRequestStreamUsage sends stream_options.include_usage so a stream reports usage.
	ShouldRequestStreamUsage bool
	// AllowedRequestFields, when non-empty, lists every top-level body field
	// the provider accepts. A request that would send another field selects
	// defect, for providers whose request schema rejects unknown fields. The
	// list must include "model" and "messages", plus "stream" when any model
	// streams and "stream_options" when streams also request usage.
	AllowedRequestFields []string
	// FinishReasons extends or overrides the default finish-token map, which is
	// "stop" to stop, "length" to length, and "content_filter" to content
	// policy. Any other token selects invalidResponse.
	FinishReasons map[string]llm.FinishReason
	// ErrorRules classify non-2xx responses before the llm.ErrorRule defaults.
	ErrorRules []llm.ErrorRule
	// ErrorTokenPointers name the error tokens read from a non-2xx body. Nil
	// uses "/error/type" and "/error/code".
	ErrorTokenPointers []string
	// RequestIDHeaders name the response headers that hold the request ID. Nil uses "x-request-id".
	RequestIDHeaders []string
	// RateLimitHeaders name the response headers copied into Receipt metadata.
	RateLimitHeaders []string
	// StallTimeout returns Retry when no byte arrives for this long; zero disables it.
	StallTimeout time.Duration
	// ModelRules override Profile values for matching models. The first rule
	// that matches a model applies; later rules are ignored for that model.
	ModelRules []ModelRule
}

// ModelRule overrides Profile values for models whose ID matches it. Exactly
// one of ModelIDPrefix and ModelIDPattern is set. A nil or empty override
// keeps the Profile value.
type ModelRule struct {
	// ModelIDPrefix matches model IDs that start with it.
	ModelIDPrefix string
	// ModelIDPattern matches model IDs with an RE2 expression anchored by ^ and $.
	ModelIDPattern string
	// Temperature replaces Profile.Temperature.
	Temperature *llm.TemperaturePolicy
	// ReasoningEfforts replaces Profile.ReasoningEfforts when non-nil; an empty
	// map rejects every effort.
	ReasoningEfforts map[llm.ReasoningEffort]string
	// StructuredOutput replaces Profile.StructuredOutput.
	StructuredOutput *llm.StructuredOutputRules
	// MaxTokensField replaces Profile.MaxTokensField.
	MaxTokensField MaxTokensField
	// Streaming replaces Profile.Streaming.
	Streaming StreamingPolicy
}

// modelSettings are the Profile values after applying the first matching ModelRule.
type modelSettings struct {
	rules          llm.ModelRequestRules
	maxTokensField MaxTokensField
	streaming      StreamingPolicy
}

// compiledModelRule is a validated ModelRule with its pattern compiled.
type compiledModelRule struct {
	rule    ModelRule
	pattern *regexp.Regexp
}

func (rule compiledModelRule) matches(model string) bool {
	if rule.pattern != nil {
		return rule.pattern.MatchString(model)
	}
	return strings.HasPrefix(model, rule.rule.ModelIDPrefix)
}

func validateProfile(profile *Profile) ([]compiledModelRule, error) {
	if strings.TrimSpace(profile.ProviderName) == "" {
		return nil, fmt.Errorf("profile provider name is required")
	}
	if path := profile.ChatCompletionsPath; path != "" {
		if path[0] != '/' || strings.ContainsAny(path, "?# ") {
			return nil, fmt.Errorf("profile chat completions path must start with / and hold no query, fragment, or space")
		}
	}
	credentialHeaderName := profile.CredentialHeader.Name
	if credentialHeaderName == "" {
		credentialHeaderName = "Authorization"
	}
	reserved := map[string]bool{
		textproto.CanonicalMIMEHeaderKey(credentialHeaderName): true, "Content-Type": true,
		"Content-Length": true, "Accept": true, "Host": true,
	}
	for name, value := range profile.FixedHeaders {
		if !headerNamePattern.MatchString(name) || reserved[textproto.CanonicalMIMEHeaderKey(name)] {
			return nil, fmt.Errorf("profile fixed header name is invalid or reserved")
		}
		for index := 0; index < len(value); index++ {
			if value[index] < ' ' || value[index] > '~' {
				return nil, fmt.Errorf("profile fixed header value must be printable ASCII")
			}
		}
	}
	switch profile.InstructionsRole {
	case "", InstructionsRoleSystem, InstructionsRoleDeveloper:
	default:
		return nil, fmt.Errorf("profile instructions role is invalid")
	}
	if err := validateModelSettings(profile.MaxTokensField, profile.Streaming, profile.ReasoningEfforts, profile.StructuredOutput); err != nil {
		return nil, fmt.Errorf("profile %w", err)
	}
	for _, field := range profile.AllowedRequestFields {
		if !requestFieldPattern.MatchString(field) {
			return nil, fmt.Errorf("profile allowed request field must match %s", requestFieldPattern)
		}
	}
	compiled := make([]compiledModelRule, 0, len(profile.ModelRules))
	for index, rule := range profile.ModelRules {
		compiledRule, err := compileModelRule(rule)
		if err != nil {
			return nil, fmt.Errorf("profile model rule %d %w", index, err)
		}
		compiled = append(compiled, compiledRule)
	}
	if err := validateAllowedRequestFields(profile); err != nil {
		return nil, err
	}
	return compiled, nil
}

// validateAllowedRequestFields requires an allowlist to hold every field that requests always or structurally send.
func validateAllowedRequestFields(profile *Profile) error {
	if len(profile.AllowedRequestFields) == 0 {
		return nil
	}
	required := []string{"model", "messages"}
	canStream := profile.Streaming == StreamingPolicyAlways
	for _, rule := range profile.ModelRules {
		canStream = canStream || rule.Streaming == StreamingPolicyAlways
	}
	if canStream {
		required = append(required, "stream")
		if profile.ShouldRequestStreamUsage {
			required = append(required, "stream_options")
		}
	}
	for _, field := range required {
		if !slices.Contains(profile.AllowedRequestFields, field) {
			return fmt.Errorf("profile allowed request fields must include %q, which the wire format sends", field)
		}
	}
	return nil
}

func compileModelRule(rule ModelRule) (compiledModelRule, error) {
	if (rule.ModelIDPrefix == "") == (rule.ModelIDPattern == "") {
		return compiledModelRule{}, fmt.Errorf("must set exactly one of ModelIDPrefix and ModelIDPattern")
	}
	compiled := compiledModelRule{rule: rule}
	if rule.ModelIDPattern != "" {
		if !strings.HasPrefix(rule.ModelIDPattern, "^") || !strings.HasSuffix(rule.ModelIDPattern, "$") {
			return compiledModelRule{}, fmt.Errorf("pattern must be anchored with ^ and $")
		}
		pattern, err := regexp.Compile(rule.ModelIDPattern)
		if err != nil {
			return compiledModelRule{}, fmt.Errorf("pattern is not valid RE2")
		}
		compiled.pattern = pattern
	}
	structuredOutput := llm.StructuredOutputRules{}
	if rule.StructuredOutput != nil {
		structuredOutput = *rule.StructuredOutput
	}
	if err := validateModelSettings(rule.MaxTokensField, rule.Streaming, rule.ReasoningEfforts, structuredOutput); err != nil {
		return compiledModelRule{}, err
	}
	return compiled, nil
}

func validateModelSettings(
	maxTokensField MaxTokensField, streaming StreamingPolicy, efforts map[llm.ReasoningEffort]string,
	structuredOutput llm.StructuredOutputRules,
) error {
	switch maxTokensField {
	case "", MaxTokensFieldMaxTokens, MaxTokensFieldMaxCompletionTokens:
	default:
		return fmt.Errorf("max tokens field is invalid")
	}
	switch streaming {
	case "", StreamingPolicyNever, StreamingPolicyAlways:
	default:
		return fmt.Errorf("streaming policy is invalid")
	}
	for effort, wireValue := range efforts {
		switch effort {
		case llm.ReasoningEffortNone, llm.ReasoningEffortMinimal, llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
			llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh, llm.ReasoningEffortMax:
		default:
			return fmt.Errorf("reasoning effort %q is unknown", effort)
		}
		if !requestFieldPattern.MatchString(wireValue) {
			return fmt.Errorf("reasoning effort wire value must match %s", requestFieldPattern)
		}
	}
	if err := structuredOutput.Validate(); err != nil {
		return err
	}
	return nil
}
