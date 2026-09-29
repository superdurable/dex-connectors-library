// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// RouteGenerateTextQueryForTest returns the provider connector Query that New built for provider.
func RouteGenerateTextQueryForTest(client *Client, provider Provider) *llm.TextGenerationQuery {
	route := client.router.routeFor(provider)
	if route == nil {
		return nil
	}
	return route.generateText
}

// ConnectionGenerateTextQueryForTest returns the routing Query of a Connection, such as one NewLocalConnection built.
func ConnectionGenerateTextQueryForTest(connection Connection) sdkgo.Query[GenerateTextRequest, GenerateTextResponse] {
	return connection.client.GenerateText()
}

// ValidateRouterDefinitionForTest runs New's check of llm's own generateText definition.
var ValidateRouterDefinitionForTest = validateRouterDefinition

// ValidateProviderRouteDefinitionForTest runs New's check of one linked provider connector's definition.
var ValidateProviderRouteDefinitionForTest = validateProviderRouteDefinition
