// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package approvaldecision demonstrates every Microsoft Excel operation in one
// Flow started from Dex Web Start Flow. It reads an approval-policy table,
// decides one spending request against the policy row for its category,
// appends the decision to a decision log table keyed by request ID, writes the
// decision to a summary range, and reads the range back to confirm it.
package approvaldecision

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "MicrosoftExcelApprovalDecision"
	// ConnectionName is the static Dex Web connection for Microsoft Excel.
	ConnectionName = "microsoft-excel-approvals"

	// PolicyCategoryColumn is the policy table's text column naming each spending category.
	PolicyCategoryColumn = "Category"
	// PolicyLimitColumn is the policy table's number column holding the largest automatically approved amount in US dollars.
	PolicyLimitColumn = "MaxAutoApproveUsd"
	// PolicyApproverColumn is the policy table's text column naming who approves a larger amount.
	PolicyApproverColumn = "Approver"
	// DecisionRequestIDColumn is the decision log's key column; a request is logged at most once.
	DecisionRequestIDColumn = "RequestId"
	// DecisionRequesterColumn is the decision log's column for the requester.
	DecisionRequesterColumn = "Requester"
	// DecisionCategoryColumn is the decision log's column for the category.
	DecisionCategoryColumn = "Category"
	// DecisionAmountColumn is the decision log's number column for the amount in US dollars.
	DecisionAmountColumn = "AmountUsd"
	// DecisionColumn is the decision log's column for approved, escalated, or needsReview.
	DecisionColumn = "Decision"
	// DecisionApproverColumn is the decision log's column for the approver of an escalated request.
	DecisionApproverColumn = "Approver"
	// DecisionTimeColumn is the decision log's column for the decision time in RFC 3339 form.
	DecisionTimeColumn = "DecidedAt"
	// SummaryAddress is the summary worksheet range that holds the latest decision.
	SummaryAddress = "A2:E2"

	recordRequestStepType     = "RecordExcelApprovalRequest"
	readPolicyStepType        = "ReadExcelApprovalPolicy"
	decideApprovalStepType    = "DecideExcelApproval"
	appendDecisionStepType    = "AppendExcelDecisionRow"
	recordDecisionLogStepType = "RecordExcelDecisionLogged"
	updateSummaryStepType     = "UpdateExcelDecisionSummary"
	readBackSummaryStepType   = "ReadBackExcelDecisionSummary"
	completeDecisionStepType  = "CompleteExcelApprovalDecision"
	reportUncertainStepType   = "ReportExcelDecisionUncertain"
	maximumTextInputRunes     = 200
	maximumAmountUSD          = 1_000_000_000
)

var decisionAttribute = dex.DefineAttribute[ApprovalDecision]("microsoft-excel-approval-decision")

// Input is the spending request entered in Dex Web Start Flow.
type Input struct {
	// RequestID identifies the request, such as REQ-2026-0042; the decision log holds it at most once.
	RequestID string `json:"requestId"`
	// Requester names the person who asked, such as dana@contoso.com.
	Requester string `json:"requester"`
	// Category selects the policy row whose Category equals it, ignoring case.
	Category string `json:"category"`
	// AmountUSD is the requested amount in US dollars.
	AmountUSD float64 `json:"amountUsd"`
}

// Decision is the outcome recorded for one request.
type Decision string

const (
	// DecisionApproved means the amount is within the matched policy's automatic approval limit.
	DecisionApproved Decision = "approved"
	// DecisionEscalated means the amount exceeds the limit, so the policy's approver decides.
	DecisionEscalated Decision = "escalated"
	// DecisionNeedsReview means no single policy row with a numeric limit matched the category.
	DecisionNeedsReview Decision = "needsReview"
)

// Phase is the durable progress of one decision.
type Phase string

const (
	// PhaseRecorded means the request was validated and recorded.
	PhaseRecorded Phase = "recorded"
	// PhaseDecided means the policy was read and the decision made.
	PhaseDecided Phase = "decided"
	// PhaseLogged means the decision log holds the request's row.
	PhaseLogged Phase = "logged"
	// PhaseCompleted means the summary range was written and read back.
	PhaseCompleted Phase = "completed"
	// PhaseLogUncertain means the append may or may not have reached the decision log; a person must check it.
	PhaseLogUncertain Phase = "logUncertain"
)

