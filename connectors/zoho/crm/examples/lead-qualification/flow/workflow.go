// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leadqualification demonstrates five Zoho CRM operations in one Flow started from Dex Web
// Start Flow: check the requested deal stage against the Deals Stage picklist, find or create the
// account, upsert the contact by email linked to the account, move the contact's newest open deal to
// the stage, and read the deal back.
package leadqualification

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ZohoCRMLeadQualification"
	// ConnectionName is the static Dex Web connection for Zoho CRM.
	ConnectionName = "zoho-crm-sales"

	recordLeadQualificationStepType   = "RecordLeadQualification"
	readZohoDealStagesStepType        = "ReadZohoDealStages"
	checkZohoDealStageStepType        = "CheckZohoDealStage"
	findZohoAccountStepType           = "FindZohoAccount"
	chooseZohoAccountStepType         = "ChooseZohoAccount"
	upsertZohoAccountStepType         = "UpsertZohoAccount"
	recordZohoAccountStepType         = "RecordZohoAccount"
	upsertZohoLeadContactStepType     = "UpsertZohoLeadContact"
	recordZohoContactStepType         = "RecordZohoContact"
	reportZohoContactRejectedStepType = "ReportZohoContactRejected"
	findZohoOpenDealStepType          = "FindZohoOpenDeal"
	chooseZohoOpenDealStepType        = "ChooseZohoOpenDeal"
	advanceZohoOpenDealStepType       = "AdvanceZohoOpenDeal"
	readBackZohoDealStepType          = "ReadBackZohoDeal"
	completeLeadQualificationStepType = "CompleteLeadQualification"

	// AccountMatchLimit reads two accounts, enough to tell one match from several.
	AccountMatchLimit = 2
	maximumNameLength = 120
)

