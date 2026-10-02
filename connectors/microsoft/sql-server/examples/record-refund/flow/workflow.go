// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package recordrefund demonstrates the SQL Server query and execute operations in a Flow started
// from Dex Web Start Flow. It records one refund in a ledger table at most once: a query finds an
// existing refund for the order, and an INSERT ... SELECT ... WHERE NOT EXISTS keyed by the Step's
// idempotency key writes a new one and returns it through OUTPUT. A replayed or uncertain INSERT is
// reconciled by reading the row back by that key.
package recordrefund

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "SQLServerRecordRefund"
	// ConnectionName is the static Dex Web connection for the refund ledger database.
	ConnectionName = "sql-server-ledger"

	// StatusRecorded means this Flow's INSERT wrote the refund.
	StatusRecorded = "recorded"
	// StatusAlreadyRecorded means the ledger already held a refund for the order, written by another Flow.
	StatusAlreadyRecorded = "alreadyRecorded"

	recordRefundRequestStepType     = "RecordRefundRequest"
	findRecordedRefundStepType      = "FindRecordedRefund"
	decideRefundRecordingStepType   = "DecideRefundRecording"
	insertRefundStepType            = "InsertRefund"
	completeRefundRecordingStepType = "CompleteRefundRecording"
	findInsertedRefundStepType      = "FindInsertedRefund"
	resolveInsertedRefundStepType   = "ResolveInsertedRefund"

	refundColumns         = "refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at"
	insertedRefundColumns = "INSERTED.refund_id, INSERTED.order_id, INSERTED.amount_usd, INSERTED.external_reference, " +
		"INSERTED.idempotency_key, INSERTED.recorded_at"
	// FindRecordedRefundStatement reads the order's existing refund.
	FindRecordedRefundStatement = "SELECT " + refundColumns + " FROM dbo.refund_ledger WHERE order_id = @p1"
	// InsertRefundStatement writes at most one row per order and returns it; @p4 is the idempotency key.
	//
	// UPDLOCK and HOLDLOCK keep the NOT EXISTS range locked until COMMIT, so a replayed dispatch of
	// this Step execution, or another Flow's refund for the order, waits and then inserts nothing.
	InsertRefundStatement = "INSERT INTO dbo.refund_ledger (order_id, amount_usd, external_reference, idempotency_key)\n" +
		"OUTPUT " + insertedRefundColumns + "\n" +
		"SELECT @p1, CAST(@p2 AS decimal(12, 2)), @p3, CAST(@p4 AS uniqueidentifier)\n" +
		"WHERE NOT EXISTS (SELECT 1 FROM dbo.refund_ledger WITH (UPDLOCK, HOLDLOCK)\n" +
		"WHERE idempotency_key = CAST(@p4 AS uniqueidentifier) OR order_id = @p1)"
	// FindInsertedRefundStatement reads back this Step's row by its key, or the order's row another Flow wrote.
	FindInsertedRefundStatement = "SELECT " + refundColumns + " FROM dbo.refund_ledger " +
		"WHERE idempotency_key = CAST(@p1 AS uniqueidentifier) OR order_id = @p2"
)

