// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package refunddecision demonstrates every Airtable operation in one Flow
// started from Dex Web Start Flow. It reads the refund policy row whose key
// matches the request, decides the refund against the policy's approval
// limit, upserts the case's decision log row keyed by case ID with a link to
// the policy, stamps the policy with the case it last decided, and reads the
// log row back to confirm the stored decision.
package refunddecision

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "AirtableRefundDecision"
	// ConnectionName is the static Dex Web connection for Airtable.
	ConnectionName = "airtable-refund-policies"

	// PolicyKeyField is the policy table's text field that names each policy.
	PolicyKeyField = "Policy Key"
	// ApprovalLimitField is the policy table's number field holding the largest automatically approved amount.
	ApprovalLimitField = "Approval Limit USD"
	// LastDecidedCaseField is the policy table's text field stamped with the last case the policy decided.
	LastDecidedCaseField = "Last Decided Case"
	// CaseIDField is the decision log's text field that identifies a case; the upsert merges on it.
	CaseIDField = "Case ID"
	// CustomerField is the decision log's text field naming the customer.
	CustomerField = "Customer"
	// AmountField is the decision log's number field holding the requested refund.
	AmountField = "Amount USD"
	// DecisionField is the decision log's text field holding approved, escalated, or needsReview.
	DecisionField = "Decision"
	// PolicyLinkField is the decision log's linked record field pointing at the policy table.
	PolicyLinkField = "Policy"

	recordRefundRequestStepType        = "RecordAirtableRefundRequest"
	findRefundPolicyStepType           = "FindAirtableRefundPolicy"
	decideRefundStepType               = "DecideAirtableRefund"
	upsertDecisionLogStepType          = "UpsertAirtableRefundDecisionLog"
	recordDecisionLogStepType          = "RecordAirtableRefundDecisionLog"
	stampRefundPolicyStepType          = "StampAirtableRefundPolicy"
	recordPolicyStampStepType          = "RecordAirtableRefundPolicyStamp"
	readBackDecisionLogStepType        = "ReadBackAirtableRefundDecisionLog"
	completeRefundDecisionStepType     = "CompleteAirtableRefundDecision"
	maximumTextInputRunes              = 200
	maximumRefundAmountUSD             = 1_000_000_000
	policyPageSizeThatRevealsAmbiguity = 2
)

var decisionAttribute = dex.DefineAttribute[RefundDecision]("airtable-refund-decision")

// Decision is the outcome recorded for one refund case.
type Decision string

const (
	// DecisionApproved means the amount is within the matched policy's approval limit.
	DecisionApproved Decision = "approved"
	// DecisionEscalated means the amount exceeds the matched policy's approval limit.
	DecisionEscalated Decision = "escalated"
	// DecisionNeedsReview means no single policy with an approval limit matched the policy key.
	DecisionNeedsReview Decision = "needsReview"
)

// Phase is the durable progress of one refund decision.
type Phase string

const (
	// PhaseRecorded means the request was validated and recorded.
	PhaseRecorded Phase = "recorded"
	// PhaseDecided means the policy was read and the decision made.
	PhaseDecided Phase = "decided"
	// PhaseLogged means Airtable holds the case's decision log row.
	PhaseLogged Phase = "logged"
	// PhaseCompleted means the read-back confirmed the stored decision.
	PhaseCompleted Phase = "completed"
)

// Input contains the refund case entered in Dex Web Start Flow.
type Input struct {
	// CaseID identifies the refund case; a later Flow for the same case updates its log row.
	CaseID string `json:"caseId"`
	// Customer names the customer.
	Customer string `json:"customer"`
	// AmountUSD is the requested refund in US dollars.
	AmountUSD float64 `json:"amountUsd"`
	// PolicyKey selects the policy row whose Policy Key equals it.
	PolicyKey string `json:"policyKey"`
}

