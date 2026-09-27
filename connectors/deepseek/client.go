// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package deepseek implements the DeepSeek API Chat Completions endpoint as the
// provider-neutral generateText Dex Connector Query.
//
// The Query runs on the shared sdkgo/llm pipeline with the openaichat wire
// format; profile.go declares DeepSeek's dialect. Applications build a
// Connection once at startup and wire deepseek.NewGenerateTextStep into a
// Flow, as the runnable example in examples/summarize-text does.
package deepseek

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

const (
	// requestTimeout stays 30 seconds below the 1200-second Execute timeout, so a stalled exchange returns Retry first.
	requestTimeout      = 1170 * time.Second
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
	httpClient *http.Client
	baseURL    string
}

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.httpClient
}

type baseURLForTestOption struct{ baseURL string }

func (option baseURLForTestOption) applyClientOption(options *clientOptions) {
	options.baseURL = option.baseURL
}

// WithHTTPClient supplies the HTTP client for provider calls, such as one with
// a proxy transport. The Client uses a copy that never follows redirects, so
// the API key is never forwarded, and applies a 1170-second timeout when the
// client sets none. A client Timeout must be below the 1200-second Execute
// timeout, or New returns an error; 1170 seconds or less lets a stalled
// exchange return Retry before the Step times out. The caller keeps ownership
// of client.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{httpClient: client} }

// WithBaseURLForTest sends provider calls to a loopback test server, such as
// llmtest.FakeProvider, instead of https://api.deepseek.com. The path
// /chat/completions is appended to baseURL. New rejects a baseURL whose host
// is not localhost, 127.0.0.0/8, or ::1, so the option cannot send the API
// key to another host.
func WithBaseURLForTest(baseURL string) Option { return baseURLForTestOption{baseURL: baseURL} }

// Client calls the DeepSeek API. Its methods never modify it, so it is safe
// for concurrent use when the credential provider is.
type Client struct {
	generateText *llm.TextGenerationQuery
}

// New validates config and returns a Client. New trims surrounding whitespace
// from config.Model; a blank config.Model uses deepseek-flash and a zero
// config.MaxResponseBytes uses 64 MiB. Credentials are resolved again for every
// provider call, so a replaced key takes effect without a restart. New returns
// an error for a nil credential provider, an invalid model or response limit, a
// nil option, a WithHTTPClient client whose Timeout is 1200 seconds or more, or
// a non-loopback WithBaseURLForTest URL; it makes no provider request.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("deepseek credential provider is required")
	}
	// Dex Web collects the model in a free-text field.
	config.Model = strings.TrimSpace(config.Model)
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	resolved := clientOptions{baseURL: apiBaseURL}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("deepseek connector option is nil")
		}
		option.applyClientOption(&resolved)
	}
	if resolved.baseURL != apiBaseURL && !isLoopbackBaseURL(resolved.baseURL) {
		return nil, fmt.Errorf("deepseek test base URL must use a loopback host")
	}
	wireFormat, err := openaichat.NewWireFormat(&chatProfile)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	generateText, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: wireFormat,
		BaseURL: resolved.baseURL, ConnectionModel: config.Model,
		HTTPClient: resolved.httpClient, RequestTimeout: requestTimeout,
		ResolveCredential: func(call sdkgo.Call) (sdkgo.SecretString, error) {
			credential, err := credentials.Resolve(call)
			return credential.APIKey, err
		},
		MaxResponseBytes:    config.MaxResponseBytes,
		MaxStreamEventBytes: maxStreamEventBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
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
