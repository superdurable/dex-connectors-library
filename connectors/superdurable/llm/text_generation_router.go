// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

const (
	modelSelectionFormatMessage = "the model must be provider/model, where provider is openai, anthropic, or gemini, such as anthropic/claude-sonnet-5"
	noAddedProviderMessage      = "the connection has added no provider; add OpenAI, Claude, or Gemini to the connection"
	unknownFirstProviderMessage = "the connection's first provider is not openai, anthropic, or gemini"
)

// textGenerationRouter is the llm generateText Query; it returns one route's
// attempt unchanged, never nesting sdkgo.RunQuery or falling back.
type textGenerationRouter struct {
	definition sdkgo.QueryDefinition
	// connectionModel is the trimmed connection model, or "" for the first added provider's default.
	connectionModel string
	credentials     sdkgo.CredentialProvider[Credentials]
	routes          []providerRoute
}

// routeSelection is one attempt's route and the model it sends.
type routeSelection struct {
	route *providerRoute
	// providerModel is the canonical model ID without the prefix, or "" for the provider connector's default.
	providerModel string
	// requestedModel is the model the provider serves, reported on a router defect.
	requestedModel string
}

var _ sdkgo.Query[GenerateTextRequest, GenerateTextResponse] = (*textGenerationRouter)(nil)

func newTextGenerationRouter(
	definition sdkgo.QueryDefinition, connectionModel string, credentials sdkgo.CredentialProvider[Credentials], routes []providerRoute,
) (*textGenerationRouter, error) {
	router := &textGenerationRouter{
		definition: definition, connectionModel: strings.TrimSpace(connectionModel), credentials: credentials, routes: routes,
	}
	if router.connectionModel == "" {
		return router, nil
	}
	if _, err := router.selectModel(router.connectionModel); err != nil {
		return nil, fmt.Errorf("llm: configuration model: %w", err)
	}
	return router, nil
}

// Definition returns the llm connector's generateText definition.
func (router *textGenerationRouter) Definition() sdkgo.QueryDefinition { return router.definition }

// Invoke selects defect without a request for a bad selection, a provider the
// connection has not added, or a bad key, else runs the provider Query once.
func (router *textGenerationRouter) Invoke(call sdkgo.Call, request GenerateTextRequest) sdkgo.QueryAttempt[GenerateTextResponse] {
	selection, failure := router.selectRoute(call, request.Model)
	if failure != nil {
		return sdkgo.NewQueryBranch(llm.DefectBranchID, GenerateTextResponse{RequestedModel: selection.requestedModel},
			failure, sdkgo.Receipt{Provider: ConnectorID})
	}
	request.Model = selection.providerModel
	return selection.route.generateText.Invoke(call, request)
}

// selectRoute checks the model grammar before reading credentials, so a
// grammar defect needs no connection; a blank model reads the first added provider.
func (router *textGenerationRouter) selectRoute(call sdkgo.Call, requestModel string) (routeSelection, *sdkgo.Failure) {
	model := llm.ResolveModel(requestModel, router.connectionModel)
	var selection routeSelection
	if model != "" {
		var err error
		if selection, err = router.selectModel(model); err != nil {
			return routeSelection{}, router.failure(sdkgo.FailureValidation, err.Error())
		}
	}
	credentials, err := router.credentials.Resolve(call)
	if err != nil {
		return selection, router.failure(sdkgo.FailureAuthentication, errCredentialsUnavailable.Error())
	}
	if model == "" {
		var message string
		if selection, message = router.selectFirstAddedProvider(credentials); message != "" {
			return routeSelection{}, router.failure(sdkgo.FailureAuthentication, message)
		}
	}
	if _, err := selection.route.apiKey.selectUsableAPIKey(credentials); err != nil {
		return selection, router.failure(sdkgo.FailureAuthentication, apiKeyProblemMessage(err, selection.route))
	}
	return selection, nil
}

// selectModel parses provider[/model] and applies that provider's model-ID
// rule; errors never repeat the value.
func (router *textGenerationRouter) selectModel(value string) (routeSelection, error) {
	providerName, providerModel, hasProviderModel := strings.Cut(value, "/")
	route := router.routeFor(Provider(providerName))
	if route == nil || (hasProviderModel && strings.TrimSpace(providerModel) == "") {
		return routeSelection{}, errors.New(modelSelectionFormatMessage)
	}
	if !hasProviderModel {
		return routeSelection{route: route, requestedModel: route.defaultModel}, nil
	}
	canonicalModel, err := route.generateText.ModelIDRule().ValidateModelID(providerModel)
	if err != nil {
		return routeSelection{}, fmt.Errorf("the %s %w", route.provider, err)
	}
	return routeSelection{route: route, providerModel: canonicalModel, requestedModel: canonicalModel}, nil
}

// selectFirstAddedProvider selects the default model of the connection's
// first auth method, or returns a failure message that repeats no ID.
func (router *textGenerationRouter) selectFirstAddedProvider(credentials Credentials) (routeSelection, string) {
	if len(credentials.AuthMethodIDs) == 0 {
		return routeSelection{}, noAddedProviderMessage
	}
	route := router.routeFor(Provider(credentials.AuthMethodIDs[0]))
	if route == nil {
		return routeSelection{}, unknownFirstProviderMessage
	}
	return routeSelection{route: route, requestedModel: route.defaultModel}, ""
}

func (router *textGenerationRouter) routeFor(provider Provider) *providerRoute {
	for index := range router.routes {
		if router.routes[index].provider == provider {
			return &router.routes[index]
		}
	}
	return nil
}

func (router *textGenerationRouter) failure(kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: router.definition.Operation.OperationID, Message: message}
}

func apiKeyProblemMessage(err error, route *providerRoute) string {
	apiKey := route.apiKey
	switch {
	case errors.Is(err, errProviderNotAdded):
		return fmt.Sprintf("the model selects %s, but the connection has not added the %s provider; add %s to the connection",
			route.provider, apiKey.authMethodDisplayName, apiKey.authMethodDisplayName)
	case errors.Is(err, errProviderAPIKeyMissing):
		return fmt.Sprintf("the model selects %s, but the connection has no %s", route.provider, apiKey.credentialField)
	case errors.Is(err, errProviderAPIKeyForeign):
		return fmt.Sprintf("%s holds a key in another provider's format", apiKey.credentialField)
	default:
		return errCredentialsUnavailable.Error()
	}
}