// RefundDecision is the Flow's durable state and completion output.
type RefundDecision struct {
	// Request is the validated Start Flow input.
	Request Input `json:"request"`
	// Phase is the current or terminal progress.
	Phase Phase `json:"phase"`
	// MatchingPolicyCount is how many policy rows matched, capped at two.
	MatchingPolicyCount int `json:"matchingPolicyCount"`
	// PolicyRecordID is the matched policy row, blank unless exactly one matched.
	PolicyRecordID string `json:"policyRecordId,omitempty"`
	// ApprovalLimitUSD is the matched policy's approval limit.
	ApprovalLimitUSD float64 `json:"approvalLimitUsd,omitempty"`
	// Decision is the recorded outcome.
	Decision Decision `json:"decision,omitempty"`
	// Reason explains a needsReview decision.
	Reason string `json:"reason,omitempty"`
	// LogRecordID is the decision log row of the case.
	LogRecordID string `json:"logRecordId,omitempty"`
	// IsLogRecordCreated reports whether the answered upsert created the log row.
	IsLogRecordCreated bool `json:"logRecordCreated"`
	// IsPolicyStamped reports whether the matched policy now names this case.
	IsPolicyStamped bool `json:"policyStamped"`
}

// TableSelection is one base and table pick Dex Web saves for an Airtable Step.
type TableSelection struct {
	// BaseID is the base ID, such as appXXXXXXXXXXXXXX.
	BaseID string `json:"baseId"`
	// BaseName is the base's display name.
	BaseName string `json:"baseName,omitempty"`
	// TableID is the table ID, such as tblXXXXXXXXXXXXXX.
	TableID string `json:"tableId"`
	// TableName is the table's display name.
	TableName string `json:"tableName,omitempty"`
}

func (selection TableSelection) isPicked() bool {
	return strings.TrimSpace(selection.BaseID) != "" && strings.TrimSpace(selection.TableID) != ""
}

// Settings holds the policy and decision log tables loaded once at startup.
type Settings struct {
	// PolicyTable is the table read for policies and stamped with decided cases.
	PolicyTable TableSelection
	// LogTable is the decision log table; it must be in the policy table's base, since links stay within a base.
	LogTable TableSelection
}

// Flow decides one refund case against an Airtable policy table and logs it.
type Flow struct {
	dex.FlowDefaults
	connection airtable.Connection
	settings   Settings
}

// NewFlow binds the Airtable Connection and startup settings. It fails when a
// table is not picked or the two tables are in different bases.
func NewFlow(connection airtable.Connection, settings *Settings) (*Flow, error) {
	if settings == nil {
		return nil, errors.New("Airtable refund decision settings are required")
	}
	if !settings.PolicyTable.isPicked() || !settings.LogTable.isPicked() {
		return nil, errors.New("Airtable refund decision needs the policy and decision log bases and tables saved in Dex Web")
	}
	if settings.PolicyTable.BaseID != settings.LogTable.BaseID {
		return nil, errors.New("the decision log table must be in the policy table's base, because Airtable links records only within one base")
	}
	return &Flow{connection: connection, settings: *settings}, nil
}

// PolicyTableConfigurationRef identifies the policy base and table pick of the list Step.
func PolicyTableConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: airtable.ConnectorID, ConnectionName: ConnectionName, OperationID: "listRecords",
		FlowType: FlowType, StepType: findRefundPolicyStepType,
	}
}

