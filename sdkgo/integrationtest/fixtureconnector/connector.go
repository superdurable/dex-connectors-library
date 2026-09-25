//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fixtureconnector models an independently authored connector.
package fixtureconnector

import (
	"fmt"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	lookupWidgetFound  sdkgo.BranchID = "found"
	lookupWidgetAbsent sdkgo.BranchID = "absent"
	lookupWidgetFailed sdkgo.BranchID = "failed"
	lookupWidgetDefect sdkgo.BranchID = "defect"
)

const (
	createWidgetCompleted sdkgo.BranchID = "completed"
	createWidgetRejected  sdkgo.BranchID = "rejected"
	createWidgetUncertain sdkgo.BranchID = "uncertain"
	createWidgetDefect    sdkgo.BranchID = "defect"
)

type Widget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type LookupInput struct {
	Name string
}

type CreateInput struct {
	Name string
}

type Credentials struct {
	Token sdkgo.SecretString
}

type ProviderStats struct {
	QueryCount      int
	WriteCount      int
	MutationCalls   []sdkgo.CallID
	IdempotencyKeys []sdkgo.IdempotencyKey
}

type Provider struct {
	mu              sync.Mutex
	widgets         map[string]Widget
	mutationCalls   []sdkgo.CallID
	idempotencyKeys []sdkgo.IdempotencyKey
	queryCount      int
	writeCount      int
}

func NewProvider() *Provider {
	return &Provider{widgets: make(map[string]Widget)}
}

func (provider *Provider) Stats() ProviderStats {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return ProviderStats{
		QueryCount: provider.queryCount, WriteCount: provider.writeCount,
		MutationCalls:   append([]sdkgo.CallID(nil), provider.mutationCalls...),
		IdempotencyKeys: append([]sdkgo.IdempotencyKey(nil), provider.idempotencyKeys...),
	}
}

type Connection struct {
	provider    *Provider
	reference   sdkgo.ConnectionRef
	credentials sdkgo.CredentialProvider[Credentials]
}

func NewConnection(provider *Provider, name string, token sdkgo.SecretString) (Connection, error) {
	if provider == nil {
		return Connection{}, fmt.Errorf("fixture provider is required")
	}
	reference := sdkgo.ConnectionRef{Provider: "fixture", Name: name}
	if err := reference.Validate(); err != nil {
		return Connection{}, err
	}
	return Connection{
		provider: provider, reference: reference,
		credentials: sdkgo.StaticCredentialProvider[Credentials]{reference: {Token: token}},
	}, nil
}

func (connection Connection) validate() error {
	if connection.provider == nil || connection.credentials == nil {
		return fmt.Errorf("fixture connector connection is required")
	}
	return connection.reference.Validate()
}

type lookupWidgetOperation struct {
	connection Connection
}

func (lookupWidgetOperation) Definition() sdkgo.QueryDefinition {
	return sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "fixture", OperationID: "lookupWidget"},
		Branches: []sdkgo.BranchDefinition{
			{ID: lookupWidgetFound, Description: "The widget exists."},
			{ID: lookupWidgetAbsent, Description: "The widget does not exist."},
			{ID: lookupWidgetFailed, Description: "The lookup was rejected."},
			{ID: lookupWidgetDefect, Description: "The connector definition is invalid."},
		},
		DefectBranch:    lookupWidgetDefect,
		ResultAttribute: sdkgo.RequirementNone,
		StepDefaults: sdkgo.StepDefaults{
			ExecuteMethodTimeout: 10 * time.Second,
			ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: 10 * time.Millisecond},
			ExecuteDurability:    dex.StepDurabilitySync,
		},
	}
}

func (operation lookupWidgetOperation) Invoke(call sdkgo.Call, input LookupInput) sdkgo.QueryAttempt[Widget] {
	credential, err := operation.connection.credentials.Resolve(call)
	if err != nil || credential.Token.Reveal() == "" {
		failure := sdkgo.Failure{Kind: sdkgo.FailureAuthentication, Provider: "fixture", Operation: "lookupWidget", Message: "credentials unavailable"}
		return sdkgo.NewQueryBranch(lookupWidgetFailed, Widget{}, &failure, sdkgo.Receipt{})
	}
	operation.connection.provider.mu.Lock()
	defer operation.connection.provider.mu.Unlock()
	operation.connection.provider.queryCount++
	value, found := operation.connection.provider.widgets[input.Name]
	if !found {
		return sdkgo.NewQueryBranch(lookupWidgetAbsent, Widget{}, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(lookupWidgetFound, value, nil, sdkgo.Receipt{})
}

type createWidgetOperation struct {
	connection Connection
}

func (createWidgetOperation) Definition() sdkgo.MutationDefinition {
	return sdkgo.MutationDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "fixture", OperationID: "createWidget"},
		Branches: []sdkgo.BranchDefinition{
			{ID: createWidgetCompleted, Description: "The widget was created."},
			{ID: createWidgetRejected, Description: "The provider rejected the widget."},
			{ID: createWidgetUncertain, Description: "The provider outcome is unknown."},
			{ID: createWidgetDefect, Description: "The connector definition is invalid."},
		},
		DefectBranch:    createWidgetDefect,
		UncertainBranch: createWidgetUncertain,
		ResultAttribute: sdkgo.RequirementRequired,
		Progress:        sdkgo.ProgressCapabilities{Structured: true},
		StepDefaults: sdkgo.StepDefaults{
			ExecuteMethodTimeout: 10 * time.Second,
			ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: 10 * time.Millisecond},
			ExecuteDurability:    dex.StepDurabilitySync,
		},
	}
}

