// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leadqualification demonstrates every HubSpot CRM operation in one
// Flow started from Dex Web Start Flow: it upserts a lead contact by email,
// searches for the contact's open deal in the configured pipeline, moves that
// deal to the configured stage, and reads the deal back to confirm the stage.
package leadqualification

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "HubSpotLeadQualification"
	// ConnectionName is the static Dex Web connection for HubSpot.
	ConnectionName = "hubspot-crm"

	recordLeadStepType                = "RecordHubSpotLead"
	upsertLeadContactStepType         = "UpsertHubSpotLeadContact"
	recordLeadContactStepType         = "RecordHubSpotLeadContact"
	findOpenDealStepType              = "FindHubSpotOpenDeal"
	routeOpenDealStepType             = "RouteHubSpotOpenDeal"
	advanceOpenDealStepType           = "AdvanceHubSpotOpenDeal"
	recordAdvancedDealStepType        = "RecordAdvancedHubSpotDeal"
	readBackDealStepType              = "ReadBackHubSpotDeal"
	completeLeadQualificationStepType = "CompleteHubSpotLeadQualification"
	recordRejectedLeadStepType        = "RecordRejectedHubSpotLead"

	maximumEmailLength = 254
	maximumNameLength  = 200
)

var qualificationAttribute = dex.DefineAttribute[LeadQualification]("hubspot-lead-qualification")

// Phase is the durable progress of one lead qualification.
type Phase string

const (
	// PhaseRecorded means the lead was validated and recorded.
	PhaseRecorded Phase = "recorded"
	// PhaseContactUpserted means HubSpot holds the lead contact.
	PhaseContactUpserted Phase = "contactUpserted"
	// PhaseDealAdvanceRequested means the open deal's stage update was sent.
	PhaseDealAdvanceRequested Phase = "dealAdvanceRequested"
	// PhaseDealAdvanced means the read-back confirmed the open deal's new stage.
	PhaseDealAdvanced Phase = "dealAdvanced"
	// PhaseNoOpenDeal means the contact has no open deal in the configured pipeline.
	PhaseNoOpenDeal Phase = "noOpenDeal"
	// PhaseRejected means HubSpot rejected the contact upsert.
	PhaseRejected Phase = "rejected"
)

// Input contains the lead fields entered in Dex Web Start Flow.
type Input struct {
	// Email is the lead's plain email address, HubSpot's unique contact identifier.
	Email string `json:"email"`
	// FirstName optionally sets the contact's firstname property.
	FirstName string `json:"firstName,omitempty"`
	// LastName optionally sets the contact's lastname property.
	LastName string `json:"lastName,omitempty"`
	// Company optionally sets the contact's company name property.
	Company string `json:"company,omitempty"`
}

// LeadQualification is the Flow's durable state and completion output.
type LeadQualification struct {
	// Lead is the validated Start Flow input.
	Lead Input `json:"lead"`
	// Phase is the current or terminal progress.
	Phase Phase `json:"phase"`
	// ContactID is the HubSpot record ID of the upserted contact.
	ContactID string `json:"contactId,omitempty"`
	// IsContactCreated reports whether the answered upsert dispatch created the contact.
	IsContactCreated bool `json:"contactCreated"`
	// DealID is the record ID of the open deal found for the contact.
	DealID string `json:"dealId,omitempty"`
	// DealName is the open deal's name.
	DealName string `json:"dealName,omitempty"`
	// PreviousDealStageID is the open deal's stage before the update.
	PreviousDealStageID string `json:"previousDealStageId,omitempty"`
	// DealStageID is the deal stage HubSpot reported after the update.
	DealStageID string `json:"dealStageId,omitempty"`
	// Rejection is HubSpot's secret-safe rejection when the upsert was rejected.
	Rejection *sdkgo.Failure `json:"rejection,omitempty"`
}

// LeadOwnerConfiguration is the owner pick Dex Web saves for the upsert Step.
type LeadOwnerConfiguration struct {
	// OwnerID is the numeric HubSpot owner ID; blank leaves each contact's owner unchanged.
	OwnerID string `json:"ownerId"`
}