// LogTableConfigurationRef identifies the decision log base and table pick of the upsert Step.
func LogTableConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: airtable.ConnectorID, ConnectionName: ConnectionName, OperationID: "upsertRecords",
		FlowType: FlowType, StepType: upsertDecisionLogStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Airtable Connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRefundRequest{}),
		dex.DefineStep(airtable.NewListRecordsStep(airtable.ListRecordsStepConfig[RefundDecision]{
			StepType: findRefundPolicyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "airtable", GroupLabel: "Airtable",
				Explanation: "List up to two policy rows whose Policy Key equals the request's key.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "policyBase", UnitID: airtable.UIUnitBasePicker, Label: "Policy base", Required: true,
					Description: "Select the base that holds the refund policy table; the picker stores its base ID and name, and the Worker does not start until a base is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UIBasePickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UIBasePickerPortBaseName, JSONPointer: "/baseName"},
					},
				},
				{
					ID: "policyTable", UnitID: airtable.UIUnitTablePicker, Label: "Policy table", Required: true,
					Description: "Select the refund policy table, which has a Policy Key text field, an Approval Limit USD number field, and a Last Decided Case text field; the picker stores its table ID and name, and the Worker does not start until a table is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UITablePickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UITablePickerPortTableID, JSONPointer: "/tableId"},
						{Port: airtable.UITablePickerPortTableName, JSONPointer: "/tableName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToFindRefundPolicyInput,
			Listed: sdkgo.GoTo(decideRefund{}),
		})),
		dex.DefineStep(decideRefund{}),
		dex.DefineStep(airtable.NewUpsertRecordsStep(airtable.UpsertRecordsStepConfig[RefundDecision]{
			StepType: upsertDecisionLogStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "airtable", GroupLabel: "Airtable",
				Explanation: "Create or update the case's decision log row, merged on Case ID, linked to the policy.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "logBase", UnitID: airtable.UIUnitBasePicker, Label: "Decision log base", Required: true,
					Description: "Select the base that holds the decision log table, the same base as the policy table because Airtable links records only within one base; the picker stores its base ID and name, and the Worker does not start until a base is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UIBasePickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UIBasePickerPortBaseName, JSONPointer: "/baseName"},
					},
				},
				{
					ID: "logTable", UnitID: airtable.UIUnitTablePicker, Label: "Decision log table", Required: true,
					Description: "Select the decision log table, which has Case ID, Customer, and Decision text fields, an Amount USD number field, and a Policy field linked to the policy table; the picker stores its table ID and name, and the Worker does not start until a table is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UITablePickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UITablePickerPortTableID, JSONPointer: "/tableId"},
						{Port: airtable.UITablePickerPortTableName, JSONPointer: "/tableName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpsertDecisionLogInput,
			Upserted: sdkgo.GoTo(recordDecisionLog{}),
		})),
		dex.DefineStep(recordDecisionLog{}),
		dex.DefineStep(airtable.NewUpdateRecordsStep(airtable.UpdateRecordsStepConfig[RefundDecision]{
			StepType: stampRefundPolicyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "airtable", GroupLabel: "Airtable",
				Explanation: "Set the matched policy's Last Decided Case to this case.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToStampRefundPolicyInput,
			Updated: sdkgo.GoTo(recordPolicyStamp{}),
		})),
		dex.DefineStep(recordPolicyStamp{}),
		dex.DefineStep(airtable.NewGetRecordStep(airtable.GetRecordStepConfig[RefundDecision]{
			StepType: readBackDecisionLogStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "airtable", GroupLabel: "Airtable",
				Explanation: "Read the decision log row back to confirm Airtable stored the decision.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadBackDecisionLogInput,
			Found: sdkgo.GoTo(completeRefundDecision{}),
		})),
		dex.DefineStep(completeRefundDecision{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the refund decision Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{decisionAttribute}}
}

// GetDexSummary returns the refund decision state.
//
// dex:field attribute-key:airtable-refund-decision value-type:json editable:false description:"Case, policy match, decision, and phase"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	decision, err := optionalDecision(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"airtable-refund-decision": decision}}, nil
}

// GetDexDisplay returns the refund decision state.
//
// dex:field attribute-key:airtable-refund-decision value-type:json editable:false description:"Case ID, decision, Airtable policy and log record IDs, and phase"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	decision, err := optionalDecision(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"airtable-refund-decision": decision}}, nil
}

// MapToFindRefundPolicyInput lists up to two policies with the request's key, so a duplicate key is visible.
func (flow *Flow) MapToFindRefundPolicyInput(decision RefundDecision) airtable.ListRecordsInput {
	return airtable.ListRecordsInput{
		BaseID: flow.settings.PolicyTable.BaseID, TableIDOrName: flow.settings.PolicyTable.TableID,
		Filters:  []airtable.FieldEqualityFilter{airtable.TextFieldEquals(PolicyKeyField, decision.Request.PolicyKey)},
		Fields:   []string{PolicyKeyField, ApprovalLimitField},
		PageSize: policyPageSizeThatRevealsAmbiguity,
	}
}

