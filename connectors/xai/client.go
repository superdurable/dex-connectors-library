// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package grok implements the xAI Chat Completions endpoint as the
// provider-neutral generateText Dex Connector Query for Grok models.
//
// The Query runs on the shared sdkgo/llm pipeline with the openaichat wire
// format; profile.go declares xAI's dialect and restores the shared Usage
// contract for xAI's reasoning-token accounting. Applications build a
// Connection once at startup and wire grok.NewGenerateTextStep into a Flow,
// as the runnable example in examples/summarize-text does. The module path
// ends in connectors/xai, the company directory, so importers name the
// package grok explicitly.
package grok

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
// is not localhost, 127.0.0.0/8, or ::1, so the option cannot send the API
// key to another host.
func WithBaseURLForTest(baseURL string) Option { return baseURLForTestOption{baseURL: baseURL} }

// Client calls the xAI API. Its methods never modify it, so it is safe for
// concurrent use when the credential provider is.
type Client struct {
	generateText *llm.TextGenerationQuery
}

// New validates config and returns a Client. A blank config.Model uses
// grok-4.3, a blank config.Endpoint uses https://api.x.ai/v1, and a zero
// config.MaxResponseBytes uses 8 MiB. config.Endpoint must be
// https://api.x.ai/v1 or the US regional https://us.api.x.ai/v1, with at most
// one trailing slash. Credentials are resolved again for every provider call,
// so a replaced key takes effect without a restart. New returns an error for a
// nil credential provider, an invalid model, endpoint, or response limit, a
// nil option, or a non-loopback WithBaseURLForTest URL; it makes no provider
// request.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("grok credential provider is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("grok: %w", err)
	}
	endpoint, err := validateXAIEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	resolved := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("grok connector option is nil")
		}
		option.applyClientOption(&resolved)
	}
	baseURL := endpoint
	if resolved.hasBaseURLForTest {
		if !isLoopbackBaseURL(resolved.baseURLForTest) {
			return nil, fmt.Errorf("grok test base URL must use a loopback host")
		}
		baseURL = resolved.baseURLForTest
	}
	wireFormat, err := openaichat.NewWireFormat(&chatProfile)
	if err != nil {
		return nil, fmt.Errorf("grok: %w", err)
	}
	generateText, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: withReasoningInOutputTokens(wireFormat),
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
		return nil, fmt.Errorf("grok: %w", err)
	}
	return &Client{generateText: generateText}, nil
}

// GenerateText returns the generateText Query, which NewGenerateTextStep and
// the llmtest suites run.
func (client *Client) GenerateText() *llm.TextGenerationQuery {
	return client.generateText
}

// validateXAIEndpoint accepts only xAI's two fixed hosts, so a configured endpoint cannot send the key elsewhere.
func validateXAIEndpoint(value string) (string, error) {
	endpoint := strings.TrimSuffix(value, "/")
	if endpoint != globalEndpoint && endpoint != usRegionalEndpoint {
		return "", fmt.Errorf("grok endpoint must be %s or %s", globalEndpoint, usRegionalEndpoint)
	}
	return endpoint, nil
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