var (
	leadQualificationAttribute = dex.DefineAttribute[LeadQualification]("zoho-crm-lead-qualification")

	zohoIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// ClosedDealStages are Zoho CRM's default closed Deals stages; an organization with custom closed
// stages lists its own here. The connector passes stage names through unchanged.
var ClosedDealStages = []string{"Closed Won", "Closed Lost", "Closed Lost to Competition"}

// Input is the qualified lead entered in Dex Web Start Flow.
type Input struct {
	// ContactEmail is the person's email address, such as jane@acme.example.com.
	ContactEmail string `json:"contactEmail"`
	// ContactFirstName is the person's first name; blank leaves it unset.
	ContactFirstName string `json:"contactFirstName,omitempty"`
	// ContactLastName is the person's last name, which Zoho CRM requires for a new contact.
	ContactLastName string `json:"contactLastName"`
	// AccountName is the company, such as Acme Corp.
	AccountName string `json:"accountName"`
	// Stage is the Zoho CRM Deals stage to move the open deal to, such as Negotiation/Review.
	Stage string `json:"stage"`
	// OwnerID is the Zoho CRM user ID to own the deal; blank keeps the deal's owner.
	OwnerID string `json:"ownerId,omitempty"`
}

// LeadQualificationPhase is how far the Flow got.
type LeadQualificationPhase string

const (
	// PhaseUnknownStage means Stage is not an offered Deals stage; nothing was written.
	PhaseUnknownStage LeadQualificationPhase = "unknownStage"
	// PhaseAmbiguousAccount means several accounts carry AccountName; nothing was written.
	PhaseAmbiguousAccount LeadQualificationPhase = "ambiguousAccount"
	// PhaseContactRejected means Zoho CRM rejected the contact's values.
	PhaseContactRejected LeadQualificationPhase = "contactRejected"
	// PhaseNoOpenDeal means the contact has no open deal to advance.
	PhaseNoOpenDeal LeadQualificationPhase = "noOpenDeal"
	// PhaseDealAdvanced means the deal was read back at the requested stage.
	PhaseDealAdvanced LeadQualificationPhase = "dealAdvanced"
	// PhaseStageNotApplied means the read-back shows another stage, such as one a workflow set.
	PhaseStageNotApplied LeadQualificationPhase = "stageNotApplied"
)

// LeadQualification is the request and its progress, the Flow's single Attribute and its result.
type LeadQualification struct {
	// Request is the validated Start Flow input.
	Request Input `json:"request"`
	// Phase is the terminal phase, empty while the Flow runs.
	Phase LeadQualificationPhase `json:"phase,omitempty"`
	// AccountID is the account the contact is linked to.
	AccountID string `json:"accountId,omitempty"`
	// IsAccountCreated reports that the Flow's upsert inserted the account.
	IsAccountCreated bool `json:"isAccountCreated,omitempty"`
	// ContactID is the upserted contact.
	ContactID string `json:"contactId,omitempty"`
	// IsContactCreated reports that the answered upsert inserted the contact.
	IsContactCreated bool `json:"isContactCreated,omitempty"`
	// DealID is the advanced deal.
	DealID string `json:"dealId,omitempty"`
	// DealStage is the stage the read-back returned.
	DealStage string `json:"dealStage,omitempty"`
	// DealOwnerID is the owner the read-back returned.
	DealOwnerID string `json:"dealOwnerId,omitempty"`
	// ProviderErrors are Zoho CRM's codes and fields when it rejected the contact.
	ProviderErrors []crm.ProviderError `json:"providerErrors,omitempty"`
}

// ModuleReference names the module whose fields a Step reads.
type ModuleReference struct {
	// Module is the module API name.
	Module string `json:"module"`
}

// AccountLookup is the company to find or create.
type AccountLookup struct {
	// AccountName is the account's name.
	AccountName string `json:"accountName"`
}

// ContactUpsert is the contact to upsert, linked to its account.
type ContactUpsert struct {
	// Request is the lead.
	Request Input `json:"request"`
	// AccountID is the account the contact belongs to.
	AccountID string `json:"accountId"`
}

// OpenDealLookup is the contact whose open deals to find.
type OpenDealLookup struct {
	// ContactID is the contact.
	ContactID string `json:"contactId"`
}

// DealStageChange is the deal to advance, its stage, and an optional owner.
type DealStageChange struct {
	// DealID is the deal.
	DealID string `json:"dealId"`
	// Stage is the Deals stage to set.
	Stage string `json:"stage"`
	// OwnerID is the user to own the deal, or empty to keep the owner.
	OwnerID string `json:"ownerId,omitempty"`
}

// Flow qualifies one lead in Zoho CRM.
type Flow struct {
	dex.FlowDefaults
	connection crm.Connection
}

// NewFlow binds the Zoho CRM Connection at registration time.
func NewFlow(connection crm.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zoho CRM connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordLeadQualification{}),
		dex.DefineStep(crm.NewListModuleFieldsStep(crm.ListModuleFieldsStepConfig[ModuleReference]{
			StepType: readZohoDealStagesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Read the Deals fields to learn which stages the Stage picklist offers.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListModuleFieldsInput,
			Listed: sdkgo.GoTo(checkZohoDealStage{}),
		})),
		dex.DefineStep(checkZohoDealStage{}),
		dex.DefineStep(crm.NewFindRecordsStep(crm.FindRecordsStepConfig[AccountLookup]{
			StepType: findZohoAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Find accounts whose name equals the lead's company.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindAccountInput,
			Found: sdkgo.GoTo(chooseZohoAccount{}), NotFound: sdkgo.GoTo(chooseZohoAccount{}),
		})),
		dex.DefineStep(chooseZohoAccount{}),
		dex.DefineStep(crm.NewUpsertRecordStep(crm.UpsertRecordStepConfig[AccountLookup]{
			StepType: upsertZohoAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Create the account by name; a repeated attempt updates the account it created.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpsertAccountInput,
			Upserted: sdkgo.GoTo(recordZohoAccount{}),
		})),
		dex.DefineStep(recordZohoAccount{}),
		dex.DefineStep(crm.NewUpsertRecordStep(crm.UpsertRecordStepConfig[ContactUpsert]{
			StepType: upsertZohoLeadContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Create or update the contact by email and link it to the account.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpsertContactInput,
			Upserted: sdkgo.GoTo(recordZohoContact{}), RecordRejected: sdkgo.GoTo(reportZohoContactRejected{}),
		})),
		dex.DefineStep(recordZohoContact{}),
		dex.DefineStep(reportZohoContactRejected{}),
		dex.DefineStep(crm.NewFindRecordsStep(crm.FindRecordsStepConfig[OpenDealLookup]{
			StepType: findZohoOpenDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Find the contact's most recently modified deal that is not in a closed stage.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindOpenDealInput,
			Found: sdkgo.GoTo(chooseZohoOpenDeal{}), NotFound: sdkgo.GoTo(chooseZohoOpenDeal{}),
		})),
		dex.DefineStep(chooseZohoOpenDeal{}),
		dex.DefineStep(crm.NewUpdateRecordStep(crm.UpdateRecordStepConfig[DealStageChange]{
			StepType: advanceZohoOpenDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Set the deal's stage, and its owner when one was given; a repeat sets the same values.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateDealInput,
			Updated: sdkgo.GoTo(sdkgo.StepRef[crm.UpdateRecordResult](readBackZohoDealStepType)),
		})),
		dex.DefineStep(crm.NewGetRecordStep(crm.GetRecordStepConfig[crm.UpdateRecordResult]{
			StepType: readBackZohoDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "Read the deal back to confirm its stage and owner.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReadBackDealInput,
			Found: sdkgo.GoTo(completeLeadQualification{}),
		})),
		dex.DefineStep(completeLeadQualification{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the lead qualification Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadQualificationAttribute}}
}

