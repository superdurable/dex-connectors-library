// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountusagesummary demonstrates the Snowflake submitStatement, getStatementResult, and
// cancelStatement operations in a Flow started from Dex Web Start Flow. It submits one long
// aggregate over a usage table, waits on a durable Timer between status reads instead of holding a
// Step open, and cancels the statement when the Flow's wait budget runs out.
package accountusagesummary

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "SnowflakeAccountUsageSummary"
	// ConnectionName is the static Dex Web connection for the Snowflake account.
	ConnectionName = "snowflake-warehouse"

	recordUsageRequestStepType        = "RecordUsageRequest"
	submitUsageQueryStepType          = "SubmitUsageQuery"
	recordSubmittedQueryStepType      = "RecordSubmittedQuery"
	recordRejectedQueryStepType       = "RecordRejectedQuery"
	waitForUsageQueryStepType         = "WaitForUsageQuery"
	readUsageResultStepType           = "ReadUsageResult"
	recordRunningQueryStepType        = "RecordRunningQuery"
	completeUsageSummaryStepType      = "CompleteUsageSummary"
	recordFailedQueryStepType         = "RecordFailedQuery"
	cancelUsageQueryStepType          = "CancelUsageQuery"
	recordWaitBudgetExhaustedStepType = "RecordWaitBudgetExhausted"

	// UsageSummaryStatement aggregates one account's usage since a date; both values are bound.
	UsageSummaryStatement = `SELECT COUNT(*) AS EVENT_COUNT,
       COALESCE(SUM(CREDITS_USED), 0) AS CREDITS_USED,
       MAX(EVENT_AT) AS LAST_EVENT_AT
FROM USAGE_EVENTS
WHERE ACCOUNT_ID = ? AND EVENT_AT >= TO_DATE(?)`
)

// Phases stored in the snowflake-usage-phase Attribute.
const (
	// PhaseSubmitting means the statement is about to be submitted.
	PhaseSubmitting = "submitting"
	// PhaseRunning means Snowflake accepted the statement and the Flow is waiting for it.
	PhaseRunning = "running"
	// PhaseCompleted means the statement finished and the usage summary is recorded.
	PhaseCompleted = "completed"
	// PhaseFailed means the statement failed, was canceled, or exceeded statementTimeout in Snowflake.
	PhaseFailed = "failed"
	// PhaseRejected means Snowflake refused the submission, so nothing ran.
	PhaseRejected = "rejected"
	// PhaseWaitBudgetExhausted means the statement outlasted the Flow's status reads and was canceled.
	PhaseWaitBudgetExhausted = "waitBudgetExhausted"
)

var (
	usagePhaseAttribute  = dex.DefineAttribute[string]("snowflake-usage-phase")
	usageRecordAttribute = dex.DefineAttribute[UsageSummaryRecord]("snowflake-usage-record")
)

// Input is the account and start date entered in Dex Web Start Flow.
type Input struct {
	// AccountID identifies the customer account in USAGE_EVENTS.ACCOUNT_ID.
	AccountID string `json:"accountId"`
	// Since is the first day to count, as YYYY-MM-DD.
	Since string `json:"since"`
}

// StatementCheck identifies the statement the Flow reads or cancels next.
type StatementCheck struct {
	// StatementHandle is the handle Snowflake returned for the submitted statement.
	StatementHandle string `json:"statementHandle"`
}

// AccountUsage is the one row the usage aggregate returns.
type AccountUsage struct {
	// EventCount is COUNT(*), a NUMBER(18,0) returned as an exact decimal string.
	EventCount string `json:"eventCount"`
	// CreditsUsed is SUM(CREDITS_USED) as an exact decimal string.
	CreditsUsed string `json:"creditsUsed"`
	// LastEventAt is the latest EVENT_AT as ISO 8601 without an offset, or nil when no event matched.
	LastEventAt *string `json:"lastEventAt,omitempty"`
}

// UsageSummaryRecord is the Flow's durable record and its result.
type UsageSummaryRecord struct {
	// Request is the validated Start Flow input.
	Request Input `json:"request"`
	// Phase mirrors the snowflake-usage-phase Attribute.
	Phase string `json:"phase"`
	// StatementHandle is the Snowflake statement handle once submitted.
	StatementHandle string `json:"statementHandle,omitempty"`
	// StatusReads counts getStatementResult reads that found the statement still running.
	StatusReads int `json:"statusReads"`
	// Usage is the aggregate once the statement completed.
	Usage *AccountUsage `json:"usage,omitempty"`
	// SnowflakeCode is Snowflake's six-digit failure code, never its message text.
	SnowflakeCode string `json:"snowflakeCode,omitempty"`
	// SQLState is the failure's SQLSTATE.
	SQLState string `json:"sqlState,omitempty"`
	// FailureKind is the connector's safe failure category.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// IsCancellationAccepted reports whether Snowflake accepted the cancel after the wait budget ran out.
	IsCancellationAccepted bool `json:"isCancellationAccepted,omitempty"`
}

