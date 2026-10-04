// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package llm implements the llm connector, the one text generation connector
// for Dex applications: a provider-neutral generateText Dex Connector Query
// that runs a model of the provider the connection names.
//
// A connection names one provider in Config.Provider, such as openai or
// anthropic, and holds that provider's API key. Every attempt sends one
// request to that provider's API in its own wire format: the OpenAI Responses
// API, the Claude Messages API, the Gemini generateContent method, or the
// OpenAI-compatible Chat Completions API of Qwen, DeepSeek, Meta, Mistral,
// Kimi, or xAI. Model IDs are the provider's own, such as claude-sonnet-5,
// never prefixed with the provider.
//
// Applications load the project configuration once at startup, open the
// connection with NewProjectConnection, and wire NewGenerateTextStep into a
// Flow, as the runnable example in examples/summarize-text does:
//
//	project, err := projectconfig.LoadFromEnvironment(ctx)
//	if err != nil {
//		return err
//	}
//	connection, err := llm.NewProjectConnection(project, summarizetext.ConnectionName)
//	if err != nil {
//		return err
//	}
//
// The package name equals the last element of its module path, so importers
// need no alias.
package llm

import (
	"cmp"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

// maxStreamEventBytes caps one server-sent event; OpenAI's final event repeats the whole response.
const maxStreamEventBytes = 1 << 20

// GenerateTextRequest is the provider-neutral generateText input. Its Model is
// the provider's own model ID, or blank for the connection's model.
type GenerateTextRequest = textgen.TextGenerationRequest

// GenerateTextResponse is the provider-neutral generateText output. Its
// RequestedModel is the provider's model ID that the request was sent for.
type GenerateTextResponse = textgen.TextGenerationResponse

// Option configures a Client dependency that is not part of Config.
type Option interface {
	applyClientOption(*clientOptions)
}

type clientOptions struct {
	httpClient        *http.Client
	baseURLForTest    string
	hasBaseURLForTest bool
}

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.httpClient
}

type baseURLForTestOption struct{ baseURL string }

func (option baseURLForTestOption) applyClientOption(options *clientOptions) {
	options.baseURLForTest, options.hasBaseURLForTest = option.baseURL, true
}

// WithHTTPClient supplies the HTTP client for provider calls, such as one with
// a proxy transport. The Client uses a copy that never follows redirects, so
// the API key is never forwarded, and bounds each exchange by the provider's
// request timeout when the client sets none: 1170 seconds for deepseek and 870
// seconds for every other provider. New rejects a client Timeout of 1200
// seconds or more, because the Execute timeout must end after the exchange.
// The caller keeps ownership of client.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{httpClient: client} }

// WithBaseURLForTest sends provider calls to a loopback test server, such as
// textgentest.FakeProvider, instead of the provider's API. The provider's API
// path, such as /chat/completions or /v1/messages, is appended to baseURL.
// New rejects a baseURL whose host is not localhost, 127.0.0.0/8, or ::1, so
// the option cannot send the API key to another host. Config.Region is still
// validated.
func WithBaseURLForTest(baseURL string) Option { return baseURLForTestOption{baseURL: baseURL} }

// Client calls the text generation API of the connection's provider. It holds
// no secrets and its methods never modify it, so it is safe for concurrent use
// when the credential provider is.
type Client struct {
	generateText *textgen.TextGenerationQuery
}

// New validates config and returns a Client for config.Provider, which is
// required. New trims surrounding whitespace from config.Model and
// config.AnthropicWorkspaceID. A blank config.Model uses the provider's
// default model, a blank config.Region uses global, and a zero
// config.MaxResponseBytes uses 64 MiB for deepseek and 8 MiB for every other
// provider.
//
// New returns an error for a nil credential provider, a missing or unknown
// provider, a region the provider does not serve, a model ID the provider's
// model-ID rule rejects, an anthropicWorkspaceId on a provider other than
// anthropic or one that is not a wrkspc_ workspace ID, a negative response
// limit, a nil option, a WithHTTPClient client whose Timeout is 1200 seconds
// or more, or a non-loopback WithBaseURLForTest URL. Credentials are resolved
// again for every provider call, so a replaced key takes effect without a
// restart. New makes no provider request, and its errors never repeat a
// configured model, workspace ID, or URL.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("llm credential provider is required")
	}
	// Dex Web collects these values in free-text fields.
	config.Model, config.AnthropicWorkspaceID = strings.TrimSpace(config.Model), strings.TrimSpace(config.AnthropicWorkspaceID)
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	api, isServed := providerAPIs[config.Provider]
	if !isServed {
		return nil, fmt.Errorf("llm: configuration provider is invalid")
	}
	baseURL, err := api.baseURLForRegion(config.Provider, config.Region)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	if config.AnthropicWorkspaceID != "" && config.Provider != ProviderAnthropic {
		return nil, fmt.Errorf("llm: configuration anthropicWorkspaceId applies only to provider anthropic; clear it for provider %s", config.Provider)
	}
	resolved := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("llm connector option is nil")
		}
		option.applyClientOption(&resolved)
	}
	if resolved.hasBaseURLForTest {
		if !isLoopbackBaseURL(resolved.baseURLForTest) {
			return nil, fmt.Errorf("llm test base URL must use a loopback host")
		}
		baseURL = resolved.baseURLForTest
	}
	wireFormat, err := api.newWireFormat(&config)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	generateText, err := textgen.NewTextGenerationQuery(&textgen.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: wireFormat,
		BaseURL: baseURL, ConnectionModel: cmp.Or(config.Model, api.defaultModel),
		HTTPClient: resolved.httpClient, RequestTimeout: api.requestTimeout,
		ResolveCredential: func(call sdkgo.Call) (sdkgo.SecretString, error) {
			credential, err := credentials.Resolve(call)
			return credential.APIKey, err
		},
		MaxResponseBytes:    cmp.Or(config.MaxResponseBytes, api.defaultMaxResponseBytes),
		MaxStreamEventBytes: maxStreamEventBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("llm %s: %w", config.Provider, err)
	}
	return &Client{generateText: generateText}, nil
}

// GenerateText returns the generateText Query, which NewGenerateTextStep and
// the textgentest suites run. Each Invoke makes at most one request, to the
// connection's provider.
func (client *Client) GenerateText() *textgen.TextGenerationQuery {
	return client.generateText
}

// isLoopbackBaseURL accepts only a localhost, 127.0.0.0/8, or ::1 host, so a test URL cannot receive the key elsewhere.
func isLoopbackBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
