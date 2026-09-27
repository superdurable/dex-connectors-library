// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fixturellm

import (
	"cmp"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

const (
	// requestTimeout stays 30 seconds below the 900-second Execute timeout, so a stalled exchange returns Retry first.
	requestTimeout          = 870 * time.Second
	defaultMaxResponseBytes = 8 << 20
	maxStreamEventBytes     = 1 << 20
)

// temperatureNotAccepted is the reasoning models' fixed-sampling rule.
var temperatureNotAccepted = llm.TemperatureNotAccepted()

// chatProfile declares the fixture provider's Chat Completions dialect.
var chatProfile = openaichat.Profile{
	ProviderName:               ConnectorID,
	ChatCompletionsPath:        "/v1/chat/completions",
	InstructionsRole:           openaichat.InstructionsRoleDeveloper,
	MaxTokensField:             openaichat.MaxTokensFieldMaxCompletionTokens,
	Temperature:                llm.TemperatureRange(0, 1.5),
	StructuredOutput:           llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
	ShouldSendStrictJSONSchema: true,
	Streaming:                  openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage:   true,
	ErrorRules: []llm.ErrorRule{
		{StatusCode: http.StatusTooManyRequests, ErrorToken: "fixture_quota_exhausted", Outcome: llm.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusBadRequest, ErrorToken: "fixture_content_filter", Outcome: llm.BlockedOutcome()},
	},
	RateLimitHeaders: []string{"x-ratelimit-remaining-requests"},
	ModelRules: []openaichat.ModelRule{{
		ModelIDPrefix: "fixture-reasoner", Temperature: &temperatureNotAccepted,
		ReasoningEfforts: map[llm.ReasoningEffort]string{llm.ReasoningEffortLow: "low", llm.ReasoningEffortHigh: "high"},
	}},
}

// Option configures a Client dependency that is not part of Config.
type Option interface {
	applyClientOption(*clientOptions)
}

type clientOptions struct {
	httpClient *http.Client
}

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.httpClient
}

// WithHTTPClient supplies the HTTP client for provider calls. The Client uses
// a copy that never follows redirects; the caller keeps ownership of client.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{httpClient: client} }

// Client calls the fixture provider. It is safe for concurrent use when the
// credential provider is.
type Client struct {
	generateText *llm.TextGenerationQuery
}

// New validates config and returns a Client. Credentials are resolved again
// for every provider call.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("fixture-llm credential provider is required")
	}
	resolved := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("fixture-llm connector option is nil")
		}
		option.applyClientOption(&resolved)
	}
	wireFormat, err := openaichat.NewWireFormat(&chatProfile)
	if err != nil {
		return nil, err
	}
	generateText, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: wireFormat,
		BaseURL: config.Endpoint, ConnectionModel: config.Model,
		HTTPClient: resolved.httpClient, RequestTimeout: requestTimeout,
		ResolveCredential: func(call sdkgo.Call) (sdkgo.SecretString, error) {
			credential, err := credentials.Resolve(call)
			return credential.APIKey, err
		},
		MaxResponseBytes:    cmp.Or(config.MaxResponseBytes, defaultMaxResponseBytes),
		MaxStreamEventBytes: maxStreamEventBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("fixture-llm: %w", err)
	}
	return &Client{generateText: generateText}, nil
}

// GenerateText returns the generateText Query, which the generated Step
// factory and the llmtest suites run.
func (client *Client) GenerateText() *llm.TextGenerationQuery {
	return client.generateText
}
