// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter

import (
	"errors"
	"fmt"
	"strings"

	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// Provider is the prefix of a provider/model selection: the lowercase name
// before the first "/". It selects which provider connector serves the call,
// and it equals the ID of the connection's auth method that adds that
// provider, as listed in Credentials.AuthMethodIDs.
type Provider string

const (
	// ProviderOpenAI routes to the OpenAI connector, which sends the call to
	// api.openai.com with openai_api_key. Receipt.Provider is "openai".
	ProviderOpenAI Provider = "openai"
	// ProviderAnthropic routes to the Claude connector, which sends the call
	// to api.anthropic.com with anthropic_api_key. Receipt.Provider is "claude".
	ProviderAnthropic Provider = "anthropic"
	// ProviderGemini routes to the Gemini connector, which sends the call to
	// generativelanguage.googleapis.com with gemini_api_key. Receipt.Provider is "gemini".
	ProviderGemini Provider = "gemini"
)

var (
	errCredentialsUnavailable = errors.New("connection credentials are unavailable")
	errProviderNotAdded       = errors.New("the connection has not added the provider")
	errProviderAPIKeyMissing  = errors.New("the provider API key is missing")
	errProviderAPIKeyForeign  = errors.New("the provider API key has another provider's format")
)

// providerConnectorIDs maps each provider connector's Receipt.Provider to its selection prefix.
var providerConnectorIDs = map[string]Provider{
	openai.ConnectorID: ProviderOpenAI,
	claude.ConnectorID: ProviderAnthropic,
	gemini.ConnectorID: ProviderGemini,
}

// QualifiedModel returns result's RequestedModel as provider/model, such as
// "anthropic/claude-opus-5-5", which the llm connector accepts as a request
// model. It maps Receipt.Provider "openai" to openai, "claude" to anthropic,
// and "gemini" to gemini. It returns "" when no provider connector served the
// call, such as for a defect the llm connector selected itself, or when the
// result has no RequestedModel.
func QualifiedModel(result llm.TextGenerationResult) string {
	provider, isProviderResult := providerConnectorIDs[result.Receipt.Provider]
	if !isProviderResult || result.Value.RequestedModel == "" {
		return ""
	}
	return string(provider) + "/" + result.Value.RequestedModel
}

func (provider Provider) isRouted() bool {
	switch provider {
	case ProviderOpenAI, ProviderAnthropic, ProviderGemini:
		return true
	default:
		return false
	}
}

// providerRoute is one provider connector's generateText Query and the key it may use.
type providerRoute struct {
	provider Provider
	// connectorID is the provider connector's ID, which its Receipts and Failures carry.
	connectorID string
	// defaultModel is the model the provider connector uses when the request names none.
	defaultModel string
	apiKey       providerAPIKey
	generateText *llm.TextGenerationQuery
}

// providerAPIKey selects one provider's key from the llm connection.
type providerAPIKey struct {
	// authMethodID is the manifest auth method that adds this provider to a connection.
	authMethodID string
	// authMethodDisplayName is that method's manifest displayName, used only in messages.
	authMethodDisplayName string
	// credentialField is the manifest field name, used only in messages.
	credentialField string
	selectAPIKey    func(Credentials) sdkgo.SecretString
	// isForeignFormat reports a key that clearly belongs to another routed provider.
	isForeignFormat func(apiKey string) bool
}

// providerCredentialProvider resolves one provider's credentials from the llm
// connection, checking exactly the key the pipeline sends.
type providerCredentialProvider[C any] struct {
	credentials            sdkgo.CredentialProvider[Credentials]
	apiKey                 providerAPIKey
	newProviderCredentials func(apiKey sdkgo.SecretString) C
}

// newProviderRoutes builds the routes in selection-table order: openai, anthropic, gemini.
func newProviderRoutes(config *Config, credentials sdkgo.CredentialProvider[Credentials], options *clientOptions) ([]providerRoute, error) {
	routes := make([]providerRoute, 0, 3)
	for _, newRoute := range []func(*Config, sdkgo.CredentialProvider[Credentials], *clientOptions) (providerRoute, error){
		newOpenAIRoute, newAnthropicRoute, newGeminiRoute,
	} {
		route, err := newRoute(config, credentials, options)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func newOpenAIRoute(config *Config, credentials sdkgo.CredentialProvider[Credentials], options *clientOptions) (providerRoute, error) {
	apiKey := providerAPIKey{
		authMethodID: string(ProviderOpenAI), authMethodDisplayName: "OpenAI", credentialField: "openai_api_key",
		selectAPIKey:    func(credentials Credentials) sdkgo.SecretString { return credentials.OpenAIAPIKey },
		isForeignFormat: func(apiKey string) bool { return isAnthropicAPIKeyFormat(apiKey) || isGoogleAPIKeyFormat(apiKey) },
	}
	var openAIOptions []openai.Option
	if options.httpClient != nil {
		openAIOptions = append(openAIOptions, openai.WithHTTPClient(options.httpClient))
	}
	// openai.Config.Endpoint accepts any HTTPS host, so only validateClientOptions keeps a test URL on loopback.
	client, err := openai.New(openai.Config{
		Endpoint: options.providerBaseURLsForTest[ProviderOpenAI], MaxResponseBytes: config.MaxResponseBytes,
	}, providerCredentialProvider[openai.Credentials]{
		credentials: credentials, apiKey: apiKey,
		newProviderCredentials: func(apiKey sdkgo.SecretString) openai.Credentials { return openai.Credentials{APIKey: apiKey} },
	}, openAIOptions...)
	if err != nil {
		return providerRoute{}, fmt.Errorf("llm: build the openai route: %w", err)
	}
	return providerRoute{
		provider: ProviderOpenAI, connectorID: openai.ConnectorID, defaultModel: openai.DefaultConfig().Model,
		apiKey: apiKey, generateText: client.GenerateText(),
	}, nil
}

func newAnthropicRoute(config *Config, credentials sdkgo.CredentialProvider[Credentials], options *clientOptions) (providerRoute, error) {
	apiKey := providerAPIKey{
		authMethodID: string(ProviderAnthropic), authMethodDisplayName: "Claude", credentialField: "anthropic_api_key",
		selectAPIKey:    func(credentials Credentials) sdkgo.SecretString { return credentials.AnthropicAPIKey },
		isForeignFormat: func(apiKey string) bool { return isOpenAIAPIKeyFormat(apiKey) || isGoogleAPIKeyFormat(apiKey) },
	}
	var claudeOptions []claude.Option
	if options.httpClient != nil {
		claudeOptions = append(claudeOptions, claude.WithHTTPClient(options.httpClient))
	}
	if baseURL, hasBaseURL := options.providerBaseURLsForTest[ProviderAnthropic]; hasBaseURL {
		claudeOptions = append(claudeOptions, claude.WithBaseURLForTest(baseURL))
	}
	client, err := claude.New(claude.Config{
		WorkspaceID: config.AnthropicWorkspaceID, MaxResponseBytes: config.MaxResponseBytes,
	}, providerCredentialProvider[claude.Credentials]{
		credentials: credentials, apiKey: apiKey,
		newProviderCredentials: func(apiKey sdkgo.SecretString) claude.Credentials { return claude.Credentials{APIKey: apiKey} },
	}, claudeOptions...)
	if err != nil {
		// Claude names anthropicWorkspaceId by its own field name, workspaceId.
		return providerRoute{}, fmt.Errorf("llm: build the anthropic route: %w", err)
	}
	return providerRoute{
		provider: ProviderAnthropic, connectorID: claude.ConnectorID, defaultModel: claude.DefaultConfig().Model,
		apiKey: apiKey, generateText: client.GenerateText(),
	}, nil
}

func newGeminiRoute(config *Config, credentials sdkgo.CredentialProvider[Credentials], options *clientOptions) (providerRoute, error) {
	apiKey := providerAPIKey{
		authMethodID: string(ProviderGemini), authMethodDisplayName: "Gemini", credentialField: "gemini_api_key",
		selectAPIKey:    func(credentials Credentials) sdkgo.SecretString { return credentials.GeminiAPIKey },
		isForeignFormat: func(apiKey string) bool { return isOpenAIAPIKeyFormat(apiKey) || isAnthropicAPIKeyFormat(apiKey) },
	}
	var geminiOptions []gemini.Option
	if options.httpClient != nil {
		geminiOptions = append(geminiOptions, gemini.WithHTTPClient(options.httpClient))
	}
	// gemini.Config.Endpoint accepts any HTTPS host, so only validateClientOptions keeps a test URL on loopback.
	client, err := gemini.New(gemini.Config{
		Endpoint: options.providerBaseURLsForTest[ProviderGemini], MaxResponseBytes: config.MaxResponseBytes,
	}, providerCredentialProvider[gemini.Credentials]{
		credentials: credentials, apiKey: apiKey,
		newProviderCredentials: func(apiKey sdkgo.SecretString) gemini.Credentials { return gemini.Credentials{APIKey: apiKey} },
	}, geminiOptions...)
	if err != nil {
		return providerRoute{}, fmt.Errorf("llm: build the gemini route: %w", err)
	}
	return providerRoute{
		provider: ProviderGemini, connectorID: gemini.ConnectorID, defaultModel: gemini.DefaultConfig().Model,
		apiKey: apiKey, generateText: client.GenerateText(),
	}, nil
}

// Resolve returns this provider's credentials, or an error for a provider the
// connection has not added or a blank or foreign-format key.
func (provider providerCredentialProvider[C]) Resolve(call sdkgo.Call) (C, error) {
	var zero C
	resolved, err := provider.credentials.Resolve(call)
	if err != nil {
		return zero, errCredentialsUnavailable
	}
	apiKey, err := provider.apiKey.selectUsableAPIKey(resolved)
	if err != nil {
		return zero, err
	}
	return provider.newProviderCredentials(apiKey), nil
}

// selectUsableAPIKey returns this provider's key, rejecting a provider the
// connection has not added, a blank key, and one in another provider's format.
func (apiKey providerAPIKey) selectUsableAPIKey(credentials Credentials) (sdkgo.SecretString, error) {
	if !credentials.HasAuthMethod(apiKey.authMethodID) {
		return sdkgo.SecretString{}, errProviderNotAdded
	}
	selected := apiKey.selectAPIKey(credentials)
	value := strings.TrimSpace(selected.Reveal())
	if value == "" {
		return sdkgo.SecretString{}, errProviderAPIKeyMissing
	}
	if apiKey.isForeignFormat(value) {
		return sdkgo.SecretString{}, errProviderAPIKeyForeign
	}
	return selected, nil
}

// isOpenAIAPIKeyFormat treats every sk- key except Claude's sk-ant- as OpenAI's, so new OpenAI prefixes stay caught.
func isOpenAIAPIKeyFormat(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-") && !isAnthropicAPIKeyFormat(apiKey)
}

func isAnthropicAPIKeyFormat(apiKey string) bool { return strings.HasPrefix(apiKey, "sk-ant-") }

// isGoogleAPIKeyFormat recognizes Google API keys, which Gemini API keys are.
func isGoogleAPIKeyFormat(apiKey string) bool { return strings.HasPrefix(apiKey, "AIza") }
