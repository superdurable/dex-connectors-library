// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package statementresults

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/redshift/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ExecuteStatementFlowType identifies the SQL submission example.
	ExecuteStatementFlowType = "AmazonRedshiftExecuteStatement"
	executeStatementStepType = "CallExecuteStatement"
)

var executeStatementOutcomeAttribute = dex.DefineAttribute[redshift.StatementIdentity]("redshift-execute-statement-outcome")

// ExecuteStatementFlow submits one non-secret SQL statement to a serverless workgroup.
type ExecuteStatementFlow struct {
	dex.FlowDefaults
	connection redshift.Connection
}

// NewExecuteStatementFlow binds the static Redshift connection at registration.
func NewExecuteStatementFlow(connection redshift.Connection) *ExecuteStatementFlow {
	return &ExecuteStatementFlow{connection: connection}
}

func (*ExecuteStatementFlow) GetFlowType() string { return ExecuteStatementFlowType }
func (*ExecuteStatementFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{executeStatementOutcomeAttribute}}
}
func (flow *ExecuteStatementFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startExecuteStatement{}),
		dex.DefineStep(redshift.NewExecuteStatementStep(redshift.ExecuteStatementStepConfig[redshift.ExecuteStatementInput]{
			StepType: executeStatementStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon Redshift", Explanation: "Submit one SQL statement with a Dex-derived client token."},
			Connection:  flow.connection, MapToOperationInput: MapToExecuteStatementInput,
			Submitted: sdkgo.GoTo(completeExecuteStatement{}),
		})),
		dex.DefineStep(completeExecuteStatement{}),
	}
}

// GetRPCs exposes the write outcome in Dex Web summary and display.
func (flow *ExecuteStatementFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:redshift-execute-statement-outcome value-type:json editable:false description:"SQL submission outcome"
// GetDexSummary returns the persisted write outcome.
func (*ExecuteStatementFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readExecuteStatementOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"redshift-execute-statement-outcome": outcome}}, nil
}

// dex:field attribute-key:redshift-execute-statement-outcome value-type:json editable:false description:"SQL submission outcome"
// GetDexDisplay returns the persisted write outcome.
func (*ExecuteStatementFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readExecuteStatementOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"redshift-execute-statement-outcome": outcome}}, nil
}

func readExecuteStatementOutcome(ctx dex.Context) (redshift.StatementIdentity, error) {
	outcome, err := executeStatementOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return redshift.StatementIdentity{}, nil
	}
	return outcome, err
}

// MapToExecuteStatementInput preserves the typed write request.
func MapToExecuteStatementInput(input redshift.ExecuteStatementInput) redshift.ExecuteStatementInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the SQL statement to its provider Step."
type startExecuteStatement struct{ dex.StepDefaults }

func (startExecuteStatement) GetStepType() string { return "StartExecuteStatement" }
func (startExecuteStatement) WaitFor(dex.Context, redshift.ExecuteStatementInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startExecuteStatement) Execute(_ dex.Context, input redshift.ExecuteStatementInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[redshift.ExecuteStatementInput](executeStatementStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Redshift accepts the SQL statement."
type completeExecuteStatement struct {
	dex.StepDefaultsNoWaitFor[redshift.ExecuteStatementResult]
}

func (completeExecuteStatement) GetStepType() string { return "CompleteExecuteStatement" }
func (completeExecuteStatement) Execute(ctx dex.Context, result redshift.ExecuteStatementResult) (*dex.StepDecision, error) {
	if err := executeStatementOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ExecuteStatementFlow)(nil)