// ApprovalDecision is the Flow's durable state and completion output.
type ApprovalDecision struct {
	// Request is the validated Start Flow input.
	Request Input `json:"request"`
	// Phase is the current or terminal progress.
	Phase Phase `json:"phase"`
	// DecidedAt is the Flow start time in RFC 3339 form, so every retry logs the same time.
	DecidedAt string `json:"decidedAt"`
	// Decision is the recorded outcome.
	Decision Decision `json:"decision,omitempty"`
	// MaxAutoApproveUSD is the matched policy's limit.
	MaxAutoApproveUSD float64 `json:"maxAutoApproveUsd,omitempty"`
	// Approver is the matched policy's approver for an escalated request.
	Approver string `json:"approver,omitempty"`
	// Reason explains a needsReview decision.
	Reason string `json:"reason,omitempty"`
	// IsLogRowAppended reports that this Flow appended the decision log row.
	IsLogRowAppended bool `json:"logRowAppended"`
	// WasLogRowAlreadyPresent reports that the decision log already held the request, from this Flow's earlier attempt or another Flow.
	WasLogRowAlreadyPresent bool `json:"logRowAlreadyPresent"`
	// SummaryAddress is the summary range as Excel reported it after the write.
	SummaryAddress string `json:"summaryAddress,omitempty"`
	// SummaryText is the summary range's displayed text as read back.
	SummaryText []string `json:"summaryText,omitempty"`
	// IsSummaryConfirmed reports that the read-back shows this request and decision.
	IsSummaryConfirmed bool `json:"summaryConfirmed"`
	// ReviewDetail is the connector's safe failure message for a person to act on.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// TableConfiguration is the workbook and table a Step's workbookPicker and tablePicker units save in Dex Web.
type TableConfiguration struct {
	// DriveID is the workbook's Microsoft Graph drive ID.
	DriveID string `json:"driveId,omitempty"`
	// WorkbookID is the workbook's drive item ID.
	WorkbookID string `json:"workbookId,omitempty"`
	// WorkbookName is the workbook's file name when it was picked.
	WorkbookName string `json:"workbookName,omitempty"`
	// Table is the table's ID when it was picked from the list, or its typed name.
	Table string `json:"table,omitempty"`
	// TableName is the table's name.
	TableName string `json:"tableName,omitempty"`
}

// WorksheetConfiguration is the workbook and worksheet a Step's workbookPicker and worksheetPicker units save in Dex Web.
type WorksheetConfiguration struct {
	// DriveID is the workbook's Microsoft Graph drive ID.
	DriveID string `json:"driveId,omitempty"`
	// WorkbookID is the workbook's drive item ID.
	WorkbookID string `json:"workbookId,omitempty"`
	// WorkbookName is the workbook's file name when it was picked.
	WorkbookName string `json:"workbookName,omitempty"`
	// Worksheet is the worksheet's ID when it was picked from the list, or its typed name.
	Worksheet string `json:"worksheet,omitempty"`
	// WorksheetName is the worksheet's name.
	WorksheetName string `json:"worksheetName,omitempty"`
}

// Flow decides one spending request against an Excel approval policy.
type Flow struct {
	dex.FlowDefaults
	connection       excel.Connection
	policyTable      sdkgo.ConnectorLoadedConfiguration[TableConfiguration]
	decisionTable    sdkgo.ConnectorLoadedConfiguration[TableConfiguration]
	summaryWorksheet sdkgo.ConnectorLoadedConfiguration[WorksheetConfiguration]
}

// NewFlow binds the Excel Connection and the three picks loaded at startup. Every pick is required.
func NewFlow(
	connection excel.Connection,
	policyTable sdkgo.ConnectorLoadedConfiguration[TableConfiguration],
	decisionTable sdkgo.ConnectorLoadedConfiguration[TableConfiguration],
	summaryWorksheet sdkgo.ConnectorLoadedConfiguration[WorksheetConfiguration],
) *Flow {
	return &Flow{connection: connection, policyTable: policyTable, decisionTable: decisionTable, summaryWorksheet: summaryWorksheet}
}

// PolicyTableConfigurationRef identifies the policy table pick of the policy read Step.
func PolicyTableConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: excel.ConnectorID, ConnectionName: ConnectionName, OperationID: "getTableRows",
		FlowType: FlowType, StepType: readPolicyStepType,
	}
}