// QualifiedDealStageConfiguration is the pipeline and stage Dex Web saves for the update Step.
type QualifiedDealStageConfiguration struct {
	// PipelineID is the deal pipeline searched for the contact's open deal.
	PipelineID string `json:"pipelineId"`
	// StageID is the stage in that pipeline the open deal moves to.
	StageID string `json:"stageId"`
}

// Settings holds the operation configuration loaded once at startup.
type Settings struct {
	// LeadOwner is the optional owner assigned to upserted contacts.
	LeadOwner LeadOwnerConfiguration
	// QualifiedDealStage is the required pipeline and stage for open deals.
	QualifiedDealStage QualifiedDealStageConfiguration
}

// Flow qualifies one HubSpot lead.
type Flow struct {
	dex.FlowDefaults
	connection hubspot.Connection
	settings   Settings
}

// NewFlow binds the HubSpot Connection and startup settings. It fails when
// the qualified deal pipeline or stage is blank.
func NewFlow(connection hubspot.Connection, settings *Settings) (*Flow, error) {
	if settings == nil {
		return nil, errors.New("HubSpot lead qualification settings are required")
	}
	if strings.TrimSpace(settings.QualifiedDealStage.PipelineID) == "" || strings.TrimSpace(settings.QualifiedDealStage.StageID) == "" {
		return nil, errors.New("HubSpot lead qualification needs a qualified deal pipeline and stage")
	}
	return &Flow{connection: connection, settings: *settings}, nil
}

// LeadOwnerConfigurationRef identifies the owner pick of the upsert Step.
func LeadOwnerConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: hubspot.ConnectorID, ConnectionName: ConnectionName, OperationID: "upsertObject",
		FlowType: FlowType, StepType: upsertLeadContactStepType,
	}
}

// QualifiedDealStageConfigurationRef identifies the pipeline and stage pick of the update Step.
func QualifiedDealStageConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: hubspot.ConnectorID, ConnectionName: ConnectionName, OperationID: "updateObject",
		FlowType: FlowType, StepType: advanceOpenDealStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and HubSpot Connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordLead{}),
		dex.DefineStep(hubspot.NewUpsertObjectStep(hubspot.UpsertObjectStepConfig[LeadQualification]{
			StepType: upsertLeadContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "hubspot", GroupLabel: "HubSpot",
				Explanation: "Create or update the lead contact identified by its email address.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "leadOwner", UnitID: hubspot.UIUnitOwnerPicker, Label: "Lead owner",
				Description: "Select the HubSpot owner assigned to each upserted lead contact; the picker stores the owner's numeric ID in hubspot_owner_id, and blank leaves the contact's current owner unchanged.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: hubspot.UIOwnerPickerPortOwnerID, JSONPointer: "/ownerId"}},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpsertLeadContactInput,
			Upserted:         sdkgo.GoTo(recordLeadContact{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedLead{}),
		})),
		dex.DefineStep(recordLeadContact{}),
		dex.DefineStep(hubspot.NewSearchObjectsStep(hubspot.SearchObjectsStepConfig[LeadQualification]{
			StepType: findOpenDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "hubspot", GroupLabel: "HubSpot",
				Explanation: "Search the contact's most recently modified open deal in the configured pipeline.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToFindOpenDealInput,
			Searched: sdkgo.GoTo(routeOpenDeal{}),
		})),
		dex.DefineStep(routeOpenDeal{}),
		dex.DefineStep(hubspot.NewUpdateObjectStep(hubspot.UpdateObjectStepConfig[LeadQualification]{
			StepType: advanceOpenDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "hubspot", GroupLabel: "HubSpot",
				Explanation: "Move the open deal to the configured qualified stage.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "qualifiedStage", UnitID: hubspot.UIUnitDealStagePicker, Label: "Qualified deal stage", Required: true,
				Description: "Select the deal pipeline searched for the contact's open deal and the stage that deal moves to; the picker stores the stable pipeline ID and stage ID, and the Worker does not start until both are saved.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: hubspot.UIDealStagePickerPortPipelineID, JSONPointer: "/pipelineId"},
					{Port: hubspot.UIDealStagePickerPortStageID, JSONPointer: "/stageId"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToAdvanceOpenDealInput,
			Updated: sdkgo.GoTo(recordAdvancedDeal{}),
		})),
		dex.DefineStep(recordAdvancedDeal{}),
		dex.DefineStep(hubspot.NewGetObjectStep(hubspot.GetObjectStepConfig[LeadQualification]{
			StepType: readBackDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "hubspot", GroupLabel: "HubSpot",
				Explanation: "Read the deal back to confirm HubSpot stored the new stage.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadBackDealInput,
			Found: sdkgo.GoTo(completeLeadQualification{settings: &flow.settings}),
		})),
		dex.DefineStep(completeLeadQualification{settings: &flow.settings}),
		dex.DefineStep(recordRejectedLead{}),
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{qualificationAttribute}}
}

