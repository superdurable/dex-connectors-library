// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fixturellm models an independently authored lab connector whose
// generateText Query runs on the shared llm pipeline and the openaichat wire
// format. This file stands in for the code a connector generates from its
// manifest; client.go is the part a connector author writes.
package fixturellm

import (
	"fmt"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ConnectorID is the fixture connector's manifest ID.
const ConnectorID = "fixture-llm"

// GenerateTextRequest is the provider-neutral generateText input.
type GenerateTextRequest = llm.TextGenerationRequest

// GenerateTextResponse is the provider-neutral generateText output.
type GenerateTextResponse = llm.TextGenerationResponse

// GenerateTextResult is the Result every generateText branch target receives.
type GenerateTextResult = sdkgo.QueryResult[GenerateTextResponse]

// GenerateTextDefinition declares the generateText Query with the shared
// branches and the streaming generateText budget: a 900-second attempt, a
// 60-second heartbeat timeout, and four attempts over at most 30 minutes.
var GenerateTextDefinition = sdkgo.QueryDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: ConnectorID, OperationID: llm.TextGenerationOperationID},
	Branches:  llm.TextGenerationBranchDefinitions(),
	StepDefaults: sdkgo.StepDefaults{
		ExecuteMethodTimeout: 900 * time.Second, HeartbeatTimeout: 60 * time.Second,
		ExecuteRetry: &dex.RetryPolicy{
			InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: 60 * time.Second,
			MaximumAttempts: 4, TotalDuration: 30 * time.Minute,
		},
		ExecuteDurability: dex.StepDurabilitySync,
	},
}

// Config is the fixture connection's non-secret configuration.
type Config struct {
	// Model is the connection's model. Empty means every request must name one.
	Model string `json:"model,omitempty"`
	// Endpoint is the provider base URL. It is required.
	Endpoint string `json:"endpoint"`
	// MaxResponseBytes caps one response. Zero uses 8 MiB.
	MaxResponseBytes int64 `json:"maxResponseBytes,omitempty"`
}

// Credentials holds the fixture connection's API key.
type Credentials struct {
	// APIKey is sent only in the Authorization header.
	APIKey sdkgo.SecretString
}

// Connection binds a Client to one logical connection reference.
type Connection struct {
	client    *Client
	reference sdkgo.ConnectionRef
}

// NewConnection returns a Connection for client and reference.
func NewConnection(client *Client, reference sdkgo.ConnectionRef) (Connection, error) {
	if client == nil {
		return Connection{}, fmt.Errorf("fixture-llm connector client is required")
	}
	if err := reference.Validate(); err != nil {
		return Connection{}, err
	}
	return Connection{client: client, reference: reference}, nil
}

func (connection Connection) validate() error {
	if connection.client == nil {
		return fmt.Errorf("fixture-llm connector connection is required")
	}
	return connection.reference.Validate()
}

// GenerateTextStepConfig configures one generateText Step.
type GenerateTextStepConfig[IN any] struct {
	sdkgo.QueryFactoryConfigMarker
	// StepType is the stable Step type.
	StepType string
	// Annotations are the Step's group and explanation.
	Annotations sdkgo.StepAnnotations
	// Connection is the fixture connection the Step uses.
	Connection Connection
	// MapToOperationInput builds the request from the Step input.
	MapToOperationInput func(IN) GenerateTextRequest
	// Generated is the required happy-path target.
	Generated sdkgo.Target[GenerateTextResult]
	// Truncated is the optional output-token-limit target.
	Truncated sdkgo.Target[GenerateTextResult]
	// Blocked is the optional content-policy target.
	Blocked sdkgo.Target[GenerateTextResult]
	// ProviderRejected is the optional provider-rejection target.
	ProviderRejected sdkgo.Target[GenerateTextResult]
	// InvalidResponse is the optional unusable-response target.
	InvalidResponse sdkgo.Target[GenerateTextResult]
	// Defect is the optional local-defect target.
	Defect sdkgo.Target[GenerateTextResult]
	// ResultAttribute stores the Result when non-nil.
	ResultAttribute *dex.Attribute[GenerateTextResult]
	// TextStream receives generated text when non-nil.
	TextStream *dex.Stream[string]
	// TextOptions configure the buffered text Stream.
	TextOptions []dex.BufferedTextStreamOption
	// StepOptionsOverride overrides the definition's Step defaults when non-nil.
	StepOptionsOverride *dex.StepOptions
}

// NewGenerateTextStep returns the generateText Step. It panics on invalid static wiring.
func NewGenerateTextStep[IN any](config GenerateTextStepConfig[IN]) sdkgo.QueryStep[IN, GenerateTextRequest, GenerateTextResponse] {
	if err := config.Connection.validate(); err != nil {
		panic(err)
	}
	return sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[IN, GenerateTextRequest, GenerateTextResponse]{
		StepType: config.StepType, Annotations: config.Annotations,
		Operation: config.Connection.client.GenerateText(), Connection: config.Connection.reference,
		MapToOperationInput: config.MapToOperationInput,
		Branches:            generateTextBranches(config),
		ResultAttribute:     config.ResultAttribute,
		TextStream:          config.TextStream, TextOptions: config.TextOptions,
		StepOptionsOverride: config.StepOptionsOverride,
	})
}

func generateTextBranches[IN any](config GenerateTextStepConfig[IN]) []sdkgo.BranchTarget[GenerateTextResult] {
	targets := []struct {
		branch sdkgo.BranchID
		target sdkgo.Target[GenerateTextResult]
	}{
		{llm.GeneratedBranchID, config.Generated}, {llm.TruncatedBranchID, config.Truncated},
		{llm.BlockedBranchID, config.Blocked}, {llm.ProviderRejectedBranchID, config.ProviderRejected},
		{llm.InvalidResponseBranchID, config.InvalidResponse}, {llm.DefectBranchID, config.Defect},
	}
	branches := make([]sdkgo.BranchTarget[GenerateTextResult], 0, len(targets))
	for _, candidate := range targets {
		if candidate.target.HasStep() {
			branches = append(branches, candidate.target.BranchTarget(candidate.branch))
		}
	}
	return branches
}