// DecisionTableConfigurationRef identifies the decision log table pick of the append Step.
func DecisionTableConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: excel.ConnectorID, ConnectionName: ConnectionName, OperationID: "appendTableRows",
		FlowType: FlowType, StepType: appendDecisionStepType,
	}
}

// SummaryWorksheetConfigurationRef identifies the summary worksheet pick of the summary update Step.
func SummaryWorksheetConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: excel.ConnectorID, ConnectionName: ConnectionName, OperationID: "updateValues",
		FlowType: FlowType, StepType: updateSummaryStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Excel, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordApprovalRequest{flow: flow}),
		dex.DefineStep(excel.NewGetTableRowsStep(excel.GetTableRowsStepConfig[ApprovalDecision]{
			StepType: readPolicyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "microsoft-excel", GroupLabel: "Microsoft Excel",
				Explanation: "Read every row of the approval-policy table, keyed by column name.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "policyWorkbook", UnitID: excel.UIUnitWorkbookPicker, Label: "Approval policy workbook", Required: true,
					Description: "Choose the .xlsx workbook in OneDrive or SharePoint that holds the approval-policy table; the picker stores its drive ID, item ID, and file name, and the Flow fails at its first Step until one is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: excel.UIWorkbookPickerPortDriveID, JSONPointer: "/driveId"},
						{Port: excel.UIWorkbookPickerPortWorkbookID, JSONPointer: "/workbookId"},
						{Port: excel.UIWorkbookPickerPortWorkbookName, JSONPointer: "/workbookName"},
					},
				},
				{
					ID: "policyTable", UnitID: excel.UIUnitTablePicker, Label: "Approval policy table", Required: true,
					Description: "Choose the approval-policy table, with a Category text column, a MaxAutoApproveUsd number column, and an Approver text column, from the workbook above; the picker stores the table's ID, which survives a rename, and the Flow fails at its first Step until one is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: excel.UITablePickerPortDriveID, JSONPointer: "/driveId"},
						{Port: excel.UITablePickerPortWorkbookID, JSONPointer: "/workbookId"},
						{Port: excel.UITablePickerPortTable, JSONPointer: "/table"},
						{Port: excel.UITablePickerPortTableName, JSONPointer: "/tableName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadPolicyInput,
			Read: sdkgo.GoTo(decideApproval{}),
		})),
		dex.DefineStep(decideApproval{}),
		dex.DefineStep(excel.NewAppendTableRowsStep(excel.AppendTableRowsStepConfig[ApprovalDecision]{
			StepType: appendDecisionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "microsoft-excel", GroupLabel: "Microsoft Excel",
				Explanation: "Append the decision row unless the decision log already holds the request ID.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "decisionLogWorkbook", UnitID: excel.UIUnitWorkbookPicker, Label: "Decision log workbook", Required: true,
					Description: "Choose the .xlsx workbook in OneDrive or SharePoint that holds the decision log table, which can be the policy workbook; the picker stores its drive ID, item ID, and file name, and the Flow fails at its first Step until one is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: excel.UIWorkbookPickerPortDriveID, JSONPointer: "/driveId"},
						{Port: excel.UIWorkbookPickerPortWorkbookID, JSONPointer: "/workbookId"},
						{Port: excel.UIWorkbookPickerPortWorkbookName, JSONPointer: "/workbookName"},
					},
				},
				{
					ID: "decisionLogTable", UnitID: excel.UIUnitTablePicker, Label: "Decision log table", Required: true,
					Description: "Choose the decision log table, with RequestId, Requester, Category, Decision, Approver, and DecidedAt text columns and an AmountUsd number column, from the workbook above; RequestId is the key that keeps a request from being logged twice. The picker stores the table's ID, and the Flow fails at its first Step until one is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: excel.UITablePickerPortDriveID, JSONPointer: "/driveId"},
						{Port: excel.UITablePickerPortWorkbookID, JSONPointer: "/workbookId"},
						{Port: excel.UITablePickerPortTable, JSONPointer: "/table"},
						{Port: excel.UITablePickerPortTableName, JSONPointer: "/tableName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToAppendDecisionInput,
			Appended:  sdkgo.GoTo(recordDecisionLogged{}),
			Uncertain: sdkgo.GoTo(reportDecisionUncertain{}),
		})),
		dex.DefineStep(recordDecisionLogged{}),
		dex.DefineStep(excel.NewUpdateValuesStep(excel.UpdateValuesStepConfig[ApprovalDecision]{
			StepType: updateSummaryStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "microsoft-excel", GroupLabel: "Microsoft Excel",
				Explanation: "Write the latest decision to the summary range A2:E2.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "summaryWorkbook", UnitID: excel.UIUnitWorkbookPicker, Label: "Summary workbook", Required: true,
					Description: "Choose the .xlsx workbook in OneDrive or SharePoint whose summary worksheet shows the latest decision; the picker stores its drive ID, item ID, and file name, and the Flow fails at its first Step until one is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: excel.UIWorkbookPickerPortDriveID, JSONPointer: "/driveId"},
						{Port: excel.UIWorkbookPickerPortWorkbookID, JSONPointer: "/workbookId"},
						{Port: excel.UIWorkbookPickerPortWorkbookName, JSONPointer: "/workbookName"},
					},
				},
				{
					ID: "summaryWorksheet", UnitID: excel.UIUnitWorksheetPicker, Label: "Summary worksheet", Required: true,
					Description: "Choose the worksheet whose cells A2:E2 receive the latest request ID, category, amount, decision, and decision time, overwriting what they held; the picker stores the worksheet's ID, and the Flow fails at its first Step until one is saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: excel.UIWorksheetPickerPortDriveID, JSONPointer: "/driveId"},
						{Port: excel.UIWorksheetPickerPortWorkbookID, JSONPointer: "/workbookId"},
						{Port: excel.UIWorksheetPickerPortWorksheet, JSONPointer: "/worksheet"},
						{Port: excel.UIWorksheetPickerPortWorksheetName, JSONPointer: "/worksheetName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpdateSummaryInput,
			Updated: sdkgo.GoTo(sdkgo.StepRef[excel.UpdateValuesResult](readBackSummaryStepType)),
		})),
		dex.DefineStep(excel.NewGetValuesStep(excel.GetValuesStepConfig[excel.UpdateValuesResult]{
			StepType: readBackSummaryStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "microsoft-excel", GroupLabel: "Microsoft Excel",
				Explanation: "Read the summary range back to confirm the written decision.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadBackSummaryInput,
			Read: sdkgo.GoTo(completeApprovalDecision{}),
		})),
		dex.DefineStep(completeApprovalDecision{}),
		dex.DefineStep(reportDecisionUncertain{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the decision Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{decisionAttribute}}
}

