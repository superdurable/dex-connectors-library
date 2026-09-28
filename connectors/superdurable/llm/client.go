// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package llmrouter implements the llm connector: one provider-neutral
// generateText Dex Connector Query that runs an OpenAI, Claude, or Gemini
// model chosen as provider/model, such as anthropic/claude-sonnet-5.
//
// Each attempt parses the model, picks that provider's route, and invokes the
// released generateText Query of the OpenAI, Claude, or Gemini connector that
// this module's go.mod pins, with the caller's Call. The provider's Result,
// Failure, Receipt, and Retry come back unchanged, so a Step sees exactly
// what a direct call to that connector returns. The connection holds one
// optional API key per provider, and each key is sent only to its own
// provider.
//
// Applications build a Connection once at startup with NewLocalConnection,
// load each Step's model pick, and wire NewGenerateTextStep into a Flow, as
// the runnable example in examples/summarize-text does.
//
// The module path ends in the connector directory, llm, while the package is
// named llmrouter, so importers name it explicitly:
//
//	import llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
package llmrouter

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

// providerRequestTimeout is every pinned provider's request timeout; Execute
// must outlast it so a hung provider returns Retry.
const providerRequestTimeout = 870 * time.Second

// minimumHeartbeatTimeout is Dex's minimum non-zero heartbeat timeout, which
// the provider pipeline's 5-second heartbeat covers twice.
const minimumHeartbeatTimeout = 10 * time.Second

// GenerateTextRequest is the provider-neutral generateText input shared by
// every lab connector. Model is provider/model, a provider alone, or blank for
// the connection's model.
type GenerateTextRequest = llm.TextGenerationRequest

// GenerateTextResponse is the provider-neutral generateText output shared by
// every lab connector. Its RequestedModel is the provider's model ID without
// the provider prefix; QualifiedModel restores the prefix.
type GenerateTextResponse = llm.TextGenerationResponse

// Option configures a Client dependency that is not part of Config.
type Option interface {
	applyClientOption(*clientOptions)
}

type clientOptions struct {
	httpClient              *http.Client
	providerBaseURLsForTest map[Provider]string
}

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.httpClient
}

type providerBaseURLForTestOption struct {
	provider Provider
	baseURL  string
}

func (option providerBaseURLForTestOption) applyClientOption(options *clientOptions) {
	if options.providerBaseURLsForTest == nil {
		options.providerBaseURLsForTest = map[Provider]string{}
	}
	options.providerBaseURLsForTest[option.provider] = option.baseURL
}

// WithHTTPClient supplies the HTTP client every provider route uses, such as
// one with a proxy transport. Each provider connector uses its own copy that
// never follows redirects, so an API key is never forwarded, and applies its
// 870-second request timeout when the client sets none. New rejects a client
// Timeout of 900 seconds or more, because the Execute timeout must end after
// the exchange. The caller keeps ownership of client.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{httpClient: client} }

// WithProviderBaseURLForTest sends one provider's generation calls to a
// loopback test server, such as llmtest.FakeProvider, instead of that
// provider's public API. The provider connector appends its own API path to
// baseURL. New rejects an unknown provider and any baseURL other than an
// http or https URL whose host is exactly 127.0.0.1 or localhost, without user
// information, a query, or a fragment, so the option cannot send a key to
// another host. A later option for the same provider replaces an earlier one.
func WithProviderBaseURLForTest(provider Provider, baseURL string) Option {
	return providerBaseURLForTestOption{provider: provider, baseURL: baseURL}
}

// Client routes generateText calls to the OpenAI, Claude, and Gemini
// connectors. It holds no secrets and its methods never modify it, so it is
// safe for concurrent use when the credential provider is.
type Client struct {
	router *textGenerationRouter
}

// New validates config and returns a Client. config.Model is required: a
// provider/model or a provider alone, which uses that provider connector's
// default model. A zero config.MaxResponseBytes uses 8 MiB for every provider,
// and a non-blank config.AnthropicWorkspaceID must be a wrkspc_ workspace ID.
//
// New builds one route per provider with that provider connector's New. It
// returns an error for a nil credential provider, an invalid or unknown
// connection model, an invalid workspace ID or response limit, a nil option,
// an HTTP client Timeout of 900 seconds or more, an invalid
// WithProviderBaseURLForTest provider or URL, or a linked provider connector
// whose generateText needs a longer Execute timeout or declares other
// branches than llm's. Credentials are resolved again for every provider
// call, so a replaced key takes effect without a restart. New makes no
// provider request, and its errors never repeat configuration values.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("llm credential provider is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	resolved := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("llm connector option is nil")
		}
		option.applyClientOption(&resolved)
	}
	if err := validateClientOptions(&resolved, GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout); err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	if err := validateRouterDefinition(GenerateTextDefinition); err != nil {
		return nil, fmt.Errorf("llm generateText definition: %w", err)
	}
	routes, err := newProviderRoutes(&config, credentials, &resolved)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		if err := validateProviderRouteDefinition(route.connectorID, route.generateText.Definition(), GenerateTextDefinition); err != nil {
			return nil, err
		}
	}
	router, err := newTextGenerationRouter(GenerateTextDefinition, config.Model, credentials, routes)
	if err != nil {
		return nil, err
	}
	return &Client{router: router}, nil
}

