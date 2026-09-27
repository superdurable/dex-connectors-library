// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package qwen implements the Alibaba Cloud Model Studio OpenAI-compatible
// Chat Completions endpoint as the provider-neutral generateText Dex Connector
// Query.
//
// The Query runs on the shared sdkgo/llm pipeline with the openaichat wire
// format; profile.go declares Model Studio's dialect. Applications build a
// Connection once at startup and wire qwen.NewGenerateTextStep into a Flow, as
// the runnable example in examples/summarize-text does.
package qwen

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// requestTimeout stays 30 seconds below the 900-second Execute timeout, so a stalled exchange returns Retry first.
	requestTimeout      = 870 * time.Second
	maxStreamEventBytes = 1 << 20
)

// GenerateTextRequest is the provider-neutral generateText input shared by every lab connector.
type GenerateTextRequest = llm.TextGenerationRequest

// GenerateTextResponse is the provider-neutral generateText output shared by every lab connector.
type GenerateTextResponse = llm.TextGenerationResponse

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
// the API key is never forwarded, and applies an 870-second timeout when the
// client sets none. The caller keeps ownership of client.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{httpClient: client} }

// WithBaseURLForTest sends provider calls to a loopback test server, such as
// llmtest.FakeProvider, instead of the configured endpoint. The path
// /chat/completions is appended to baseURL. New rejects a baseURL whose host
// is not localhost, 127.0.0.0/8, or ::1, so the option cannot send the API key
// to another host.
func WithBaseURLForTest(baseURL string) Option { return baseURLForTestOption{baseURL: baseURL} }

// Client calls Alibaba Cloud Model Studio. Its methods never modify it, so it
// is safe for concurrent use when the credential provider is.
type Client struct {
	generateText *llm.TextGenerationQuery
}

// New validates config and returns a Client. A blank config.Model uses
// qwen3.7-plus, a blank config.Endpoint uses the Singapore endpoint
// https://dashscope-intl.aliyuncs.com/compatible-mode/v1, and a zero
// config.MaxResponseBytes uses 8 MiB. The endpoint must be one of the fixed
// DashScope endpoints for Singapore, China (Hong Kong), or China (Beijing),
// with or without a trailing slash; workspace-dedicated endpoints are
// rejected. Credentials are resolved again for every provider call, so a
// replaced key takes effect without a restart. New returns an error for a nil
// credential provider, an invalid model, endpoint, or response limit, a nil
// option, or a non-loopback WithBaseURLForTest URL; it makes no provider
// request.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("qwen credential provider is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("qwen: %w", err)
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil || !supportedEndpoints[endpoint] {
		return nil, fmt.Errorf("qwen endpoint must be a DashScope endpoint for Singapore, China (Hong Kong), or China (Beijing)")
	}
	resolved := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("qwen connector option is nil")
		}
		option.applyClientOption(&resolved)
	}
	baseURL := endpoint
	if resolved.hasBaseURLForTest {
		if !isLoopbackBaseURL(resolved.baseURLForTest) {
			return nil, fmt.Errorf("qwen test base URL must use a loopback host")
		}
		baseURL = resolved.baseURLForTest
	}
	wireFormat, err := openaichat.NewWireFormat(&chatProfile)
	if err != nil {
		return nil, fmt.Errorf("qwen: %w", err)
	}
	generateText, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: wireFormat,
		BaseURL: baseURL, ConnectionModel: config.Model,
		HTTPClient: resolved.httpClient, RequestTimeout: requestTimeout,
		ResolveCredential: func(call sdkgo.Call) (sdkgo.SecretString, error) {
			credential, err := credentials.Resolve(call)
			return credential.APIKey, err
		},
		MaxResponseBytes:    config.MaxResponseBytes,
		MaxStreamEventBytes: maxStreamEventBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("qwen: %w", err)
	}
	return &Client{generateText: generateText}, nil
}

// GenerateText returns the generateText Query, which NewGenerateTextStep and
// the llmtest suites run.
func (client *Client) GenerateText() *llm.TextGenerationQuery {
	return client.generateText
}

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
