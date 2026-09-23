//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fixtureconnector models an independently authored connector.
package fixtureconnector

import (
	"fmt"
	"sync"
	"time"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	lookupWidgetFound  connector.BranchID = "found"
	lookupWidgetAbsent connector.BranchID = "absent"
	lookupWidgetFailed connector.BranchID = "failed"
	lookupWidgetDefect connector.BranchID = "defect"
)

const (
	createWidgetCompleted connector.BranchID = "completed"
	createWidgetRejected  connector.BranchID = "rejected"
	createWidgetUncertain connector.BranchID = "uncertain"
	createWidgetDefect    connector.BranchID = "defect"
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
	Token connector.SecretString
}

type ProviderStats struct {
	QueryCount      int
	WriteCount      int
	MutationCalls   []connector.CallID
	IdempotencyKeys []connector.IdempotencyKey
}

type Provider struct {
	mu              sync.Mutex
	widgets         map[string]Widget
	mutationCalls   []connector.CallID
	idempotencyKeys []connector.IdempotencyKey
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
		MutationCalls:   append([]connector.CallID(nil), provider.mutationCalls...),
		IdempotencyKeys: append([]connector.IdempotencyKey(nil), provider.idempotencyKeys...),
	}
}

type Connection struct {
	provider    *Provider
	reference   connector.ConnectionRef
	credentials connector.CredentialProvider[Credentials]
}

func NewConnection(provider *Provider, name string, token connector.SecretString) (Connection, error) {
	if provider == nil {
		return Connection{}, fmt.Errorf("fixture provider is required")
	}
	reference := connector.ConnectionRef{Provider: "fixture", Name: name}
	if err := reference.Validate(); err != nil {
		return Connection{}, err
	}
	return Connection{
		provider: provider, reference: reference,
		credentials: connector.StaticCredentialProvider[Credentials]{reference: {Token: token}},
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

func (lookupWidgetOperation) Definition() connector.QueryDefinition {
	return connector.QueryDefinition{
		Operation: connector.OperationRef{ConnectorID: "fixture", OperationID: "lookupWidget"},
		Branches: []connector.BranchDefinition{
			{ID: lookupWidgetFound, Description: "The widget exists."},
			{ID: lookupWidgetAbsent, Description: "The widget does not exist."},
			{ID: lookupWidgetFailed, Description: "The lookup was rejected."},
			{ID: lookupWidgetDefect, Description: "The connector definition is invalid."},
		},
		DefectBranch:    lookupWidgetDefect,
		ResultAttribute: connector.RequirementNone,
		StepDefaults: connector.StepDefaults{
			ExecuteMethodTimeout: 10 * time.Second,
			ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: 10 * time.Millisecond},
			ExecuteDurability:    dex.StepDurabilitySync,
		},
	}
}

func (operation lookupWidgetOperation) Invoke(call connector.Call, input LookupInput) connector.QueryAttempt[Widget] {
	credential, err := operation.connection.credentials.Resolve(call)
	if err != nil || credential.Token.Reveal() == "" {
		failure := connector.Failure{Kind: connector.FailureAuthentication, Provider: "fixture", Operation: "lookupWidget", Message: "credentials unavailable"}
		return connector.NewQueryBranch(lookupWidgetFailed, Widget{}, &failure, connector.Receipt{})
	}
	operation.connection.provider.mu.Lock()
	defer operation.connection.provider.mu.Unlock()
	operation.connection.provider.queryCount++
	value, found := operation.connection.provider.widgets[input.Name]
	if !found {
		return connector.NewQueryBranch(lookupWidgetAbsent, Widget{}, nil, connector.Receipt{})
	}
	return connector.NewQueryBranch(lookupWidgetFound, value, nil, connector.Receipt{})
}

type createWidgetOperation struct {
	connection Connection
}

func (createWidgetOperation) Definition() connector.MutationDefinition {
	return connector.MutationDefinition{
		Operation: connector.OperationRef{ConnectorID: "fixture", OperationID: "createWidget"},
		Branches: []connector.BranchDefinition{
			{ID: createWidgetCompleted, Description: "The widget was created."},
			{ID: createWidgetRejected, Description: "The provider rejected the widget."},
			{ID: createWidgetUncertain, Description: "The provider outcome is unknown."},
			{ID: createWidgetDefect, Description: "The connector definition is invalid."},
		},
		DefectBranch:    createWidgetDefect,
		UncertainBranch: createWidgetUncertain,
		ResultAttribute: connector.RequirementRequired,
		Progress:        connector.ProgressCapabilities{Structured: true},
		StepDefaults: connector.StepDefaults{
			ExecuteMethodTimeout: 10 * time.Second,
			ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: 10 * time.Millisecond},
			ExecuteDurability:    dex.StepDurabilitySync,
		},
	}
}

func (createWidgetOperation) IdempotencyKey(callID connector.CallID, _ CreateInput) connector.IdempotencyKey {
	return connector.IdempotencyKey(callID)
}

