// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package dealintake demonstrates every Pipedrive operation in one Flow
// started from Dex Web Start Flow: it finds the lead's organization by exact
// name, upserts the lead person by email, lists the person's open deal in the
// configured pipeline, moves that deal to the configured stage or creates one
// there, and reads the deal back to confirm the stage.
package dealintake

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "PipedriveDealIntake"
	// ConnectionName is the static Dex Web connection for Pipedrive.
	ConnectionName = "pipedrive-crm"

	recordLeadStepType           = "RecordPipedriveLead"
	findOrganizationStepType     = "FindPipedriveOrganization"
	routeOrganizationStepType    = "RoutePipedriveOrganization"
	upsertLeadPersonStepType     = "UpsertPipedriveLeadPerson"
	recordLeadPersonStepType     = "RecordPipedriveLeadPerson"
	recordUnresolvedLeadStepType = "RecordUnresolvedPipedriveLead"
	findOpenDealStepType         = "FindPipedriveOpenDeal"
	routeOpenDealStepType        = "RoutePipedriveOpenDeal"
	createDealStepType           = "CreatePipedriveDeal"
	advanceDealStepType          = "AdvancePipedriveDeal"
	recordCreatedDealStepType    = "RecordCreatedPipedriveDeal"
	recordAdvancedDealStepType   = "RecordAdvancedPipedriveDeal"
	recordUnresolvedDealStepType = "RecordUnresolvedPipedriveDeal"
	readBackDealStepType         = "ReadBackPipedriveDeal"
	completeDealIntakeStepType   = "CompletePipedriveDealIntake"

	maximumEmailLength = 254
	maximumTextLength  = 255
)

var (
	intakeAttribute     = dex.DefineAttribute[DealIntake]("pipedrive-deal-intake")
	decimalIDPattern    = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	customFieldPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	errSettingsRequired = errors.New("Pipedrive deal intake settings are required")
)

// Phase is the durable progress of one deal intake.
type Phase string

const (
	// PhaseRecorded means the lead was validated and recorded.
	PhaseRecorded Phase = "recorded"
	// PhasePersonUpserted means Pipedrive holds the lead person.
	PhasePersonUpserted Phase = "personUpserted"
	// PhaseDealWritten means the deal was created or moved, before the read-back.
	PhaseDealWritten Phase = "dealWritten"
	// PhaseCompleted means the read-back confirmed the deal's stage.
	PhaseCompleted Phase = "completed"
	// PhaseNeedsReview means a person must resolve the lead or deal in Pipedrive.
	PhaseNeedsReview Phase = "needsReview"
)

// DealAction says what happened to the lead's deal.
type DealAction string

const (
	// DealActionCreated means the person had no open deal in the pipeline, so one was created.
	DealActionCreated DealAction = "created"
	// DealActionAdvanced means the person's open deal was moved to the configured stage.
	DealActionAdvanced DealAction = "advanced"
)

// Input contains the lead fields entered in Dex Web Start Flow.
type Input struct {
	// Email is the lead's plain email address, which identifies the Pipedrive person.
	Email string `json:"email"`
	// Name is the person's full name.
	Name string `json:"name"`
	// Organization optionally names the lead's organization exactly as Pipedrive has it.
	Organization string `json:"organization,omitempty"`
	// DealTitle is the title of a deal the intake creates.
	DealTitle string `json:"dealTitle"`
	// Source optionally records where the lead came from in the configured deal custom field.
	Source string `json:"source,omitempty"`
}