// GetDexSummary returns the lead qualification.
//
// dex:field attribute-key:zoho-crm-lead-qualification value-type:json editable:false description:"Zoho CRM lead qualification"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	qualification, err := optionalLeadQualification(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zoho-crm-lead-qualification": qualification}}, nil
}

// GetDexDisplay returns the request, the records the Flow touched, and its phase.
//
// dex:field attribute-key:zoho-crm-lead-qualification value-type:json editable:false description:"Request, account, contact, deal, and phase"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	qualification, err := optionalLeadQualification(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zoho-crm-lead-qualification": qualification}}, nil
}

// MapToListModuleFieldsInput reads the module's fields.
func MapToListModuleFieldsInput(reference ModuleReference) crm.ListModuleFieldsInput {
	return crm.ListModuleFieldsInput{Module: reference.Module}
}

// MapToFindAccountInput finds accounts whose Account_Name equals the company, oldest first.
func MapToFindAccountInput(lookup AccountLookup) crm.FindRecordsInput {
	return crm.FindRecordsInput{
		Module: crm.ModuleAccounts, Fields: []string{crm.FieldAccountName},
		Conditions: []crm.RecordCondition{{Field: crm.FieldAccountName, Operator: crm.ConditionEquals, Value: conditionValue(crm.COQLText(lookup.AccountName))}},
		Limit:      AccountMatchLimit,
	}
}

// MapToUpsertAccountInput creates the account, using Account_Name, its system duplicate-check field.
func MapToUpsertAccountInput(lookup AccountLookup) crm.UpsertRecordInput {
	return crm.UpsertRecordInput{
		Module: crm.ModuleAccounts, DuplicateCheckFields: []string{crm.FieldAccountName},
		Fields: map[string]json.RawMessage{crm.FieldAccountName: crm.TextFieldValue(lookup.AccountName)},
	}
}

// MapToUpsertContactInput upserts the contact by Email and links it to the account.
func MapToUpsertContactInput(contact ContactUpsert) crm.UpsertRecordInput {
	fields := map[string]json.RawMessage{
		crm.FieldEmail: crm.TextFieldValue(contact.Request.ContactEmail), crm.FieldLastName: crm.TextFieldValue(contact.Request.ContactLastName),
		crm.FieldAccountName: crm.LookupFieldValue(contact.AccountID),
	}
	if contact.Request.ContactFirstName != "" {
		fields[crm.FieldFirstName] = crm.TextFieldValue(contact.Request.ContactFirstName)
	}
	return crm.UpsertRecordInput{Module: crm.ModuleContacts, DuplicateCheckFields: []string{crm.FieldEmail}, Fields: fields}
}

// MapToFindOpenDealInput finds the contact's newest deal outside ClosedDealStages.
func MapToFindOpenDealInput(lookup OpenDealLookup) crm.FindRecordsInput {
	return crm.FindRecordsInput{
		Module: crm.ModuleDeals, Fields: []string{crm.FieldDealName, crm.FieldStage, crm.FieldOwner, crm.FieldModifiedTime},
		Conditions: []crm.RecordCondition{
			{Field: crm.FieldContactName, Operator: crm.ConditionEquals, Value: conditionValue(crm.COQLRecordID(lookup.ContactID))},
			{Field: crm.FieldStage, Operator: crm.ConditionNotIn, Value: conditionValue(crm.COQLTextList(ClosedDealStages))},
		},
		Sort: &crm.RecordSort{Field: crm.FieldModifiedTime, IsDescending: true}, Limit: 1,
	}
}