// GetDexSummary returns the request and its decision.
//
// dex:field attribute-key:microsoft-excel-approval-decision value-type:json editable:false description:"Request, decision, and logging progress"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	decision, err := optionalDecision(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-excel-approval-decision": decision}}, nil
}

// GetDexDisplay returns the request and its decision.
//
// dex:field attribute-key:microsoft-excel-approval-decision value-type:json editable:false description:"Policy match, decision log row, and summary read-back"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	decision, err := optionalDecision(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-excel-approval-decision": decision}}, nil
}

// MapToReadPolicyInput reads the saved policy table.
func (flow *Flow) MapToReadPolicyInput(ApprovalDecision) excel.GetTableRowsInput {
	saved := flow.policyTable.Value
	return excel.GetTableRowsInput{DriveID: saved.DriveID, WorkbookID: saved.WorkbookID, Table: saved.Table}
}

// MapToAppendDecisionInput appends one decision row keyed by request ID to the saved decision log.
func (flow *Flow) MapToAppendDecisionInput(decision ApprovalDecision) excel.AppendTableRowsInput {
	saved := flow.decisionTable.Value
	return excel.AppendTableRowsInput{
		DriveID: saved.DriveID, WorkbookID: saved.WorkbookID, Table: saved.Table, KeyColumn: DecisionRequestIDColumn,
		Rows: []map[string]excel.CellValue{{
			DecisionRequestIDColumn: excel.TextCellValue(decision.Request.RequestID),
			DecisionRequesterColumn: excel.TextCellValue(decision.Request.Requester),
			DecisionCategoryColumn:  excel.TextCellValue(decision.Request.Category),
			DecisionAmountColumn:    excel.NumberCellValue(decision.Request.AmountUSD),
			DecisionColumn:          excel.TextCellValue(string(decision.Decision)),
			DecisionApproverColumn:  excel.TextCellValue(decision.Approver),
			DecisionTimeColumn:      excel.TextCellValue(decision.DecidedAt),
		}},
	}
}