// DealIntake is the Flow's durable state and completion output.
type DealIntake struct {
	// Lead is the validated Start Flow input.
	Lead Input `json:"lead"`
	// Phase is the current or terminal progress.
	Phase Phase `json:"phase"`
	// OrganizationID is the one organization whose name equals Lead.Organization.
	OrganizationID string `json:"organizationId,omitempty"`
	// OrganizationMatches counts the organizations found with that exact name.
	OrganizationMatches int `json:"organizationMatches"`
	// PersonID is the upserted person.
	PersonID string `json:"personId,omitempty"`
	// IsPersonCreated reports whether the answered upsert created the person.
	IsPersonCreated bool `json:"personCreated"`
	// DealID is the created or advanced deal.
	DealID string `json:"dealId,omitempty"`
	// DealAction says whether the deal was created or advanced.
	DealAction DealAction `json:"dealAction,omitempty"`
	// PreviousStageID is the advanced deal's stage before the update.
	PreviousStageID string `json:"previousStageId,omitempty"`
	// StageID is the stage Pipedrive reported for the deal.
	StageID string `json:"stageId,omitempty"`
	// ReviewStep names the Step whose outcome needs a person's review.
	ReviewStep string `json:"reviewStep,omitempty"`
	// ReviewBranch is that Step's branch, such as multipleMatches or uncertain.
	ReviewBranch sdkgo.BranchID `json:"reviewBranch,omitempty"`
	// ReviewDetail is Pipedrive's secret-safe failure for the review.
	ReviewDetail *sdkgo.Failure `json:"reviewDetail,omitempty"`
}

// LeadOwnerConfiguration is the owner pick Dex Web saves for the person upsert Step.
type LeadOwnerConfiguration struct {
	// OwnerID is the numeric Pipedrive user ID; blank sets no owner.
	OwnerID string `json:"ownerId"`
}

// DealConfiguration is the stage and lead source field picks Dex Web saves for the deal create Step.
type DealConfiguration struct {
	// PipelineID is the pipeline whose open deal is reused.
	PipelineID string `json:"pipelineId"`
	// StageID is the stage a created or reused deal moves to.
	StageID string `json:"stageId"`
	// SourceObjectType is deals when a lead source field is picked.
	SourceObjectType string `json:"sourceObjectType,omitempty"`
	// SourceFieldKey is the 40-character key of a deal text custom field; blank writes no source.
	SourceFieldKey string `json:"sourceFieldKey,omitempty"`
}

// Settings holds the operation configuration loaded once at startup.
type Settings struct {
	// LeadOwner is the optional owner of upserted persons, created deals, and advanced deals.
	LeadOwner LeadOwnerConfiguration
	// Deal is the required pipeline and stage and the optional lead source field.
	Deal DealConfiguration
}

// Flow runs one Pipedrive deal intake.
type Flow struct {
	dex.FlowDefaults
	connection pipedrive.Connection
	settings   Settings
}

// NewFlow binds the Pipedrive Connection and startup settings. It fails when
// the stage pick is missing or any saved ID is malformed.
func NewFlow(connection pipedrive.Connection, settings *Settings) (*Flow, error) {
	if settings == nil {
		return nil, errSettingsRequired
	}
	deal := settings.Deal
	if !decimalIDPattern.MatchString(deal.PipelineID) || !decimalIDPattern.MatchString(deal.StageID) {
		return nil, errors.New("Pipedrive deal intake needs the numeric pipeline and stage IDs saved for CreatePipedriveDeal")
	}
	if settings.LeadOwner.OwnerID != "" && !decimalIDPattern.MatchString(settings.LeadOwner.OwnerID) {
		return nil, errors.New("the saved Pipedrive lead owner must be a numeric user ID")
	}
	if deal.SourceFieldKey != "" && (deal.SourceObjectType != string(pipedrive.ObjectTypeDeals) || !customFieldPattern.MatchString(deal.SourceFieldKey)) {
		return nil, errors.New("the saved lead source field must be a deals custom field key")
	}
	return &Flow{connection: connection, settings: *settings}, nil
}

// LeadOwnerConfigurationRef identifies the owner pick of the person upsert Step.
func LeadOwnerConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: pipedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "upsertObject",
		FlowType: FlowType, StepType: upsertLeadPersonStepType,
	}
}