// MapToUpdateDealInput sets the stage, and the owner when one was given.
func MapToUpdateDealInput(change DealStageChange) crm.UpdateRecordInput {
	fields := map[string]json.RawMessage{crm.FieldStage: crm.TextFieldValue(change.Stage)}
	if change.OwnerID != "" {
		fields[crm.FieldOwner] = crm.LookupFieldValue(change.OwnerID)
	}
	return crm.UpdateRecordInput{Module: crm.ModuleDeals, RecordID: change.DealID, Fields: fields}
}

// MapToReadBackDealInput reads the updated deal's stage, owner, and contact.
func MapToReadBackDealInput(result crm.UpdateRecordResult) crm.GetRecordInput {
	return crm.GetRecordInput{
		Module: crm.ModuleDeals, RecordID: result.Value.ID,
		Fields: []string{crm.FieldDealName, crm.FieldStage, crm.FieldOwner, crm.FieldContactName, crm.FieldModifiedTime},
	}
}

// BuildLeadQualificationRequest validates Start Flow input so no connector Step receives an unusable lead.
func BuildLeadQualificationRequest(input Input) (Input, error) {
	request := Input{
		ContactEmail: strings.TrimSpace(input.ContactEmail), ContactFirstName: strings.TrimSpace(input.ContactFirstName),
		ContactLastName: strings.TrimSpace(input.ContactLastName), AccountName: strings.TrimSpace(input.AccountName),
		Stage: strings.TrimSpace(input.Stage), OwnerID: strings.TrimSpace(input.OwnerID),
	}
	address, err := mail.ParseAddress(request.ContactEmail)
	if err != nil || address.Name != "" || address.Address != request.ContactEmail || strings.ContainsAny(request.ContactEmail, `'\`) {
		return Input{}, fmt.Errorf("contactEmail %q must be one bare email address without an apostrophe", input.ContactEmail)
	}
	for name, value := range map[string]string{"contactLastName": request.ContactLastName, "accountName": request.AccountName, "stage": request.Stage} {
		if value == "" || len(value) > maximumNameLength {
			return Input{}, fmt.Errorf("%s is required and at most %d characters", name, maximumNameLength)
		}
	}
	if strings.ContainsAny(request.AccountName+request.Stage, `'\`) {
		return Input{}, errors.New("accountName and stage cannot contain an apostrophe or backslash, which COQL cannot quote")
	}
	if request.OwnerID != "" && !zohoIDPattern.MatchString(request.OwnerID) {
		return Input{}, errors.New("ownerId must be a numeric Zoho CRM user ID such as 4150868000000225013")
	}
	return request, nil
}

func conditionValue(value crm.COQLValue) *crm.COQLValue { return &value }

func optionalLeadQualification(ctx dex.Context) (LeadQualification, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return LeadQualification{}, nil
	}
	return qualification, err
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Validate the lead and record it before calling Zoho CRM."
type recordLeadQualification struct {
	dex.StepDefaults
}

func (recordLeadQualification) GetStepType() string { return recordLeadQualificationStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordLeadQualification) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordLeadQualification) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildLeadQualificationRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := leadQualificationAttribute.Set(ctx, LeadQualification{Request: request}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ModuleReference](readZohoDealStagesStepType), ModuleReference{Module: crm.ModuleDeals}), nil
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Continue only when the requested stage is an offered Deals stage; otherwise complete without writing."
type checkZohoDealStage struct {
	dex.StepDefaultsNoWaitFor[crm.ListModuleFieldsResult]
}

func (checkZohoDealStage) GetStepType() string { return checkZohoDealStageStepType }