// MapToUpdateSummaryInput writes the latest decision to SummaryAddress on the saved worksheet.
func (flow *Flow) MapToUpdateSummaryInput(decision ApprovalDecision) excel.UpdateValuesInput {
	saved := flow.summaryWorksheet.Value
	return excel.UpdateValuesInput{
		DriveID: saved.DriveID, WorkbookID: saved.WorkbookID, Worksheet: saved.Worksheet, Address: SummaryAddress,
		Values: [][]excel.CellValue{{
			excel.TextCellValue(decision.Request.RequestID), excel.TextCellValue(decision.Request.Category),
			excel.NumberCellValue(decision.Request.AmountUSD), excel.TextCellValue(string(decision.Decision)),
			excel.TextCellValue(decision.DecidedAt),
		}},
	}
}

// MapToReadBackSummaryInput reads SummaryAddress back from the worksheet the summary Step wrote.
func (flow *Flow) MapToReadBackSummaryInput(excel.UpdateValuesResult) excel.GetValuesInput {
	saved := flow.summaryWorksheet.Value
	return excel.GetValuesInput{DriveID: saved.DriveID, WorkbookID: saved.WorkbookID, Worksheet: saved.Worksheet, Address: SummaryAddress}
}

// DecideAgainstPolicy decides a request against the policy rows: the single row
// whose Category equals the request's, ignoring case and surrounding spaces,
// must have a numeric MaxAutoApproveUsd; otherwise the request needs review.
func DecideAgainstPolicy(request Input, rows []excel.TableRow) (Decision, float64, string, string) {
	var matches []excel.TableRow
	for _, row := range rows {
		category, _ := row.Value(PolicyCategoryColumn)
		text, isText := category.Text()
		if isText && strings.EqualFold(strings.TrimSpace(text), request.Category) {
			matches = append(matches, row)
		}
	}
	if len(matches) != 1 {
		return DecisionNeedsReview, 0, "", "the policy table has no single row for this category"
	}
	limitValue, _ := matches[0].Value(PolicyLimitColumn)
	limit, isNumber := limitValue.Number()
	if !isNumber || limit < 0 || math.IsNaN(limit) {
		return DecisionNeedsReview, 0, "", "the policy row has no numeric MaxAutoApproveUsd"
	}
	if request.AmountUSD <= limit {
		return DecisionApproved, limit, "", ""
	}
	approverValue, _ := matches[0].Value(PolicyApproverColumn)
	return DecisionEscalated, limit, strings.TrimSpace(approverValue.String()), ""
}

func optionalDecision(ctx dex.Context) (ApprovalDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return ApprovalDecision{}, nil
	}
	return decision, err
}

// dex:group group-id:approval-decision group-label:"Approval decision"
// dex:explanation text:"Validate and record the spending request, and check that every Excel pick is saved."
type recordApprovalRequest struct {
	dex.StepDefaults
	flow *Flow
}

func (recordApprovalRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordApprovalRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordApprovalRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := ValidateRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if missing := step.flow.missingPickStep(); missing != "" {
		return dex.ForceFail("save the Excel picks on the " + missing + " Step in Dex Web, then restart the Worker"), nil
	}
	decision := ApprovalDecision{Request: request, Phase: PhaseRecorded, DecidedAt: ctx.FlowStartedAt().UTC().Format(time.RFC3339)}
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ApprovalDecision](readPolicyStepType), decision), nil
}

// missingPickStep names the first Step whose saved workbook, table, or worksheet is blank.
func (flow *Flow) missingPickStep() string {
	policy, decisions, summary := flow.policyTable.Value, flow.decisionTable.Value, flow.summaryWorksheet.Value
	switch {
	case policy.DriveID == "" || policy.WorkbookID == "" || policy.Table == "":
		return readPolicyStepType
	case decisions.DriveID == "" || decisions.WorkbookID == "" || decisions.Table == "":
		return appendDecisionStepType
	case summary.DriveID == "" || summary.WorkbookID == "" || summary.Worksheet == "":
		return updateSummaryStepType
	}
	return ""
}