// GetDexSummary returns the lead qualification state.
//
// dex:field attribute-key:hubspot-lead-qualification value-type:json editable:false description:"Lead, contact, deal, and phase"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	qualification, err := optionalQualification(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hubspot-lead-qualification": qualification}}, nil
}

// GetDexDisplay returns the lead qualification state.
//
// dex:field attribute-key:hubspot-lead-qualification value-type:json editable:false description:"Lead email, HubSpot contact and deal IDs, deal stage, and phase"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	qualification, err := optionalQualification(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hubspot-lead-qualification": qualification}}, nil
}

// MapToUpsertLeadContactInput upserts the contact by email and sets only the supplied fields and owner.
func (flow *Flow) MapToUpsertLeadContactInput(qualification LeadQualification) hubspot.UpsertObjectInput {
	properties := map[string]string{}
	for name, value := range map[string]string{
		"firstname": qualification.Lead.FirstName, "lastname": qualification.Lead.LastName,
		"company": qualification.Lead.Company, "hubspot_owner_id": flow.settings.LeadOwner.OwnerID,
	} {
		if value != "" {
			properties[name] = value
		}
	}
	return hubspot.UpsertObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: qualification.Lead.Email, Properties: properties,
	}
}

// MapToFindOpenDealInput searches one open deal associated with the contact in the configured pipeline.
func (flow *Flow) MapToFindOpenDealInput(qualification LeadQualification) hubspot.SearchObjectsInput {
	return hubspot.SearchObjectsInput{
		ObjectType: hubspot.ObjectTypeDeals,
		FilterGroups: []hubspot.SearchFilterGroup{{Filters: []hubspot.SearchFilter{
			{PropertyName: "associations.contact", Operator: hubspot.FilterOperatorEqual, Value: qualification.ContactID},
			{PropertyName: "pipeline", Operator: hubspot.FilterOperatorEqual, Value: flow.settings.QualifiedDealStage.PipelineID},
			{PropertyName: "hs_is_closed", Operator: hubspot.FilterOperatorEqual, Value: "false"},
		}}},
		Sort:       &hubspot.SearchSort{PropertyName: "hs_lastmodifieddate", Direction: hubspot.SortDirectionDescending},
		Properties: []string{"dealname", "dealstage", "pipeline"},
		Limit:      1,
	}
}

// MapToAdvanceOpenDealInput sets the configured stage on the open deal.
func (flow *Flow) MapToAdvanceOpenDealInput(qualification LeadQualification) hubspot.UpdateObjectInput {
	return hubspot.UpdateObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: qualification.DealID,
		Properties: map[string]string{"dealstage": flow.settings.QualifiedDealStage.StageID},
	}
}

// MapToReadBackDealInput reads the deal's stage and pipeline after the update.
func (*Flow) MapToReadBackDealInput(qualification LeadQualification) hubspot.GetObjectInput {
	return hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: qualification.DealID, Properties: []string{"dealname", "dealstage", "pipeline"},
	}
}

func optionalQualification(ctx dex.Context) (LeadQualification, error) {
	qualification, err := qualificationAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return LeadQualification{}, nil
	}
	return qualification, err
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Validate and record the lead before calling HubSpot."
type recordLead struct {
	dex.StepDefaults
}

func (recordLead) GetStepType() string { return recordLeadStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordLead) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordLead) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	lead, err := validateLead(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	qualification := LeadQualification{Lead: lead, Phase: PhaseRecorded}
	if err := qualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LeadQualification](upsertLeadContactStepType), qualification), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Record the upserted contact and search for its open deal."
type recordLeadContact struct {
	dex.StepDefaultsNoWaitFor[hubspot.UpsertObjectResult]
}

