// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package recordrefund demonstrates the PostgreSQL query and execute operations in a Flow started
// from Dex Web Start Flow. It records one refund in a ledger table at most once: a read-only query
// finds an existing refund for the order, and an idempotent INSERT keyed by the Step's idempotency
// key writes a new one. An uncertain COMMIT is reconciled by reading the row back by that key.
package recordrefund

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "PostgreSQLRecordRefund"
	// ConnectionName is the static Dex Web connection for the refund ledger database.
	ConnectionName = "postgresql-ledger"

	// StatusRecorded means this Flow's INSERT wrote the refund.
	StatusRecorded = "recorded"
	// StatusAlreadyRecorded means the ledger already held a refund for the order.
	StatusAlreadyRecorded = "alreadyRecorded"

	recordRefundRequestStepType        = "RecordRefundRequest"
	findRecordedRefundStepType         = "FindRecordedRefund"
	decideRefundRecordingStepType      = "DecideRefundRecording"
	insertRefundStepType               = "InsertRefund"
	completeRefundRecordingStepType    = "CompleteRefundRecording"
	findRefundByIdempotencyKeyStepType = "FindRefundByIdempotencyKey"
	completeReconciledRefundStepType   = "CompleteReconciledRefund"

	refundColumns = "refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at"
	// FindRecordedRefundStatement reads the order's existing refund inside a read-only transaction.
	FindRecordedRefundStatement = "SELECT " + refundColumns + " FROM refund_ledger WHERE order_id = $1"
	// InsertRefundStatement writes at most one row per Step execution; $4 is the idempotency key.
	InsertRefundStatement = `INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key)
VALUES ($1, $2::numeric, $3, $4)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING ` + refundColumns
	// FindRefundByIdempotencyKeyStatement reads back the row an uncertain or replayed INSERT may have written.
	FindRefundByIdempotencyKeyStatement = "SELECT " + refundColumns + " FROM refund_ledger WHERE idempotency_key = $1"
)

