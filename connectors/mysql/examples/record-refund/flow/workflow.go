// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package recordrefund demonstrates the MySQL query and execute operations in a Flow started from
// Dex Web Start Flow. It records one refund in a ledger table at most once: a read-only query finds
// an existing refund for the order, and an INSERT ... ON DUPLICATE KEY UPDATE keyed by the Step's
// idempotency key writes a new one. MySQL has no RETURNING clause, so the Flow always reads the row
// back, which also reconciles an uncertain COMMIT.
package recordrefund

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/mysql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "MySQLRecordRefund"
	// ConnectionName is the static Dex Web connection for the refund ledger database.
	ConnectionName = "mysql-ledger"

	// StatusRecorded means this Flow's INSERT wrote the refund.
	StatusRecorded = "recorded"
	// StatusAlreadyRecorded means the ledger already held a refund for the order, written by another Flow.
	StatusAlreadyRecorded = "alreadyRecorded"

	recordRefundRequestStepType     = "RecordRefundRequest"
	findRecordedRefundStepType      = "FindRecordedRefund"
	decideRefundRecordingStepType   = "DecideRefundRecording"
	insertRefundStepType            = "InsertRefund"
	findInsertedRefundStepType      = "FindInsertedRefund"
	completeRefundRecordingStepType = "CompleteRefundRecording"

	refundColumns = "refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at"
	// FindRecordedRefundStatement reads the order's existing refund inside a read-only transaction.
	FindRecordedRefundStatement = "SELECT " + refundColumns + " FROM refund_ledger WHERE order_id = ?"
	// InsertRefundStatement writes at most one row per order; the fourth ? is the idempotency key.
	//
	// ON DUPLICATE KEY UPDATE turns a conflict on any unique key into an update of the existing row:
	// the idempotency key for a replayed dispatch of this Step execution, or order_id for another
	// Flow's refund. Assigning refund_id to itself changes nothing, so the statement reports zero rows
	// affected, and LAST_INSERT_ID(refund_id) reports the existing row's ID for the read-back.
	InsertRefundStatement = `INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE refund_id = LAST_INSERT_ID(refund_id)`
	// FindInsertedRefundStatement reads back this Step's row by its key, or the conflicting row by the reported ID.
	FindInsertedRefundStatement = "SELECT " + refundColumns + " FROM refund_ledger WHERE idempotency_key = ? OR refund_id = CAST(? AS UNSIGNED)"
)

var (
	refundRequestAttribute      = dex.DefineAttribute[Input]("mysql-refund-request")
	refundOutcomeAttribute      = dex.DefineAttribute[Outcome]("mysql-refund-outcome")
	refundInsertResultAttribute = dex.DefineAttribute[mysql.ExecuteStatementResult]("mysql-refund-insert-result")

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
	// RefundID is the BIGINT UNSIGNED AUTO_INCREMENT key, returned as a decimal string.
	RefundID string `json:"refund_id"`
	// OrderID is the refunded order.
	OrderID string `json:"order_id"`
	// AmountUSD is the DECIMAL(12,2) amount, returned as an exact decimal string.
	AmountUSD string `json:"amount_usd"`
	// ExternalReference is the optional reference, or nil for SQL NULL.
	ExternalReference *string `json:"external_reference"`
	// IdempotencyKey is the key of the Step execution that inserted the row.
	IdempotencyKey string `json:"idempotency_key"`
	// RecordedAt is the TIMESTAMP(6) insertion time in RFC 3339 UTC.
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

// Flow records one refund in a MySQL ledger at most once.
type Flow struct {
	dex.FlowDefaults
	connection mysql.Connection
}

// NewFlow binds the MySQL Connection at registration time.
func NewFlow(connection mysql.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, query, decision, write, read-back, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRefundRequest{}),
		dex.DefineStep(mysql.NewQueryRowsStep(mysql.QueryRowsStepConfig[Input]{
			StepType: findRecordedRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mysql", GroupLabel: "MySQL",
				Explanation: "Read the order's existing refund in a read-only transaction.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToFindRecordedRefundInput,
			Completed:           sdkgo.GoTo(decideRefundRecording{}),
		})),
		dex.DefineStep(decideRefundRecording{}),
		dex.DefineStep(mysql.NewExecuteStatementStep(mysql.ExecuteStatementStepConfig[Input]{
			StepType: insertRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mysql", GroupLabel: "MySQL",
				Explanation: "Insert the refund once with ON DUPLICATE KEY UPDATE, keyed by this Step's idempotency key.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToInsertRefundInput,
			Completed:           sdkgo.GoTo(sdkgo.StepRef[mysql.ExecuteStatementResult](findInsertedRefundStepType)),
			Uncertain:           sdkgo.GoTo(sdkgo.StepRef[mysql.ExecuteStatementResult](findInsertedRefundStepType)),
			ResultAttribute:     &refundInsertResultAttribute,
		})),
		dex.DefineStep(mysql.NewQueryRowsStep(mysql.QueryRowsStepConfig[mysql.ExecuteStatementResult]{
			StepType: findInsertedRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mysql", GroupLabel: "MySQL",
				Explanation: "Read back the row by this Step's idempotency key or the ID the insert reported.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToFindInsertedRefundInput,
			Completed:           sdkgo.GoTo(completeRefundRecording{}),
		})),
		dex.DefineStep(completeRefundRecording{}),
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
// dex:field attribute-key:mysql-refund-request value-type:json editable:false description:"Submitted refund request"
// dex:field attribute-key:mysql-refund-outcome value-type:json editable:false description:"Recorded refund row and status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"mysql-refund-request": request,
		"mysql-refund-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the refund request and its recorded outcome.
//
// dex:field attribute-key:mysql-refund-request value-type:json editable:false description:"Order, amount, and reference"
// dex:field attribute-key:mysql-refund-outcome value-type:json editable:false description:"Status, ledger row, and idempotency key"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"mysql-refund-request": request,
		"mysql-refund-outcome": outcome,
	}}, nil
}