// StatusPolicy bounds how long the Flow waits for the statement.
type StatusPolicy struct {
	// CheckInterval is the durable Timer before each status read; at least one second.
	CheckInterval time.Duration
	// MaximumRunningReads is the number of running reads before the Flow cancels the statement.
	MaximumRunningReads int
}

// DefaultStatusPolicy reads the status every 15 seconds for ten minutes.
func DefaultStatusPolicy() StatusPolicy {
	return StatusPolicy{CheckInterval: 15 * time.Second, MaximumRunningReads: 40}
}

// Flow summarizes one account's usage with a long Snowflake aggregate.
type Flow struct {
	dex.FlowDefaults
	connection snowflake.Connection
	policy     StatusPolicy
}

// NewFlow binds the Snowflake Connection and status policy at registration time.
// It panics when policy is nil, its interval is below one second, or it allows no reads.
func NewFlow(connection snowflake.Connection, policy *StatusPolicy) *Flow {
	if policy == nil || policy.CheckInterval < time.Second || policy.MaximumRunningReads < 1 {
		panic("account usage summary requires a status interval of at least one second and at least one read")
	}
	return &Flow{connection: connection, policy: *policy}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, submit, wait, read, cancel, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordUsageRequest{}),
		dex.DefineStep(snowflake.NewSubmitStatementStep(snowflake.SubmitStatementStepConfig[Input]{
			StepType: submitUsageQueryStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "snowflake", GroupLabel: "Snowflake",
				Explanation: "Submit the aggregate asynchronously; a duplicate dispatch returns the same statement.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToSubmitUsageQueryInput,
			Submitted:           sdkgo.GoTo(recordSubmittedQuery{}),
			ProviderRejected:    sdkgo.GoTo(recordRejectedQuery{}),
		})),
		dex.DefineStep(recordSubmittedQuery{}),
		dex.DefineStep(recordRejectedQuery{}),
		dex.DefineStep(waitForUsageQuery{checkInterval: flow.policy.CheckInterval}),
		dex.DefineStep(snowflake.NewGetStatementResultStep(snowflake.GetStatementResultStepConfig[StatementCheck]{
			StepType: readUsageResultStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "snowflake", GroupLabel: "Snowflake",
				Explanation: "Read the statement once: still running, finished with rows, or failed.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToReadUsageResultInput,
			Completed:           sdkgo.GoTo(completeUsageSummary{}),
			Running:             sdkgo.GoTo(recordRunningQuery{maximumRunningReads: flow.policy.MaximumRunningReads}),
			ProviderRejected:    sdkgo.GoTo(recordFailedQuery{}),
		})),
		dex.DefineStep(recordRunningQuery{maximumRunningReads: flow.policy.MaximumRunningReads}),
		dex.DefineStep(completeUsageSummary{}),
		dex.DefineStep(recordFailedQuery{}),
		dex.DefineStep(snowflake.NewCancelStatementStep(snowflake.CancelStatementStepConfig[StatementCheck]{
			StepType: cancelUsageQueryStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "snowflake", GroupLabel: "Snowflake",
				Explanation: "Cancel the statement after the last allowed status read.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToCancelUsageQueryInput,
			Canceled:            sdkgo.GoTo(recordWaitBudgetExhausted{}),
			ProviderRejected:    sdkgo.GoTo(recordWaitBudgetExhausted{}),
		})),
		dex.DefineStep(recordWaitBudgetExhausted{}),
	}
}

// GetRPCs returns the record read RPC and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetUsageSummaryRecord, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and record Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{usagePhaseAttribute, usageRecordAttribute}}
}

// MapToSubmitUsageQueryInput binds the account and start date; neither is formatted into SQL.
func (*Flow) MapToSubmitUsageQueryInput(input Input) snowflake.SubmitStatementInput {
	return snowflake.SubmitStatementInput{Statement: UsageSummaryStatement, Parameters: []any{input.AccountID, input.Since}}
}

// MapToReadUsageResultInput reads partition 0, which carries the one aggregate row.
func (*Flow) MapToReadUsageResultInput(check StatementCheck) snowflake.GetStatementResultInput {
	return snowflake.GetStatementResultInput{StatementHandle: check.StatementHandle}
}