var (
	refundRequestAttribute = dex.DefineAttribute[Input]("postgresql-refund-request")
	refundOutcomeAttribute = dex.DefineAttribute[Outcome]("postgresql-refund-outcome")

	amountPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,2})?$`)
)

// Input contains the refund fields entered in Dex Web Start Flow.
type Input struct {
	// OrderID identifies the refunded order; the ledger holds at most one refund per order.
	OrderID string `json:"orderId"`
	// AmountUSD is the positive refund amount as an exact decimal string, such as "250.00".
	AmountUSD string `json:"amountUsd"`
	// ExternalReference is an optional support-ticket or payment reference.
	ExternalReference string `json:"externalReference,omitempty"`
}

// Refund is one refund_ledger row decoded from the connector's JSON type mapping.
type Refund struct {
	// RefundID is the bigint identity, returned as a decimal string.
	RefundID string `json:"refund_id"`
	// OrderID is the refunded order.
	OrderID string `json:"order_id"`
	// AmountUSD is the numeric(12,2) amount, returned as an exact decimal string.
	AmountUSD string `json:"amount_usd"`
	// ExternalReference is the optional reference, or nil for SQL NULL.
	ExternalReference *string `json:"external_reference"`
	// IdempotencyKey is the uuid of the Step execution that inserted the row.
	IdempotencyKey string `json:"idempotency_key"`
	// RecordedAt is the timestamptz insertion time in RFC 3339 UTC.
	RecordedAt string `json:"recorded_at"`
}

// Outcome is the Flow result.
type Outcome struct {
	// Status is StatusRecorded or StatusAlreadyRecorded.
	Status string `json:"status"`
	// Refund is the ledger row for the order.
	Refund Refund `json:"refund"`
	// WasReconciled reports that the row was confirmed by reading it back after an uncertain or replayed INSERT.
	WasReconciled bool `json:"wasReconciled,omitempty"`
}

// Flow records one refund in a PostgreSQL ledger at most once.
type Flow struct {
	dex.FlowDefaults
	connection postgresql.Connection
}

// NewFlow binds the PostgreSQL Connection at registration time.
func NewFlow(connection postgresql.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, query, decision, write, and reconciliation Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRefundRequest{}),
		dex.DefineStep(postgresql.NewQueryRowsStep(postgresql.QueryRowsStepConfig[Input]{
			StepType: findRecordedRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "postgresql", GroupLabel: "PostgreSQL",
				Explanation: "Read the order's existing refund in a read-only transaction.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToFindRecordedRefundInput,
			Completed:           sdkgo.GoTo(decideRefundRecording{}),
		})),
		dex.DefineStep(decideRefundRecording{}),
		dex.DefineStep(postgresql.NewExecuteStatementStep(postgresql.ExecuteStatementStepConfig[Input]{
			StepType: insertRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "postgresql", GroupLabel: "PostgreSQL",
				Explanation: "Insert the refund once, keyed by this Step's idempotency key.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToInsertRefundInput,
			Completed:           sdkgo.GoTo(completeRefundRecording{}),
			Uncertain:           sdkgo.GoTo(sdkgo.StepRef[postgresql.ExecuteStatementResult](findRefundByIdempotencyKeyStepType)),
		})),
		dex.DefineStep(completeRefundRecording{}),
		dex.DefineStep(postgresql.NewQueryRowsStep(postgresql.QueryRowsStepConfig[postgresql.ExecuteStatementResult]{
			StepType: findRefundByIdempotencyKeyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "postgresql", GroupLabel: "PostgreSQL",
				Explanation: "Read back the row an uncertain or replayed insert may have written.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToFindRefundByIdempotencyKeyInput,
			Completed:           sdkgo.GoTo(completeReconciledRefund{}),
		})),
		dex.DefineStep(completeReconciledRefund{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{refundRequestAttribute, refundOutcomeAttribute}}
}

// GetDexSummary returns the refund request and its recorded outcome.
//
// dex:field attribute-key:postgresql-refund-request value-type:json editable:false description:"Submitted refund request"
// dex:field attribute-key:postgresql-refund-outcome value-type:json editable:false description:"Recorded refund row and status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"postgresql-refund-request": request,
		"postgresql-refund-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the refund request and its recorded outcome.
//
// dex:field attribute-key:postgresql-refund-request value-type:json editable:false description:"Order, amount, and reference"
// dex:field attribute-key:postgresql-refund-outcome value-type:json editable:false description:"Status, ledger row, and idempotency key"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"postgresql-refund-request": request,
		"postgresql-refund-outcome": outcome,
	}}, nil
}

// MapToFindRecordedRefundInput binds the order ID to the read-only duplicate check.
func (*Flow) MapToFindRecordedRefundInput(input Input) postgresql.QueryRowsInput {
	return postgresql.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{input.OrderID}}
}

// MapToInsertRefundInput binds the refund fields; the connector binds the idempotency key as $4.
func (*Flow) MapToInsertRefundInput(input Input) postgresql.ExecuteStatementInput {
	var externalReference any
	if input.ExternalReference != "" {
		externalReference = input.ExternalReference
	}
	maxRowsAffected := int64(1)
	return postgresql.ExecuteStatementInput{
		Statement:                 InsertRefundStatement,
		Parameters:                []any{input.OrderID, input.AmountUSD, externalReference},
		IdempotencyKeyPlaceholder: 4,
		MaxRowsAffected:           &maxRowsAffected,
	}
}

// MapToFindRefundByIdempotencyKeyInput reads back by the key the INSERT's Receipt reports.
func (*Flow) MapToFindRefundByIdempotencyKeyInput(result postgresql.ExecuteStatementResult) postgresql.QueryRowsInput {
	return postgresql.QueryRowsInput{
		Statement: FindRefundByIdempotencyKeyStatement, Parameters: []any{string(result.Receipt.IdempotencyKey)},
	}
}

func refundInspection(ctx dex.Context) (Input, Outcome, error) {
	request, err := optionalAttribute(ctx, refundRequestAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, refundOutcomeAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// decodeRefund converts one row map into the typed ledger row.
func decodeRefund(row map[string]any) (Refund, error) {
	encoded, err := json.Marshal(row)
	if err != nil {
		return Refund{}, err
	}
	var refund Refund
	if err := json.Unmarshal(encoded, &refund); err != nil {
		return Refund{}, fmt.Errorf("decode refund_ledger row: %w", err)
	}
	return refund, nil
}

// dex:group group-id:postgresql group-label:"PostgreSQL"
// dex:explanation text:"Validate and record the refund request before touching the database."
type recordRefundRequest struct {
	dex.StepDefaults
}

func (recordRefundRequest) GetStepType() string { return recordRefundRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordRefundRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordRefundRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.AmountUSD = strings.TrimSpace(input.AmountUSD)
	input.ExternalReference = strings.TrimSpace(input.ExternalReference)
	if input.OrderID == "" || len(input.OrderID) > 100 {
		return dex.ForceFail("orderId is required and must be at most 100 bytes"), nil
	}
	if !amountPattern.MatchString(input.AmountUSD) || strings.Trim(input.AmountUSD, "0.") == "" {
		return dex.ForceFail("amountUsd must be a positive decimal string with at most two fractional digits, such as 250.00"), nil
	}
	if len(input.ExternalReference) > 200 {
		return dex.ForceFail("externalReference must be at most 200 bytes"), nil
	}
	if err := refundRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](findRecordedRefundStepType), input), nil
}

// dex:group group-id:postgresql group-label:"PostgreSQL"
// dex:explanation text:"Complete with the existing refund, or continue to insert a new one."
type decideRefundRecording struct {
	dex.StepDefaultsNoWaitFor[postgresql.QueryRowsResult]
}

func (decideRefundRecording) GetStepType() string { return decideRefundRecordingStepType }

func (decideRefundRecording) Execute(ctx dex.Context, result postgresql.QueryRowsResult) (*dex.StepDecision, error) {
	if len(result.Value.Rows) > 0 {
		refund, err := decodeRefund(result.Value.Rows[0])
		if err != nil {
			return dex.ForceFail(err.Error()), nil
		}
		outcome := Outcome{Status: StatusAlreadyRecorded, Refund: refund}
		if err := refundOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	request, err := refundRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](insertRefundStepType), request), nil
}

// dex:group group-id:postgresql group-label:"PostgreSQL"
// dex:explanation text:"Complete with the inserted row, or read it back when a replay inserted nothing."
type completeRefundRecording struct {
	dex.StepDefaultsNoWaitFor[postgresql.ExecuteStatementResult]
}

func (completeRefundRecording) GetStepType() string { return completeRefundRecordingStepType }

// Execute handles a replay of the same Step execution, whose ON CONFLICT DO NOTHING returns no row.
func (completeRefundRecording) Execute(ctx dex.Context, result postgresql.ExecuteStatementResult) (*dex.StepDecision, error) {
	if len(result.Value.Rows) == 0 {
		return dex.GoTo(sdkgo.StepRef[postgresql.ExecuteStatementResult](findRefundByIdempotencyKeyStepType), result), nil
	}
	refund, err := decodeRefund(result.Value.Rows[0])
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	outcome := Outcome{Status: StatusRecorded, Refund: refund}
	if err := refundOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:postgresql group-label:"PostgreSQL"
// dex:explanation text:"Complete when the read-back found the row; otherwise stop for review."
type completeReconciledRefund struct {
	dex.StepDefaultsNoWaitFor[postgresql.QueryRowsResult]
}

func (completeReconciledRefund) GetStepType() string { return completeReconciledRefundStepType }

// Execute fails instead of inserting again, because a stalled COMMIT can still become visible later.
func (completeReconciledRefund) Execute(ctx dex.Context, result postgresql.QueryRowsResult) (*dex.StepDecision, error) {
	if len(result.Value.Rows) == 0 {
		return dex.ForceFail("no refund_ledger row carries this Flow's idempotency key; inspect the ledger before recording the refund again"), nil
	}
	refund, err := decodeRefund(result.Value.Rows[0])
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	outcome := Outcome{Status: StatusRecorded, Refund: refund, WasReconciled: true}
	if err := refundOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