// MapToUpsertDecisionLogInput writes the case's log row, merged on Case ID, linking the matched policy or none.
func (flow *Flow) MapToUpsertDecisionLogInput(decision RefundDecision) airtable.UpsertRecordsInput {
	policyLinks := airtable.LinkedRecordsCellValue()
	if decision.PolicyRecordID != "" {
		policyLinks = airtable.LinkedRecordsCellValue(decision.PolicyRecordID)
	}
	return airtable.UpsertRecordsInput{
		BaseID: flow.settings.LogTable.BaseID, TableIDOrName: flow.settings.LogTable.TableID,
		FieldsToMergeOn: []string{CaseIDField},
		Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{
			CaseIDField:     airtable.TextCellValue(decision.Request.CaseID),
			CustomerField:   airtable.TextCellValue(decision.Request.Customer),
			AmountField:     airtable.NumberCellValue(decision.Request.AmountUSD),
			DecisionField:   airtable.TextCellValue(string(decision.Decision)),
			PolicyLinkField: policyLinks,
		}}},
	}
}

// MapToStampRefundPolicyInput sets the matched policy's Last Decided Case to this case.
func (flow *Flow) MapToStampRefundPolicyInput(decision RefundDecision) airtable.UpdateRecordsInput {
	return airtable.UpdateRecordsInput{
		BaseID: flow.settings.PolicyTable.BaseID, TableIDOrName: flow.settings.PolicyTable.TableID,
		Records: []airtable.RecordUpdate{{
			ID: decision.PolicyRecordID, Fields: map[string]airtable.CellValue{LastDecidedCaseField: airtable.TextCellValue(decision.Request.CaseID)},
		}},
	}
}

// MapToReadBackDecisionLogInput reads the case's log row after the upsert.
func (flow *Flow) MapToReadBackDecisionLogInput(decision RefundDecision) airtable.GetRecordInput {
	return airtable.GetRecordInput{
		BaseID: flow.settings.LogTable.BaseID, TableIDOrName: flow.settings.LogTable.TableID, RecordID: decision.LogRecordID,
	}
}

func optionalDecision(ctx dex.Context) (RefundDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return RefundDecision{}, nil
	}
	return decision, err
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Validate and record the refund case before calling Airtable."
type recordRefundRequest struct {
	dex.StepDefaults
}

func (recordRefundRequest) GetStepType() string { return recordRefundRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordRefundRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordRefundRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := validateRefundRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	decision := RefundDecision{Request: request, Phase: PhaseRecorded}
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[RefundDecision](findRefundPolicyStepType), decision), nil
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Decide the refund against the one matching policy, or mark it for review."
type decideRefund struct {
	dex.StepDefaultsNoWaitFor[airtable.ListRecordsResult]
}

func (decideRefund) GetStepType() string { return decideRefundStepType }

func (decideRefund) Execute(ctx dex.Context, result airtable.ListRecordsResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	decision = decideAgainstPolicies(decision, result.Value.Records)
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[RefundDecision](upsertDecisionLogStepType), decision), nil
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Record the log row, then stamp the matched policy or read the row back."
type recordDecisionLog struct {
	dex.StepDefaultsNoWaitFor[airtable.UpsertRecordsResult]
}

func (recordDecisionLog) GetStepType() string { return recordDecisionLogStepType }

func (recordDecisionLog) Execute(ctx dex.Context, result airtable.UpsertRecordsResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Records) != 1 {
		return dex.ForceFail(fmt.Sprintf("the Airtable upsert returned %d log rows instead of one", len(result.Value.Records))), nil
	}
	decision.Phase = PhaseLogged
	decision.LogRecordID = result.Value.Records[0].ID
	decision.IsLogRecordCreated = len(result.Value.CreatedRecordIDs) == 1 && result.Value.CreatedRecordIDs[0] == decision.LogRecordID
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	if decision.PolicyRecordID == "" {
		return dex.GoTo(sdkgo.StepRef[RefundDecision](readBackDecisionLogStepType), decision), nil
	}
	return dex.GoTo(sdkgo.StepRef[RefundDecision](stampRefundPolicyStepType), decision), nil
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Record the policy stamp and read the log row back."
type recordPolicyStamp struct {
	dex.StepDefaultsNoWaitFor[airtable.UpdateRecordsResult]
}

func (recordPolicyStamp) GetStepType() string { return recordPolicyStampStepType }