// DealConfigurationRef identifies the stage and source field picks of the deal create Step.
func DealConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: pipedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "createObject",
		FlowType: FlowType, StepType: createDealStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Pipedrive Connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordLead{}),
		dex.DefineStep(pipedrive.NewSearchObjectsStep(pipedrive.SearchObjectsStepConfig[DealIntake]{
			StepType: findOrganizationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "pipedrive", GroupLabel: "Pipedrive",
				Explanation: "Search organizations whose name equals the lead's organization.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToFindOrganizationInput,
			Searched: sdkgo.GoTo(routeOrganization{}),
		})),
		dex.DefineStep(routeOrganization{}),
		dex.DefineStep(pipedrive.NewUpsertObjectStep(pipedrive.UpsertObjectStepConfig[DealIntake]{
			StepType: upsertLeadPersonStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "pipedrive", GroupLabel: "Pipedrive",
				Explanation: "Create or update the lead person identified by email, linked to the organization.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "leadOwner", UnitID: pipedrive.UIUnitOwnerPicker, Label: "Lead owner",
				Description: "Select the Pipedrive user who owns each upserted lead person and the deal the intake creates or advances; the picker stores the user's numeric ID for owner_id, and blank leaves owners to Pipedrive's default for new records and unchanged for existing ones.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: pipedrive.UIOwnerPickerPortOwnerID, JSONPointer: "/ownerId"}},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpsertLeadPersonInput,
			Upserted:         sdkgo.GoTo(recordLeadPerson{}),
			MultipleMatches:  sdkgo.GoTo(recordUnresolvedLead{}),
			ProviderRejected: sdkgo.GoTo(recordUnresolvedLead{}),
			Uncertain:        sdkgo.GoTo(recordUnresolvedLead{}),
		})),
		dex.DefineStep(recordLeadPerson{}),
		dex.DefineStep(recordUnresolvedLead{}),
		dex.DefineStep(pipedrive.NewListObjectsStep(pipedrive.ListObjectsStepConfig[DealIntake]{
			StepType: findOpenDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "pipedrive", GroupLabel: "Pipedrive",
				Explanation: "List the person's most recently changed open deal in the configured pipeline.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToFindOpenDealInput,
			Listed: sdkgo.GoTo(routeOpenDeal{}),
		})),
		dex.DefineStep(routeOpenDeal{}),
		dex.DefineStep(pipedrive.NewCreateObjectStep(pipedrive.CreateObjectStepConfig[DealIntake]{
			StepType: createDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "pipedrive", GroupLabel: "Pipedrive",
				Explanation: "Create the lead's deal in the configured stage; a later attempt reports an unconfirmed create as uncertain.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "dealStage", UnitID: pipedrive.UIUnitDealStagePicker, Label: "Deal stage", Required: true,
					Description: "Select the pipeline whose open deal of the lead is reused and the stage that deal moves to, or where a new deal is created; the picker stores the numeric pipeline ID and stage ID, and the Worker does not start until both are saved.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: pipedrive.UIDealStagePickerPortPipelineID, JSONPointer: "/pipelineId"},
						{Port: pipedrive.UIDealStagePickerPortStageID, JSONPointer: "/stageId"},
					},
				},
				{
					ID: "leadSourceField", UnitID: pipedrive.UIUnitCustomFieldPicker, Label: "Lead source field",
					Description: "Select deals and a text custom field that receives the Start Flow source on each deal the intake creates; the picker stores the object type and the field's 40-character key, and blank, or a blank source, writes no custom field.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: pipedrive.UICustomFieldPickerPortObjectType, JSONPointer: "/sourceObjectType"},
						{Port: pipedrive.UICustomFieldPickerPortFieldKey, JSONPointer: "/sourceFieldKey"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToCreateDealInput,
			Created:          sdkgo.GoTo(recordCreatedDeal{}),
			ProviderRejected: sdkgo.GoTo(recordUnresolvedDeal{}),
			Uncertain:        sdkgo.GoTo(recordUnresolvedDeal{}),
		})),
		dex.DefineStep(pipedrive.NewUpdateObjectStep(pipedrive.UpdateObjectStepConfig[DealIntake]{
			StepType: advanceDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "pipedrive", GroupLabel: "Pipedrive",
				Explanation: "Move the open deal to the configured stage and owner.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToAdvanceDealInput,
			Updated: sdkgo.GoTo(recordAdvancedDeal{}),
		})),
		dex.DefineStep(recordCreatedDeal{}),
		dex.DefineStep(recordAdvancedDeal{}),
		dex.DefineStep(recordUnresolvedDeal{}),
		dex.DefineStep(pipedrive.NewGetObjectStep(pipedrive.GetObjectStepConfig[DealIntake]{
			StepType: readBackDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "pipedrive", GroupLabel: "Pipedrive",
				Explanation: "Read the deal back to confirm Pipedrive stored the configured stage.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadBackDealInput,
			Found: sdkgo.GoTo(completeDealIntake{settings: &flow.settings}),
		})),
		dex.DefineStep(completeDealIntake{settings: &flow.settings}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the deal intake Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{intakeAttribute}}
}