func (recordLeadContact) GetStepType() string { return recordLeadContactStepType }

func (recordLeadContact) Execute(ctx dex.Context, result hubspot.UpsertObjectResult) (*dex.StepDecision, error) {
	qualification, err := qualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.Phase = PhaseContactUpserted
	qualification.ContactID = result.Value.Object.ID
	qualification.IsContactCreated = result.Value.Created
	if err := qualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LeadQualification](findOpenDealStepType), qualification), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Advance the open deal when one exists, or complete without a deal."
type routeOpenDeal struct {
	dex.StepDefaultsNoWaitFor[hubspot.SearchObjectsResult]
}

func (routeOpenDeal) GetStepType() string { return routeOpenDealStepType }

func (routeOpenDeal) Execute(ctx dex.Context, result hubspot.SearchObjectsResult) (*dex.StepDecision, error) {
	qualification, err := qualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Objects) == 0 {
		qualification.Phase = PhaseNoOpenDeal
		if err := qualificationAttribute.Set(ctx, qualification); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(qualification), nil
	}
	deal := result.Value.Objects[0]
	qualification.Phase = PhaseDealAdvanceRequested
	qualification.DealID = deal.ID
	qualification.DealName = deal.Properties["dealname"]
	qualification.PreviousDealStageID = deal.Properties["dealstage"]
	if err := qualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LeadQualification](advanceOpenDealStepType), qualification), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Record the stage HubSpot returned and read the deal back."
type recordAdvancedDeal struct {
	dex.StepDefaultsNoWaitFor[hubspot.UpdateObjectResult]
}

func (recordAdvancedDeal) GetStepType() string { return recordAdvancedDealStepType }

func (recordAdvancedDeal) Execute(ctx dex.Context, result hubspot.UpdateObjectResult) (*dex.StepDecision, error) {
	qualification, err := qualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.DealStageID = result.Value.Properties["dealstage"]
	if err := qualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LeadQualification](readBackDealStepType), qualification), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Confirm the read-back stage and complete the qualification."
type completeLeadQualification struct {
	dex.StepDefaultsNoWaitFor[hubspot.GetObjectResult]
	settings *Settings
}

func (completeLeadQualification) GetStepType() string { return completeLeadQualificationStepType }

func (step completeLeadQualification) Execute(ctx dex.Context, result hubspot.GetObjectResult) (*dex.StepDecision, error) {
	storedStageID := result.Value.Properties["dealstage"]
	if storedStageID != step.settings.QualifiedDealStage.StageID {
		return dex.ForceFail(fmt.Sprintf("HubSpot deal %s reads back stage %q, not the configured stage", result.Value.ID, storedStageID)), nil
	}
	qualification, err := qualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.Phase = PhaseDealAdvanced
	qualification.DealStageID = storedStageID
	if err := qualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(qualification), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Record HubSpot's secret-safe rejection of the contact and complete."
type recordRejectedLead struct {
	dex.StepDefaultsNoWaitFor[hubspot.UpsertObjectResult]
}

func (recordRejectedLead) GetStepType() string { return recordRejectedLeadStepType }

func (recordRejectedLead) Execute(ctx dex.Context, result hubspot.UpsertObjectResult) (*dex.StepDecision, error) {
	qualification, err := qualificationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	qualification.Phase = PhaseRejected
	qualification.Rejection = result.Failure
	if err := qualificationAttribute.Set(ctx, qualification); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(qualification), nil
}

func validateLead(input Input) (Input, error) {
	lead := Input{
		Email: strings.TrimSpace(input.Email), FirstName: strings.TrimSpace(input.FirstName),
		LastName: strings.TrimSpace(input.LastName), Company: strings.TrimSpace(input.Company),
	}
	address, err := mail.ParseAddress(lead.Email)
	if err != nil || address.Address != lead.Email || len(lead.Email) > maximumEmailLength {
		return Input{}, errors.New("email must be one plain email address")
	}
	for _, value := range []string{lead.FirstName, lead.LastName, lead.Company} {
		if len([]rune(value)) > maximumNameLength {
			return Input{}, fmt.Errorf("firstName, lastName, and company are limited to %d characters", maximumNameLength)
		}
	}
	return lead, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
