// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

const (
	// streamingRequestTimeout is the exchange bound every lab connector except DeepSeek used under a 900-second budget.
	streamingRequestTimeout = 870 * time.Second
	// queuedStreamingRequestTimeout stays 30 seconds below the 1200-second Execute
	// timeout, so a stalled exchange returns Retry first.
	queuedStreamingRequestTimeout = 1170 * time.Second
	defaultMaxResponseBytes       = 8 << 20
	// deepSeekMaxResponseBytes also counts the keep-alive comments and reasoning chunks of a DeepSeek stream.
	deepSeekMaxResponseBytes = 64 << 20
)

// providerAPI is how the llm connector reaches one provider's text generation API.
type providerAPI struct {
	// defaultModel is the model a connection without a model uses.
	defaultModel string
	// regionBaseURLs maps each region the provider serves to its API base URL; every provider serves RegionGlobal.
	regionBaseURLs map[Region]string
	// requestTimeout bounds one HTTP exchange when the caller's client sets no timeout.
	requestTimeout time.Duration
	// defaultMaxResponseBytes applies when Config.MaxResponseBytes is zero.
	defaultMaxResponseBytes int64
	// newWireFormat returns the provider's wire format for one validated connection.
	newWireFormat func(config *Config) (textgen.WireFormat, error)
}

// providerAPIs holds every provider the provider enum declares; manifest tests keep both lists equal.
var providerAPIs = map[Provider]providerAPI{
	ProviderOpenai: {
		defaultModel: "gpt-6-sol", regionBaseURLs: map[Region]string{RegionGlobal: openAIAPIBaseURL},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newOpenAIResponsesWireFormat,
	},
	ProviderAnthropic: {
		defaultModel: "claude-sonnet-5", regionBaseURLs: map[Region]string{RegionGlobal: claudeAPIBaseURL},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newClaudeMessagesWireFormat,
	},
	ProviderGemini: {
		defaultModel: "gemini-3.5-flash-lite", regionBaseURLs: map[Region]string{RegionGlobal: geminiAPIBaseURL},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newGeminiGenerateContentWireFormat,
	},
	ProviderQwen: {
		defaultModel: "qwen3.7-plus",
		regionBaseURLs: map[Region]string{
			RegionGlobal: qwenSingaporeEndpoint, RegionHongKong: qwenHongKongEndpoint, RegionChina: qwenBeijingEndpoint,
		},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newQwenWireFormat,
	},
	ProviderDeepseek: {
		defaultModel: "deepseek-flash", regionBaseURLs: map[Region]string{RegionGlobal: deepSeekAPIBaseURL},
		requestTimeout: queuedStreamingRequestTimeout, defaultMaxResponseBytes: deepSeekMaxResponseBytes,
		newWireFormat: newDeepSeekWireFormat,
	},
	ProviderMeta: {
		defaultModel: "muse-spark-1.3", regionBaseURLs: map[Region]string{RegionGlobal: metaAPIBaseURL},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newMetaWireFormat,
	},
	ProviderMistral: {
		defaultModel: "mistral-large-2512",
		regionBaseURLs: map[Region]string{
			RegionGlobal: mistralGlobalEndpoint, RegionEu: mistralEUEndpoint, RegionUs: mistralUSEndpoint,
		},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newMistralWireFormat,
	},
	ProviderKimi: {
		defaultModel:   "kimi-k2.6",
		regionBaseURLs: map[Region]string{RegionGlobal: kimiGlobalEndpoint, RegionChina: kimiChinaEndpoint},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newKimiWireFormat,
	},
	ProviderXai: {
		defaultModel:   "grok-4.3",
		regionBaseURLs: map[Region]string{RegionGlobal: xaiGlobalEndpoint, RegionUs: xaiUSRegionalEndpoint},
		requestTimeout: streamingRequestTimeout, defaultMaxResponseBytes: defaultMaxResponseBytes,
		newWireFormat: newXAIWireFormat,
	},
}

// baseURLForRegion returns the provider's API base URL in region, or an error naming the regions it serves.
func (api providerAPI) baseURLForRegion(provider Provider, region Region) (string, error) {
	if baseURL, isServed := api.regionBaseURLs[region]; isServed {
		return baseURL, nil
	}
	servedRegions := make([]string, 0, len(api.regionBaseURLs))
	for servedRegion := range api.regionBaseURLs {
		servedRegions = append(servedRegions, string(servedRegion))
	}
	slices.Sort(servedRegions)
	return "", fmt.Errorf("provider %s does not serve region %s; use %s", provider, region, strings.Join(servedRegions, " or "))
}