// GenerateText returns the routing generateText Query, which
// NewGenerateTextStep runs. Each Invoke makes at most one provider exchange,
// through the provider that the resolved model selects.
func (client *Client) GenerateText() sdkgo.Query[GenerateTextRequest, GenerateTextResponse] {
	return client.router
}

func validateClientOptions(options *clientOptions, executeTimeout time.Duration) error {
	if options.httpClient != nil && options.httpClient.Timeout >= executeTimeout {
		return fmt.Errorf("the HTTP client Timeout must be shorter than the %.0f-second generateText Execute timeout", executeTimeout.Seconds())
	}
	for provider, baseURL := range options.providerBaseURLsForTest {
		if !provider.isRouted() {
			return fmt.Errorf("a test base URL names an unknown provider; use openai, anthropic, or gemini")
		}
		if !isLoopbackTestBaseURL(baseURL) {
			return fmt.Errorf("the %s test base URL must be http or https on 127.0.0.1 or localhost", provider)
		}
	}
	return nil
}

// isLoopbackTestBaseURL accepts only the loopback hosts every provider connector accepts for tests.
func isLoopbackTestBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "localhost"
}

// validateRouterDefinition repeats llm.NewTextGenerationQuery's definition checks for the routing Query.
func validateRouterDefinition(definition sdkgo.QueryDefinition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	if definition.Operation.OperationID != llm.TextGenerationOperationID {
		return fmt.Errorf("the operation ID must be %q", llm.TextGenerationOperationID)
	}
	if !hasTextGenerationBranches(definition) {
		return fmt.Errorf("the branches must equal llm.TextGenerationBranchDefinitions")
	}
	defaults := definition.StepDefaults
	if defaults.ExecuteDurability != dex.StepDurabilitySync {
		return fmt.Errorf("generateText must use sync Execute durability, because an async fallback attempt calls the provider again")
	}
	if defaults.HeartbeatTimeout != 0 && defaults.HeartbeatTimeout < minimumHeartbeatTimeout {
		return fmt.Errorf("the heartbeat timeout must be zero or at least %.0f seconds", minimumHeartbeatTimeout.Seconds())
	}
	if defaults.ExecuteMethodTimeout <= providerRequestTimeout {
		return fmt.Errorf("the Execute timeout must exceed the providers' %.0f-second request timeout", providerRequestTimeout.Seconds())
	}
	return nil
}

// validateProviderRouteDefinition rejects a provider connector, newer than
// go.mod pins, whose generateText llm's Step options cannot cover.
func validateProviderRouteDefinition(providerConnectorID string, providerDefinition, routerDefinition sdkgo.QueryDefinition) error {
	upgrade := fmt.Sprintf("Upgrade the llm connector, or require the %s connector version that the llm connector's go.mod lists.", providerConnectorID)
	if !hasTextGenerationBranches(providerDefinition) {
		return fmt.Errorf("the llm connector cannot route to the %s connector linked into this application: its generateText declares other branches than llm's. %s",
			providerConnectorID, upgrade)
	}
	if providerDefinition.StepDefaults.ExecuteMethodTimeout > routerDefinition.StepDefaults.ExecuteMethodTimeout {
		return fmt.Errorf("the llm connector cannot route to the %s connector linked into this application: its generateText needs a longer Execute timeout than llm's %.0f seconds. %s",
			providerConnectorID, routerDefinition.StepDefaults.ExecuteMethodTimeout.Seconds(), upgrade)
	}
	return nil
}

func hasTextGenerationBranches(definition sdkgo.QueryDefinition) bool {
	expected := llm.TextGenerationBranchDefinitions()
	if len(definition.Branches) != len(expected) {
		return false
	}
	declared := make(map[sdkgo.BranchID]bool, len(definition.Branches))
	for _, branch := range definition.Branches {
		declared[branch.ID] = branch.Optional
	}
	for _, branch := range expected {
		isOptional, isDeclared := declared[branch.ID]
		if !isDeclared || isOptional != branch.Optional {
			return false
		}
	}
	return true
}