// MapToCancelUsageQueryInput cancels the submitted statement.
func (*Flow) MapToCancelUsageQueryInput(check StatementCheck) snowflake.CancelStatementInput {
	return snowflake.CancelStatementInput{StatementHandle: check.StatementHandle}
}

// GetUsageSummaryRecord returns the current record.
func (*Flow) GetUsageSummaryRecord(ctx dex.Context, _ dex.None) (*dex.RPCResult[UsageSummaryRecord], error) {
	record, err := optionalAttribute(ctx, usageRecordAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[UsageSummaryRecord]{Output: record}, nil
}

// GetDexSummary returns the phase and record for Dex Web lists.
//
// dex:field attribute-key:snowflake-usage-phase value-type:string editable:false description:"Statement phase"
// dex:field attribute-key:snowflake-usage-record value-type:json editable:false description:"Account, statement handle, status reads, and usage"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, usagePhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, usageRecordAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"snowflake-usage-phase":  phase,
		"snowflake-usage-record": record,
	}}, nil
}

// GetDexDisplay returns the phase and record for the Dex Web run view.
//
// dex:field attribute-key:snowflake-usage-phase value-type:string editable:false description:"Statement phase" ui-slot:status
// dex:field attribute-key:snowflake-usage-record value-type:json editable:false description:"Request, Snowflake statement handle, status reads, usage, and any Snowflake code and SQLSTATE"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, usagePhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, usageRecordAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"snowflake-usage-phase":  phase,
		"snowflake-usage-record": record,
	}}, nil
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

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate and record the account and start date before calling Snowflake."
type recordUsageRequest struct {
	dex.StepDefaults
}

func (recordUsageRequest) GetStepType() string { return recordUsageRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordUsageRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordUsageRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.Since = strings.TrimSpace(input.Since)
	if input.AccountID == "" || len(input.AccountID) > 100 {
		return dex.ForceFail("accountId is required and must be at most 100 bytes"), nil
	}
	if _, err := time.Parse(time.DateOnly, input.Since); err != nil {
		return dex.ForceFail("since must be a date such as 2026-01-01"), nil
	}
	record := UsageSummaryRecord{Request: input, Phase: PhaseSubmitting}
	if err := usagePhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](submitUsageQueryStepType), input), nil
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Record the statement handle and schedule the first status read."
type recordSubmittedQuery struct {
	dex.StepDefaultsNoWaitFor[snowflake.SubmitStatementResult]
}

func (recordSubmittedQuery) GetStepType() string { return recordSubmittedQueryStepType }

func (recordSubmittedQuery) Execute(ctx dex.Context, result snowflake.SubmitStatementResult) (*dex.StepDecision, error) {
	record, err := usageRecordAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.StatementHandle = PhaseRunning, result.Value.StatementHandle
	if err := usagePhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(waitForUsageQuery{}, StatementCheck{StatementHandle: result.Value.StatementHandle}), nil
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Complete with Snowflake's code and SQLSTATE after a refused submission; nothing ran."
type recordRejectedQuery struct {
	dex.StepDefaultsNoWaitFor[snowflake.SubmitStatementResult]
}

func (recordRejectedQuery) GetStepType() string { return recordRejectedQueryStepType }

func (recordRejectedQuery) Execute(ctx dex.Context, result snowflake.SubmitStatementResult) (*dex.StepDecision, error) {
	record, err := usageRecordAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseRejected
	recordFailure(&record, result.Receipt, result.Failure)
	if err := usagePhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:wait group-label:"Wait"
// dex:explanation text:"Wait on a durable Timer before reading the statement again; no Worker is held."
type waitForUsageQuery struct {
	dex.StepDefaults
	checkInterval time.Duration
}

func (waitForUsageQuery) GetStepType() string { return waitForUsageQueryStepType }

func (step waitForUsageQuery) WaitFor(dex.Context, StatementCheck) (*dex.Wait, error) {
	return dex.Until(dex.Timer(step.checkInterval)), nil
}

func (waitForUsageQuery) Execute(_ dex.Context, check StatementCheck) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[StatementCheck](readUsageResultStepType), check), nil
}

// dex:group group-id:wait group-label:"Wait"
// dex:explanation text:"Count the running read, then wait again or cancel after the last allowed read."
type recordRunningQuery struct {
	dex.StepDefaultsNoWaitFor[snowflake.GetStatementResultResult]
	maximumRunningReads int
}

func (recordRunningQuery) GetStepType() string { return recordRunningQueryStepType }