func (checkZohoDealStage) Execute(ctx dex.Context, result crm.ListModuleFieldsResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	stage, isFound := result.Value.FieldNamed(crm.FieldStage)
	if !isFound || !stage.HasPicklistValue(qualification.Request.Stage) {
		qualification.Phase = PhaseUnknownStage
		if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(qualification), nil
	}
	return dex.GoTo(sdkgo.StepRef[AccountLookup](findZohoAccountStepType), AccountLookup{AccountName: qualification.Request.AccountName}), nil
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Use the one matching account, create one when none matches, or complete without writing when several match."
type chooseZohoAccount struct {
	dex.StepDefaultsNoWaitFor[crm.FindRecordsResult]
}

func (chooseZohoAccount) GetStepType() string { return chooseZohoAccountStepType }

func (chooseZohoAccount) Execute(ctx dex.Context, result crm.FindRecordsResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	switch len(result.Value.Records) {
	case 0:
		return dex.GoTo(sdkgo.StepRef[AccountLookup](upsertZohoAccountStepType), AccountLookup{AccountName: qualification.Request.AccountName}), nil
	case 1:
		qualification.AccountID = result.Value.Records[0].ID
		if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[ContactUpsert](upsertZohoLeadContactStepType), ContactUpsert{Request: qualification.Request, AccountID: qualification.AccountID}), nil
	default:
		qualification.Phase = PhaseAmbiguousAccount
		if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(qualification), nil
	}
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Record the created account and upsert the contact into it."
type recordZohoAccount struct {
	dex.StepDefaultsNoWaitFor[crm.UpsertRecordResult]
}

func (recordZohoAccount) GetStepType() string { return recordZohoAccountStepType }

func (recordZohoAccount) Execute(ctx dex.Context, result crm.UpsertRecordResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.AccountID, qualification.IsAccountCreated = result.Value.ID, result.Value.IsCreated
	if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ContactUpsert](upsertZohoLeadContactStepType), ContactUpsert{Request: qualification.Request, AccountID: qualification.AccountID}), nil
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Record the contact and look for its open deal."
type recordZohoContact struct {
	dex.StepDefaultsNoWaitFor[crm.UpsertRecordResult]
}

func (recordZohoContact) GetStepType() string { return recordZohoContactStepType }

func (recordZohoContact) Execute(ctx dex.Context, result crm.UpsertRecordResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.ContactID, qualification.IsContactCreated = result.Value.ID, result.Value.IsCreated
	if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OpenDealLookup](findZohoOpenDealStepType), OpenDealLookup{ContactID: qualification.ContactID}), nil
}

// dex:group group-id:review group-label:"Rejected"
// dex:explanation text:"Zoho CRM rejected the contact's values; complete with its error codes and field names."
type reportZohoContactRejected struct {
	dex.StepDefaultsNoWaitFor[crm.UpsertRecordResult]
}

func (reportZohoContactRejected) GetStepType() string { return reportZohoContactRejectedStepType }

func (reportZohoContactRejected) Execute(ctx dex.Context, result crm.UpsertRecordResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.ProviderErrors, qualification.Phase = result.Value.ProviderErrors, PhaseContactRejected
	if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(qualification), nil
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Advance the newest open deal, or complete when the contact has none."
type chooseZohoOpenDeal struct {
	dex.StepDefaultsNoWaitFor[crm.FindRecordsResult]
}

func (chooseZohoOpenDeal) GetStepType() string { return chooseZohoOpenDealStepType }

func (chooseZohoOpenDeal) Execute(ctx dex.Context, result crm.FindRecordsResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Records) == 0 {
		qualification.Phase = PhaseNoOpenDeal
		if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(qualification), nil
	}
	qualification.DealID = result.Value.Records[0].ID
	if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DealStageChange](advanceZohoOpenDealStepType), DealStageChange{
		DealID: qualification.DealID, Stage: qualification.Request.Stage, OwnerID: qualification.Request.OwnerID,
	}), nil
}

// dex:group group-id:lead group-label:"Lead"
// dex:explanation text:"Complete with the deal as read back, noting when its stage or owner differs from the request."
type completeLeadQualification struct {
	dex.StepDefaultsNoWaitFor[crm.GetRecordResult]
}

func (completeLeadQualification) GetStepType() string { return completeLeadQualificationStepType }

func (completeLeadQualification) Execute(ctx dex.Context, result crm.GetRecordResult) (*dex.StepDecision, error) {
	qualification, err := leadQualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.DealStage, _ = result.Value.StringField(crm.FieldStage)
	qualification.DealOwnerID, _ = result.Value.LookupID(crm.FieldOwner)
	isApplied := qualification.DealStage == qualification.Request.Stage &&
		(qualification.Request.OwnerID == "" || qualification.DealOwnerID == qualification.Request.OwnerID)
	qualification.Phase = PhaseDealAdvanced
	if !isApplied {
		qualification.Phase = PhaseStageNotApplied
	}
	if err := leadQualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(qualification), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