func (operation createWidgetOperation) Invoke(call connector.Call, input CreateInput) connector.MutationAttempt[Widget] {
	credential, err := operation.connection.credentials.Resolve(call)
	if err != nil || credential.Token.Reveal() == "" {
		failure := connector.Failure{Kind: connector.FailureAuthentication, Provider: "fixture", Operation: "createWidget", Message: "credentials unavailable"}
		return connector.NewMutationBranch(createWidgetRejected, Widget{}, &failure, connector.Receipt{})
	}
	if err := call.ReportProgress(connector.Progress{Phase: "dispatching"}); err != nil {
		return connector.NewMutationRetry[Widget](connector.Failure{
			Kind: connector.FailureAvailability, Provider: "fixture", Operation: "createWidget", Message: "progress unavailable",
		}, 10*time.Millisecond)
	}
	provider := operation.connection.provider
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.mutationCalls = append(provider.mutationCalls, call.ID)
	provider.idempotencyKeys = append(provider.idempotencyKeys, call.IdempotencyKey)
	if len(provider.mutationCalls) == 1 {
		return connector.NewMutationRetry[Widget](connector.Failure{
			Kind: connector.FailureRateLimit, Provider: "fixture", Operation: "createWidget", Message: "retry fixture",
		}, 10*time.Millisecond)
	}
	if existing, found := provider.widgets[input.Name]; found {
		return connector.NewMutationBranch(createWidgetCompleted, existing, nil, connector.Receipt{})
	}
	created := Widget{ID: "widget-1", Name: input.Name}
	provider.widgets[input.Name] = created
	provider.writeCount++
	return connector.NewMutationBranch(createWidgetCompleted, created, nil, connector.Receipt{ProviderObjectID: created.ID})
}

type LookupWidgetStepOutput[IN any] = connector.QueryStepOutput[IN, Widget]

type LookupWidgetStepConfig[IN any] struct {
	connector.QueryFactoryConfigMarker
	StepType     string
	Presentation connector.StepPresentation
	Connection   Connection
	BuildInput   func(IN) (LookupInput, error)
	Found        connector.Target[LookupWidgetStepOutput[IN]]
	Absent       connector.Target[LookupWidgetStepOutput[IN]]
	Failed       connector.Target[LookupWidgetStepOutput[IN]]
	Defect       connector.Target[LookupWidgetStepOutput[IN]]
}

func NewLookupWidgetStep[IN any](config LookupWidgetStepConfig[IN]) connector.QueryStep[IN, LookupInput, Widget] {
	if err := config.Connection.validate(); err != nil {
		panic(err)
	}
	return connector.MustNewQueryStep(connector.QueryStepConfig[IN, LookupInput, Widget]{
		StepType: config.StepType, Presentation: config.Presentation,
		Operation: lookupWidgetOperation{connection: config.Connection}, Connection: config.Connection.reference,
		BuildInput: config.BuildInput,
		Branches: []connector.BranchTarget[LookupWidgetStepOutput[IN]]{
			config.Found.BranchTarget(lookupWidgetFound),
			config.Absent.BranchTarget(lookupWidgetAbsent),
			config.Failed.BranchTarget(lookupWidgetFailed),
			config.Defect.BranchTarget(lookupWidgetDefect),
		},
	})
}

type CreateWidgetStepOutput[IN any] = connector.MutationStepOutput[IN, Widget]

type CreateWidgetStepConfig[IN any] struct {
	connector.MutationFactoryConfigMarker
	StepType        string
	Presentation    connector.StepPresentation
	Connection      Connection
	BuildInput      func(IN) (CreateInput, error)
	Completed       connector.Target[CreateWidgetStepOutput[IN]]
	Rejected        connector.Target[CreateWidgetStepOutput[IN]]
	Uncertain       connector.Target[CreateWidgetStepOutput[IN]]
	Defect          connector.Target[CreateWidgetStepOutput[IN]]
	ResultAttribute *dex.Attribute[connector.MutationResult[Widget]]
	ProgressStream  *dex.Stream[connector.ProgressUpdate]
}

func NewCreateWidgetStep[IN any](config CreateWidgetStepConfig[IN]) connector.MutationStep[IN, CreateInput, Widget] {
	if err := config.Connection.validate(); err != nil {
		panic(err)
	}
	return connector.MustNewMutationStep(connector.MutationStepConfig[IN, CreateInput, Widget]{
		StepType: config.StepType, Presentation: config.Presentation,
		Operation: createWidgetOperation{connection: config.Connection}, Connection: config.Connection.reference,
		BuildInput: config.BuildInput,
		Branches: []connector.BranchTarget[CreateWidgetStepOutput[IN]]{
			config.Completed.BranchTarget(createWidgetCompleted),
			config.Rejected.BranchTarget(createWidgetRejected),
			config.Uncertain.BranchTarget(createWidgetUncertain),
			config.Defect.BranchTarget(createWidgetDefect),
		},
		ResultAttribute: config.ResultAttribute,
		ProgressStream:  config.ProgressStream,
	})
}