func (createWidgetOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func (operation createWidgetOperation) Invoke(call sdkgo.Call, input CreateInput) sdkgo.MutationAttempt[Widget] {
	credential, err := operation.connection.credentials.Resolve(call)
	if err != nil || credential.Token.Reveal() == "" {
		failure := sdkgo.Failure{Kind: sdkgo.FailureAuthentication, Provider: "fixture", Operation: "createWidget", Message: "credentials unavailable"}
		return sdkgo.NewMutationBranch(createWidgetRejected, Widget{}, &failure, sdkgo.Receipt{})
	}
	if err := call.ReportProgress(sdkgo.Progress{Phase: "dispatching"}); err != nil {
		return sdkgo.NewMutationRetry[Widget](sdkgo.Failure{
			Kind: sdkgo.FailureAvailability, Provider: "fixture", Operation: "createWidget", Message: "progress unavailable",
		}, 10*time.Millisecond)
	}
	provider := operation.connection.provider
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.mutationCalls = append(provider.mutationCalls, call.ID)
	provider.idempotencyKeys = append(provider.idempotencyKeys, call.IdempotencyKey)
	if len(provider.mutationCalls) == 1 {
		return sdkgo.NewMutationRetry[Widget](sdkgo.Failure{
			Kind: sdkgo.FailureRateLimit, Provider: "fixture", Operation: "createWidget", Message: "retry fixture",
		}, 10*time.Millisecond)
	}
	if existing, found := provider.widgets[input.Name]; found {
		return sdkgo.NewMutationBranch(createWidgetCompleted, existing, nil, sdkgo.Receipt{})
	}
	created := Widget{ID: "widget-1", Name: input.Name}
	provider.widgets[input.Name] = created
	provider.writeCount++
	return sdkgo.NewMutationBranch(createWidgetCompleted, created, nil, sdkgo.Receipt{ProviderObjectID: created.ID})
}

type LookupWidgetResult = sdkgo.QueryResult[Widget]

type LookupWidgetStepConfig[IN any] struct {
	sdkgo.QueryFactoryConfigMarker
	StepType            string
	Annotations         sdkgo.StepAnnotations
	Connection          Connection
	BuildOperationInput func(IN) (LookupInput, error)
	Found               sdkgo.Target[LookupWidgetResult]
	Absent              sdkgo.Target[LookupWidgetResult]
	Failed              sdkgo.Target[LookupWidgetResult]
	Defect              sdkgo.Target[LookupWidgetResult]
}

func NewLookupWidgetStep[IN any](config LookupWidgetStepConfig[IN]) sdkgo.QueryStep[IN, LookupInput, Widget] {
	if err := config.Connection.validate(); err != nil {
		panic(err)
	}
	return sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[IN, LookupInput, Widget]{
		StepType: config.StepType, Annotations: config.Annotations,
		Operation: lookupWidgetOperation{connection: config.Connection}, Connection: config.Connection.reference,
		BuildOperationInput: config.BuildOperationInput,
		Branches: []sdkgo.BranchTarget[LookupWidgetResult]{
			config.Found.BranchTarget(lookupWidgetFound),
			config.Absent.BranchTarget(lookupWidgetAbsent),
			config.Failed.BranchTarget(lookupWidgetFailed),
			config.Defect.BranchTarget(lookupWidgetDefect),
		},
	})
}

type CreateWidgetResult = sdkgo.MutationResult[Widget]

type CreateWidgetStepConfig[IN any] struct {
	sdkgo.MutationFactoryConfigMarker
	StepType            string
	Annotations         sdkgo.StepAnnotations
	Connection          Connection
	BuildOperationInput func(IN) (CreateInput, error)
	Completed           sdkgo.Target[CreateWidgetResult]
	Rejected            sdkgo.Target[CreateWidgetResult]
	Uncertain           sdkgo.Target[CreateWidgetResult]
	Defect              sdkgo.Target[CreateWidgetResult]
	ResultAttribute     *dex.Attribute[sdkgo.MutationResult[Widget]]
	ProgressStream      *dex.Stream[sdkgo.ProgressUpdate]
}

func NewCreateWidgetStep[IN any](config CreateWidgetStepConfig[IN]) sdkgo.MutationStep[IN, CreateInput, Widget] {
	if err := config.Connection.validate(); err != nil {
		panic(err)
	}
	return sdkgo.MustNewMutationStep(sdkgo.MutationStepConfig[IN, CreateInput, Widget]{
		StepType: config.StepType, Annotations: config.Annotations,
		Operation: createWidgetOperation{connection: config.Connection}, Connection: config.Connection.reference,
		BuildOperationInput: config.BuildOperationInput,
		Branches: []sdkgo.BranchTarget[CreateWidgetResult]{
			config.Completed.BranchTarget(createWidgetCompleted),
			config.Rejected.BranchTarget(createWidgetRejected),
			config.Uncertain.BranchTarget(createWidgetUncertain),
			config.Defect.BranchTarget(createWidgetDefect),
		},
		ResultAttribute: config.ResultAttribute,
		ProgressStream:  config.ProgressStream,
	})
}