func (recordPolicyStamp) Execute(ctx dex.Context, result airtable.UpdateRecordsResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	stampedCase := ""
	if len(result.Value.Records) == 1 {
		stampedCase, _ = result.Value.Records[0].Fields[LastDecidedCaseField].Text()
	}
	if stampedCase != decision.Request.CaseID {
		return dex.ForceFail("the Airtable policy update did not store this case in Last Decided Case"), nil
	}
	decision.IsPolicyStamped = true
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[RefundDecision](readBackDecisionLogStepType), decision), nil
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Confirm the read-back log row holds this decision and complete."
type completeRefundDecision struct {
	dex.StepDefaultsNoWaitFor[airtable.GetRecordResult]
}

func (completeRefundDecision) GetStepType() string { return completeRefundDecisionStepType }

func (completeRefundDecision) Execute(ctx dex.Context, result airtable.GetRecordResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := confirmStoredDecision(decision, result.Value); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	decision.Phase = PhaseCompleted
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(decision), nil
}

// decideAgainstPolicies approves within the one matching policy's limit and marks every other match count for review.
func decideAgainstPolicies(decision RefundDecision, policies []airtable.Record) RefundDecision {
	decision.Phase = PhaseDecided
	decision.MatchingPolicyCount = len(policies)
	switch len(policies) {
	case 0:
		decision.Decision, decision.Reason = DecisionNeedsReview, "no policy has this Policy Key"
		return decision
	case 1:
	default:
		decision.Decision, decision.Reason = DecisionNeedsReview, "several policies have this Policy Key"
		return decision
	}
	limit, hasLimit := policies[0].Fields[ApprovalLimitField].Number()
	if !hasLimit {
		decision.Decision, decision.Reason = DecisionNeedsReview, "the policy has no Approval Limit USD"
		return decision
	}
	decision.PolicyRecordID = policies[0].ID
	decision.ApprovalLimitUSD = limit
	decision.Decision = DecisionEscalated
	if decision.Request.AmountUSD <= limit {
		decision.Decision = DecisionApproved
	}
	return decision
}

// confirmStoredDecision checks the read-back log row against the durable decision.
func confirmStoredDecision(decision RefundDecision, logRecord airtable.Record) error {
	storedCaseID, _ := logRecord.Fields[CaseIDField].Text()
	storedDecision, _ := logRecord.Fields[DecisionField].Text()
	storedPolicies, _ := logRecord.Fields[PolicyLinkField].StringList()
	if storedCaseID != decision.Request.CaseID || storedDecision != string(decision.Decision) {
		return fmt.Errorf("Airtable log row %s reads back case %q and decision %q, not this decision", logRecord.ID, storedCaseID, storedDecision)
	}
	expectedPolicies := []string{}
	if decision.PolicyRecordID != "" {
		expectedPolicies = []string{decision.PolicyRecordID}
	}
	if strings.Join(storedPolicies, ",") != strings.Join(expectedPolicies, ",") {
		return fmt.Errorf("Airtable log row %s links policies %v, not %v", logRecord.ID, storedPolicies, expectedPolicies)
	}
	return nil
}

func validateRefundRequest(input Input) (Input, error) {
	request := Input{
		CaseID: strings.TrimSpace(input.CaseID), Customer: strings.TrimSpace(input.Customer),
		AmountUSD: input.AmountUSD, PolicyKey: strings.TrimSpace(input.PolicyKey),
	}
	if request.CaseID == "" || request.PolicyKey == "" {
		return Input{}, errors.New("caseId and policyKey are required")
	}
	for _, value := range []string{request.CaseID, request.Customer, request.PolicyKey} {
		if len([]rune(value)) > maximumTextInputRunes || strings.ContainsFunc(value, isUnsupportedTextRune) {
			return Input{}, fmt.Errorf("caseId, customer, and policyKey are limited to %d characters without backslashes or control characters", maximumTextInputRunes)
		}
	}
	if math.IsNaN(request.AmountUSD) || request.AmountUSD < 0 || request.AmountUSD > maximumRefundAmountUSD {
		return Input{}, fmt.Errorf("amountUsd must be between 0 and %d", maximumRefundAmountUSD)
	}
	return request, nil
}

// isUnsupportedTextRune reports a rune that a typed Airtable text filter cannot carry.
func isUnsupportedTextRune(character rune) bool {
	return character == '\\' || unicode.IsControl(character)
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