// ValidateRequest trims the text fields and rejects a request the Flow cannot decide.
func ValidateRequest(input Input) (Input, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Requester = strings.TrimSpace(input.Requester)
	input.Category = strings.TrimSpace(input.Category)
	for name, value := range map[string]string{"requestId": input.RequestID, "requester": input.Requester, "category": input.Category} {
		if value == "" || len([]rune(value)) > maximumTextInputRunes || strings.ContainsFunc(value, isControlCharacter) {
			return Input{}, errors.New(name + " must be 1 to 200 characters on one line")
		}
	}
	if math.IsNaN(input.AmountUSD) || input.AmountUSD <= 0 || input.AmountUSD > maximumAmountUSD {
		return Input{}, errors.New("amountUsd must be a positive amount of at most 1000000000")
	}
	return input, nil
}

func isControlCharacter(character rune) bool { return character < 0x20 || character == 0x7f }

// dex:group group-id:approval-decision group-label:"Approval decision"
// dex:explanation text:"Decide the request against the policy row for its category."
type decideApproval struct {
	dex.StepDefaultsNoWaitFor[excel.GetTableRowsResult]
}

func (decideApproval) GetStepType() string { return decideApprovalStepType }

func (decideApproval) Execute(ctx dex.Context, result excel.GetTableRowsResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	decision.Decision, decision.MaxAutoApproveUSD, decision.Approver, decision.Reason = DecideAgainstPolicy(decision.Request, result.Value.Rows)
	decision.Phase = PhaseDecided
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ApprovalDecision](appendDecisionStepType), decision), nil
}

// dex:group group-id:approval-decision group-label:"Approval decision"
// dex:explanation text:"Record whether this Flow appended the decision row or found it already logged."
type recordDecisionLogged struct {
	dex.StepDefaultsNoWaitFor[excel.AppendTableRowsResult]
}

func (recordDecisionLogged) GetStepType() string { return recordDecisionLogStepType }

func (recordDecisionLogged) Execute(ctx dex.Context, result excel.AppendTableRowsResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	decision.Phase = PhaseLogged
	decision.IsLogRowAppended = len(result.Value.AppendedKeys) == 1
	decision.WasLogRowAlreadyPresent = len(result.Value.AlreadyPresentKeys) == 1
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ApprovalDecision](updateSummaryStepType), decision), nil
}

// dex:group group-id:approval-decision group-label:"Approval decision"
// dex:explanation text:"Confirm the summary read-back shows this request and complete the Flow."
type completeApprovalDecision struct {
	dex.StepDefaultsNoWaitFor[excel.GetValuesResult]
}

func (completeApprovalDecision) GetStepType() string { return completeDecisionStepType }

func (completeApprovalDecision) Execute(ctx dex.Context, result excel.GetValuesResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	decision.Phase = PhaseCompleted
	decision.SummaryAddress = result.Value.Address
	if len(result.Value.Text) == 1 {
		decision.SummaryText = result.Value.Text[0]
		decision.IsSummaryConfirmed = len(decision.SummaryText) == 5 && decision.SummaryText[0] == decision.Request.RequestID &&
			decision.SummaryText[3] == string(decision.Decision)
	}
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(decision), nil
}

// dex:group group-id:approval-decision group-label:"Approval decision"
// dex:explanation text:"Complete with logUncertain so a person checks the decision log instead of the Flow appending again."
type reportDecisionUncertain struct {
	dex.StepDefaultsNoWaitFor[excel.AppendTableRowsResult]
}

func (reportDecisionUncertain) GetStepType() string { return reportUncertainStepType }

func (reportDecisionUncertain) Execute(ctx dex.Context, result excel.AppendTableRowsResult) (*dex.StepDecision, error) {
	decision, err := decisionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	decision.Phase = PhaseLogUncertain
	if result.Failure != nil {
		decision.ReviewDetail = result.Failure.Message
	}
	if err := decisionAttribute.Set(ctx, decision); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(decision), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