// MapToFindRecordedRefundInput binds the order ID to the read-only duplicate check.
func (*Flow) MapToFindRecordedRefundInput(input Input) mysql.QueryRowsInput {
	return mysql.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{input.OrderID}}
}

// MapToInsertRefundInput binds the refund fields; the connector binds the idempotency key as the fourth ?.
func (*Flow) MapToInsertRefundInput(input Input) mysql.ExecuteStatementInput {
	var externalReference any
	if input.ExternalReference != "" {
		externalReference = input.ExternalReference
	}
	maxRowsAffected := int64(1)
	return mysql.ExecuteStatementInput{
		Statement:                 InsertRefundStatement,
		Parameters:                []any{input.OrderID, input.AmountUSD, externalReference},
		IdempotencyKeyPlaceholder: 4,
		MaxRowsAffected:           &maxRowsAffected,
	}
}

// MapToFindInsertedRefundInput reads back by the key the Receipt reports and the ID LAST_INSERT_ID() reported.
//
// An uncertain insert reports LastInsertID "0", which matches no AUTO_INCREMENT row, so only the key applies.
func (*Flow) MapToFindInsertedRefundInput(result mysql.ExecuteStatementResult) mysql.QueryRowsInput {
	return mysql.QueryRowsInput{
		Statement:  FindInsertedRefundStatement,
		Parameters: []any{string(result.Receipt.IdempotencyKey), result.Value.LastInsertID},
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

// resolveRefundOutcome classifies the read-back: this Step's row, another Flow's row for the order, or none.
func resolveRefundOutcome(insert mysql.ExecuteStatementResult, readBack mysql.QueryRowsResult) (Outcome, error) {
	var otherFlowsRefund *Refund
	for _, row := range readBack.Value.Rows {
		refund, err := decodeRefund(row)
		if err != nil {
			return Outcome{}, err
		}
		if refund.IdempotencyKey == string(insert.Receipt.IdempotencyKey) {
			wasReconciled := insert.Branch == mysql.ExecuteStatementBranchUncertain || insert.Value.RowsAffected == 0
			return Outcome{Status: StatusRecorded, Refund: refund, WasReconciled: wasReconciled}, nil
		}
		otherFlowsRefund = &refund
	}
	if otherFlowsRefund != nil && insert.Branch == mysql.ExecuteStatementBranchCompleted {
		return Outcome{Status: StatusAlreadyRecorded, Refund: *otherFlowsRefund}, nil
	}
	return Outcome{}, errRefundNotFound
}

var errRefundNotFound = errors.New("no refund_ledger row carries this Flow's idempotency key; inspect the ledger before recording the refund again")

// dex:group group-id:mysql group-label:"MySQL"
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

// dex:group group-id:mysql group-label:"MySQL"
// dex:explanation text:"Complete with the existing refund, or continue to insert a new one."
type decideRefundRecording struct {
	dex.StepDefaultsNoWaitFor[mysql.QueryRowsResult]
}

func (decideRefundRecording) GetStepType() string { return decideRefundRecordingStepType }

func (decideRefundRecording) Execute(ctx dex.Context, result mysql.QueryRowsResult) (*dex.StepDecision, error) {
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

// dex:group group-id:mysql group-label:"MySQL"
// dex:explanation text:"Complete with this Flow's row or another Flow's refund; otherwise stop for review."
type completeRefundRecording struct {
	dex.StepDefaultsNoWaitFor[mysql.QueryRowsResult]
}

func (completeRefundRecording) GetStepType() string { return completeRefundRecordingStepType }

// Execute fails rather than inserting again, because a stalled COMMIT can still become visible later.
func (completeRefundRecording) Execute(ctx dex.Context, readBack mysql.QueryRowsResult) (*dex.StepDecision, error) {
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