// GetDexSummary returns the deal intake state.
//
// dex:field attribute-key:pipedrive-deal-intake value-type:json editable:false description:"Lead, person, deal, and phase"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	intake, err := optionalIntake(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pipedrive-deal-intake": intake}}, nil
}

// GetDexDisplay returns the deal intake state.
//
// dex:field attribute-key:pipedrive-deal-intake value-type:json editable:false description:"Lead email, Pipedrive person and deal IDs, deal stage, and phase"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	intake, err := optionalIntake(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pipedrive-deal-intake": intake}}, nil
}

// MapToFindOrganizationInput searches organizations whose name equals the lead's organization, ignoring case.
func (*Flow) MapToFindOrganizationInput(intake DealIntake) pipedrive.SearchObjectsInput {
	return pipedrive.SearchObjectsInput{
		ObjectType: pipedrive.ObjectTypeOrganizations, Term: intake.Lead.Organization,
		Fields: []pipedrive.SearchField{pipedrive.SearchFieldName}, ExactMatch: true, Limit: 10,
	}
}

// MapToUpsertLeadPersonInput upserts the person by email with the name, the one matching organization, and the owner.
func (flow *Flow) MapToUpsertLeadPersonInput(intake DealIntake) pipedrive.UpsertObjectInput {
	fields := map[string]json.RawMessage{"name": pipedrive.StringValue(intake.Lead.Name)}
	if intake.OrganizationID != "" {
		fields["org_id"] = pipedrive.IDValue(intake.OrganizationID)
	}
	if flow.settings.LeadOwner.OwnerID != "" {
		fields["owner_id"] = pipedrive.IDValue(flow.settings.LeadOwner.OwnerID)
	}
	return pipedrive.UpsertObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, IDProperty: pipedrive.IdentityPropertyEmail, IDValue: intake.Lead.Email, Fields: fields,
	}
}

// MapToFindOpenDealInput lists the person's most recently changed open deal in the configured pipeline.
func (flow *Flow) MapToFindOpenDealInput(intake DealIntake) pipedrive.ListObjectsInput {
	return pipedrive.ListObjectsInput{
		ObjectType: pipedrive.ObjectTypeDeals, PersonID: intake.PersonID, PipelineID: flow.settings.Deal.PipelineID,
		Statuses: []pipedrive.DealStatus{pipedrive.DealStatusOpen},
		SortBy:   pipedrive.ListSortFieldUpdateTime, SortDirection: pipedrive.SortDirectionDescending, Limit: 1,
	}
}

// MapToCreateDealInput creates the lead's deal in the configured stage, with the source in the picked custom field.
func (flow *Flow) MapToCreateDealInput(intake DealIntake) pipedrive.CreateObjectInput {
	deal := flow.settings.Deal
	fields := map[string]json.RawMessage{
		"title": pipedrive.StringValue(intake.Lead.DealTitle), "person_id": pipedrive.IDValue(intake.PersonID),
		"pipeline_id": pipedrive.IDValue(deal.PipelineID), "stage_id": pipedrive.IDValue(deal.StageID),
	}
	if intake.OrganizationID != "" {
		fields["org_id"] = pipedrive.IDValue(intake.OrganizationID)
	}
	if flow.settings.LeadOwner.OwnerID != "" {
		fields["owner_id"] = pipedrive.IDValue(flow.settings.LeadOwner.OwnerID)
	}
	input := pipedrive.CreateObjectInput{ObjectType: pipedrive.ObjectTypeDeals, Fields: fields}
	if deal.SourceFieldKey != "" && intake.Lead.Source != "" {
		input.CustomFields = map[string]json.RawMessage{deal.SourceFieldKey: pipedrive.StringValue(intake.Lead.Source)}
	}
	return input
}