func (step recordRunningQuery) Execute(ctx dex.Context, result snowflake.GetStatementResultResult) (*dex.StepDecision, error) {
	record, err := usageRecordAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.StatusReads++
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	check := StatementCheck{StatementHandle: result.Value.StatementHandle}
	if record.StatusReads >= step.maximumRunningReads {
		return dex.GoTo(sdkgo.StepRef[StatementCheck](cancelUsageQueryStepType), check), nil
	}
	return dex.GoTo(waitForUsageQuery{}, check), nil
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Decode the one aggregate row and complete with the account's usage."
type completeUsageSummary struct {
	dex.StepDefaultsNoWaitFor[snowflake.GetStatementResultResult]
}

func (completeUsageSummary) GetStepType() string { return completeUsageSummaryStepType }

func (completeUsageSummary) Execute(ctx dex.Context, result snowflake.GetStatementResultResult) (*dex.StepDecision, error) {
	if len(result.Value.Rows) != 1 {
		return dex.ForceFail(fmt.Sprintf("the usage aggregate returned %d rows instead of one", len(result.Value.Rows))), nil
	}
	usage, err := decodeAccountUsage(result.Value.Rows[0])
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record, err := usageRecordAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Usage = PhaseCompleted, &usage
	if err := usagePhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Complete with Snowflake's code and SQLSTATE after the statement failed or was canceled."
type recordFailedQuery struct {
	dex.StepDefaultsNoWaitFor[snowflake.GetStatementResultResult]
}

func (recordFailedQuery) GetStepType() string { return recordFailedQueryStepType }

func (recordFailedQuery) Execute(ctx dex.Context, result snowflake.GetStatementResultResult) (*dex.StepDecision, error) {
	record, err := usageRecordAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseFailed
	recordFailure(&record, result.Receipt, result.Failure)
	if err := usagePhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:wait group-label:"Wait"
// dex:explanation text:"Complete as waitBudgetExhausted, recording whether Snowflake accepted the cancel."
type recordWaitBudgetExhausted struct {
	dex.StepDefaultsNoWaitFor[snowflake.CancelStatementResult]
}

func (recordWaitBudgetExhausted) GetStepType() string { return recordWaitBudgetExhaustedStepType }

// Execute completes either way; a rejected cancel usually means the statement finished meanwhile.
func (recordWaitBudgetExhausted) Execute(ctx dex.Context, result snowflake.CancelStatementResult) (*dex.StepDecision, error) {
	record, err := usageRecordAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseWaitBudgetExhausted
	record.IsCancellationAccepted = result.Branch == snowflake.CancelStatementBranchCanceled
	if !record.IsCancellationAccepted {
		recordFailure(&record, result.Receipt, result.Failure)
	}
	if err := usagePhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := usageRecordAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// recordFailure copies only Snowflake's code, SQLSTATE, and the safe failure kind into the record.
func recordFailure(record *UsageSummaryRecord, receipt sdkgo.Receipt, failure *sdkgo.Failure) {
	record.SnowflakeCode, record.SQLState = receipt.Metadata["code"], receipt.Metadata["sqlState"]
	if failure != nil {
		record.FailureKind = failure.Kind
	}
}

// decodeAccountUsage reads the aggregate row; a small NUMBER precision would arrive as a JSON number.
func decodeAccountUsage(row map[string]any) (AccountUsage, error) {
	eventCount, err := decimalText(row["EVENT_COUNT"])
	if err != nil {
		return AccountUsage{}, fmt.Errorf("EVENT_COUNT: %w", err)
	}
	creditsUsed, err := decimalText(row["CREDITS_USED"])
	if err != nil {
		return AccountUsage{}, fmt.Errorf("CREDITS_USED: %w", err)
	}
	usage := AccountUsage{EventCount: eventCount, CreditsUsed: creditsUsed}
	switch lastEventAt := row["LAST_EVENT_AT"].(type) {
	case nil:
	case string:
		usage.LastEventAt = &lastEventAt
	default:
		return AccountUsage{}, fmt.Errorf("LAST_EVENT_AT is not a timestamp string")
	}
	return usage, nil
}

func decimalText(value any) (string, error) {
	switch number := value.(type) {
	case string:
		return number, nil
	case json.Number:
		return number.String(), nil
	case int64:
		return fmt.Sprint(number), nil
	case float64:
		if number != float64(int64(number)) {
			return "", fmt.Errorf("is not an exact integer")
		}
		return fmt.Sprint(int64(number)), nil
	default:
		return "", fmt.Errorf("is not a number")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, UsageSummaryRecord] = (*Flow)(nil).GetUsageSummaryRecord
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