var (
	refundRequestAttribute      = dex.DefineAttribute[Input]("sql-server-refund-request")
	refundOutcomeAttribute      = dex.DefineAttribute[Outcome]("sql-server-refund-outcome")
	refundInsertResultAttribute = dex.DefineAttribute[sqlserver.ExecuteStatementResult]("sql-server-refund-insert-result")

	amountPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,2})?$`)

	errRefundNotFound = errors.New("no refund_ledger row carries this Flow's idempotency key; inspect the ledger before recording the refund again")
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
	// RefundID is the bigint IDENTITY key, returned as a decimal string.
	RefundID string `json:"refund_id"`
	// OrderID is the refunded order.
	OrderID string `json:"order_id"`
	// AmountUSD is the decimal(12, 2) amount, returned as an exact decimal string.
	AmountUSD string `json:"amount_usd"`
	// ExternalReference is the optional reference, or nil for SQL NULL.
	ExternalReference *string `json:"external_reference"`
	// IdempotencyKey is the uniqueidentifier of the Step execution that inserted the row, in lowercase.
	IdempotencyKey string `json:"idempotency_key"`
	// RecordedAt is the datetime2(6) insertion time in UTC, without an offset.
	RecordedAt string `json:"recorded_at"`
}

// Outcome is the Flow result.
type Outcome struct {
	// Status is StatusRecorded or StatusAlreadyRecorded.
	Status string `json:"status"`
	// Refund is the ledger row for the order.
	Refund Refund `json:"refund"`
	// WasReconciled reports that this Flow's row was confirmed by the read-back after an uncertain or replayed INSERT.
	WasReconciled bool `json:"wasReconciled,omitempty"`
}

// RefundReadBack names the row an uncertain or row-less INSERT may have written, or another Flow's row for the order.
type RefundReadBack struct {
	// IdempotencyKey is the INSERT's key from its Receipt.
	IdempotencyKey string `json:"idempotencyKey"`
	// OrderID is the refunded order.
	OrderID string `json:"orderId"`
}

// Flow records one refund in a SQL Server ledger at most once.
type Flow struct {
	dex.FlowDefaults
	connection sqlserver.Connection
}

// NewFlow binds the SQL Server Connection at registration time.
func NewFlow(connection sqlserver.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, query, decision, write, read-back, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRefundRequest{}),
		dex.DefineStep(sqlserver.NewQueryRowsStep(sqlserver.QueryRowsStepConfig[Input]{
			StepType: findRecordedRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "sql-server", GroupLabel: "SQL Server",
				Explanation: "Read the order's existing refund in a transaction that is rolled back.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToFindRecordedRefundInput,
			Completed:           sdkgo.GoTo(decideRefundRecording{}),
		})),
		dex.DefineStep(decideRefundRecording{}),
		dex.DefineStep(sqlserver.NewExecuteStatementStep(sqlserver.ExecuteStatementStepConfig[Input]{
			StepType: insertRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "sql-server", GroupLabel: "SQL Server",
				Explanation: "Insert the refund once with WHERE NOT EXISTS, keyed by this Step's idempotency key.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToInsertRefundInput,
			Completed:           sdkgo.GoTo(completeRefundRecording{}),
			Uncertain:           sdkgo.GoTo(completeRefundRecording{}),
			ResultAttribute:     &refundInsertResultAttribute,
		})),
		dex.DefineStep(completeRefundRecording{}),
		dex.DefineStep(sqlserver.NewQueryRowsStep(sqlserver.QueryRowsStepConfig[RefundReadBack]{
			StepType: findInsertedRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "sql-server", GroupLabel: "SQL Server",
				Explanation: "Read back the row by this Step's idempotency key or the order's existing row.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToFindInsertedRefundInput,
			Completed:           sdkgo.GoTo(resolveInsertedRefund{}),
		})),
		dex.DefineStep(resolveInsertedRefund{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, outcome, and insert result Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{refundRequestAttribute, refundOutcomeAttribute, refundInsertResultAttribute}}
}

// GetDexSummary returns the refund request and its recorded outcome.
//
// dex:field attribute-key:sql-server-refund-request value-type:json editable:false description:"Submitted refund request"
// dex:field attribute-key:sql-server-refund-outcome value-type:json editable:false description:"Recorded refund row and status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"sql-server-refund-request": request,
		"sql-server-refund-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the refund request and its recorded outcome.
//
// dex:field attribute-key:sql-server-refund-request value-type:json editable:false description:"Order, amount, and reference"
// dex:field attribute-key:sql-server-refund-outcome value-type:json editable:false description:"Status, ledger row, and idempotency key"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"sql-server-refund-request": request,
		"sql-server-refund-outcome": outcome,
	}}, nil
}

// MapToFindRecordedRefundInput binds the order ID to the duplicate check.
func (*Flow) MapToFindRecordedRefundInput(input Input) sqlserver.QueryRowsInput {
	return sqlserver.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{input.OrderID}}
}

// MapToInsertRefundInput binds the refund fields; the connector binds the idempotency key as @p4.
func (*Flow) MapToInsertRefundInput(input Input) sqlserver.ExecuteStatementInput {
	var externalReference any
	if input.ExternalReference != "" {
		externalReference = input.ExternalReference
	}
	maxRowsAffected := int64(1)
	return sqlserver.ExecuteStatementInput{
		Statement:                 InsertRefundStatement,
		Parameters:                []any{input.OrderID, input.AmountUSD, externalReference},
		IdempotencyKeyPlaceholder: 4,
		MaxRowsAffected:           &maxRowsAffected,
		ReturnsOutputRows:         true,
	}
}

// MapToFindInsertedRefundInput reads back by the key the INSERT's Receipt reported and the request's order.
func (*Flow) MapToFindInsertedRefundInput(readBack RefundReadBack) sqlserver.QueryRowsInput {
	return sqlserver.QueryRowsInput{Statement: FindInsertedRefundStatement, Parameters: []any{readBack.IdempotencyKey, readBack.OrderID}}
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

// resolveRefundOutcome accepts another Flow's row only after a completed INSERT, which proves this Step wrote nothing.
func resolveRefundOutcome(insert sqlserver.ExecuteStatementResult, readBack sqlserver.QueryRowsResult) (Outcome, error) {
	var otherFlowsRefund *Refund
	for _, row := range readBack.Value.Rows {
		refund, err := decodeRefund(row)
		if err != nil {
			return Outcome{}, err
		}
		if strings.EqualFold(refund.IdempotencyKey, string(insert.Receipt.IdempotencyKey)) {
			return Outcome{Status: StatusRecorded, Refund: refund, WasReconciled: true}, nil
		}
		otherFlowsRefund = &refund
	}
	if otherFlowsRefund != nil && insert.Branch == sqlserver.ExecuteStatementBranchCompleted {
		return Outcome{Status: StatusAlreadyRecorded, Refund: *otherFlowsRefund}, nil
	}
	return Outcome{}, errRefundNotFound
}

// dex:group group-id:sql-server group-label:"SQL Server"
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

// dex:group group-id:sql-server group-label:"SQL Server"
// dex:explanation text:"Complete with the existing refund, or continue to insert a new one."
type decideRefundRecording struct {
	dex.StepDefaultsNoWaitFor[sqlserver.QueryRowsResult]
}

func (decideRefundRecording) GetStepType() string { return decideRefundRecordingStepType }

func (decideRefundRecording) Execute(ctx dex.Context, result sqlserver.QueryRowsResult) (*dex.StepDecision, error) {
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

// dex:group group-id:sql-server group-label:"SQL Server"
// dex:explanation text:"Complete with the OUTPUT row, or read it back after a replayed or uncertain insert."
type completeRefundRecording struct {
	dex.StepDefaultsNoWaitFor[sqlserver.ExecuteStatementResult]
}

func (completeRefundRecording) GetStepType() string { return completeRefundRecordingStepType }

// Execute reads back when the INSERT is uncertain or returned no row: a replay, or another Flow's refund.
func (completeRefundRecording) Execute(ctx dex.Context, result sqlserver.ExecuteStatementResult) (*dex.StepDecision, error) {
	if result.Branch == sqlserver.ExecuteStatementBranchCompleted && len(result.Value.Rows) == 1 {
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
	request, err := refundRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	readBack := RefundReadBack{IdempotencyKey: string(result.Receipt.IdempotencyKey), OrderID: request.OrderID}
	return dex.GoTo(sdkgo.StepRef[RefundReadBack](findInsertedRefundStepType), readBack), nil
}

// dex:group group-id:sql-server group-label:"SQL Server"
// dex:explanation text:"Complete with this Flow's row or another Flow's refund; otherwise stop for review."
type resolveInsertedRefund struct {
	dex.StepDefaultsNoWaitFor[sqlserver.QueryRowsResult]
}

func (resolveInsertedRefund) GetStepType() string { return resolveInsertedRefundStepType }

// Execute fails rather than inserting again, because a stalled COMMIT can still become visible later.
func (resolveInsertedRefund) Execute(ctx dex.Context, readBack sqlserver.QueryRowsResult) (*dex.StepDecision, error) {
	insert, err := refundInsertResultAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := resolveRefundOutcome(insert, readBack)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := refundOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