// MapToAdvanceDealInput moves the open deal to the configured stage and, when one is picked, owner.
func (flow *Flow) MapToAdvanceDealInput(intake DealIntake) pipedrive.UpdateObjectInput {
	fields := map[string]json.RawMessage{"stage_id": pipedrive.IDValue(flow.settings.Deal.StageID)}
	if flow.settings.LeadOwner.OwnerID != "" {
		fields["owner_id"] = pipedrive.IDValue(flow.settings.LeadOwner.OwnerID)
	}
	return pipedrive.UpdateObjectInput{ObjectType: pipedrive.ObjectTypeDeals, ObjectID: intake.DealID, Fields: fields}
}

// MapToReadBackDealInput reads the written deal.
func (*Flow) MapToReadBackDealInput(intake DealIntake) pipedrive.GetObjectInput {
	return pipedrive.GetObjectInput{ObjectType: pipedrive.ObjectTypeDeals, ObjectID: intake.DealID}
}

func optionalIntake(ctx dex.Context) (DealIntake, error) {
	intake, err := intakeAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return DealIntake{}, nil
	}
	return intake, err
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Validate and record the lead before calling Pipedrive."
type recordLead struct {
	dex.StepDefaults
}

func (recordLead) GetStepType() string { return recordLeadStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordLead) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordLead) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	lead, err := ValidateLead(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	intake := DealIntake{Lead: lead, Phase: PhaseRecorded}
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	if lead.Organization == "" {
		return dex.GoTo(sdkgo.StepRef[DealIntake](upsertLeadPersonStepType), intake), nil
	}
	return dex.GoTo(sdkgo.StepRef[DealIntake](findOrganizationStepType), intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Link the person to the organization only when exactly one has the exact name."
type routeOrganization struct {
	dex.StepDefaultsNoWaitFor[pipedrive.SearchObjectsResult]
}

func (routeOrganization) GetStepType() string { return routeOrganizationStepType }

func (routeOrganization) Execute(ctx dex.Context, result pipedrive.SearchObjectsResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	var matchingIDs []string
	for _, organization := range result.Value.Objects {
		if strings.EqualFold(strings.TrimSpace(organization.Name), intake.Lead.Organization) {
			matchingIDs = append(matchingIDs, organization.ID)
		}
	}
	intake.OrganizationMatches = len(matchingIDs)
	if len(matchingIDs) == 1 {
		intake.OrganizationID = matchingIDs[0]
	}
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DealIntake](upsertLeadPersonStepType), intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Record the upserted person and list its open deal."
type recordLeadPerson struct {
	dex.StepDefaultsNoWaitFor[pipedrive.UpsertObjectResult]
}

func (recordLeadPerson) GetStepType() string { return recordLeadPersonStepType }

func (recordLeadPerson) Execute(ctx dex.Context, result pipedrive.UpsertObjectResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.Phase = PhasePersonUpserted
	intake.PersonID = result.Value.Object.ID
	intake.IsPersonCreated = result.Value.Created
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DealIntake](findOpenDealStepType), intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Complete for review when several persons share the email, Pipedrive rejected the person, or an earlier create is unconfirmed."
type recordUnresolvedLead struct {
	dex.StepDefaultsNoWaitFor[pipedrive.UpsertObjectResult]
}

func (recordUnresolvedLead) GetStepType() string { return recordUnresolvedLeadStepType }

func (recordUnresolvedLead) Execute(ctx dex.Context, result pipedrive.UpsertObjectResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.Phase, intake.ReviewStep, intake.ReviewBranch, intake.ReviewDetail = PhaseNeedsReview, upsertLeadPersonStepType, result.Branch, result.Failure
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Advance the open deal when one exists, or create the deal."
type routeOpenDeal struct {
	dex.StepDefaultsNoWaitFor[pipedrive.ListObjectsResult]
}

func (routeOpenDeal) GetStepType() string { return routeOpenDealStepType }

func (routeOpenDeal) Execute(ctx dex.Context, result pipedrive.ListObjectsResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Objects) == 0 {
		return dex.GoTo(sdkgo.StepRef[DealIntake](createDealStepType), intake), nil
	}
	deal := result.Value.Objects[0]
	intake.DealID = deal.ID
	intake.PreviousStageID = deal.StageID
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DealIntake](advanceDealStepType), intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Record the created deal and read it back."
type recordCreatedDeal struct {
	dex.StepDefaultsNoWaitFor[pipedrive.CreateObjectResult]
}

func (recordCreatedDeal) GetStepType() string { return recordCreatedDealStepType }

func (recordCreatedDeal) Execute(ctx dex.Context, result pipedrive.CreateObjectResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.Phase, intake.DealAction, intake.DealID, intake.StageID = PhaseDealWritten, DealActionCreated, result.Value.ID, result.Value.StageID
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DealIntake](readBackDealStepType), intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Record the advanced deal and read it back."
type recordAdvancedDeal struct {
	dex.StepDefaultsNoWaitFor[pipedrive.UpdateObjectResult]
}

func (recordAdvancedDeal) GetStepType() string { return recordAdvancedDealStepType }

func (recordAdvancedDeal) Execute(ctx dex.Context, result pipedrive.UpdateObjectResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.Phase, intake.DealAction, intake.StageID = PhaseDealWritten, DealActionAdvanced, result.Value.StageID
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DealIntake](readBackDealStepType), intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Complete for review when Pipedrive rejected the deal or its create is unconfirmed."
type recordUnresolvedDeal struct {
	dex.StepDefaultsNoWaitFor[pipedrive.CreateObjectResult]
}

func (recordUnresolvedDeal) GetStepType() string { return recordUnresolvedDealStepType }

func (recordUnresolvedDeal) Execute(ctx dex.Context, result pipedrive.CreateObjectResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.Phase, intake.ReviewStep, intake.ReviewBranch, intake.ReviewDetail = PhaseNeedsReview, createDealStepType, result.Branch, result.Failure
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(intake), nil
}

// dex:group group-id:pipedrive group-label:"Pipedrive"
// dex:explanation text:"Confirm the read-back stage and complete the intake."
type completeDealIntake struct {
	dex.StepDefaultsNoWaitFor[pipedrive.GetObjectResult]
	settings *Settings
}

func (completeDealIntake) GetStepType() string { return completeDealIntakeStepType }

func (step completeDealIntake) Execute(ctx dex.Context, result pipedrive.GetObjectResult) (*dex.StepDecision, error) {
	if result.Value.StageID != step.settings.Deal.StageID {
		return dex.ForceFail(fmt.Sprintf("Pipedrive deal %s reads back stage %q, not the configured stage", result.Value.ID, result.Value.StageID)), nil
	}
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.Phase = PhaseCompleted
	intake.StageID = result.Value.StageID
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(intake), nil
}

// ValidateLead trims the Start Flow input and checks it before any Pipedrive call.
func ValidateLead(input Input) (Input, error) {
	lead := Input{
		Email: strings.TrimSpace(input.Email), Name: strings.TrimSpace(input.Name), Organization: strings.TrimSpace(input.Organization),
		DealTitle: strings.TrimSpace(input.DealTitle), Source: strings.TrimSpace(input.Source),
	}
	address, err := mail.ParseAddress(lead.Email)
	if err != nil || address.Address != lead.Email || len(lead.Email) > maximumEmailLength {
		return Input{}, errors.New("email must be one plain email address")
	}
	if lead.Name == "" || lead.DealTitle == "" {
		return Input{}, errors.New("name and dealTitle are required")
	}
	for _, value := range []string{lead.Name, lead.Organization, lead.DealTitle, lead.Source} {
		if utf8.RuneCountInString(value) > maximumTextLength {
			return Input{}, fmt.Errorf("name, organization, dealTitle, and source are limited to %d characters", maximumTextLength)
		}
	}
	return lead, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
