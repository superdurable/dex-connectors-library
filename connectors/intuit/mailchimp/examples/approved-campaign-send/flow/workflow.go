// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package approvedcampaignsend demonstrates every Mailchimp operation in one Flow started from Dex Web Start
// Flow: enroll a few contacts in an audience and tag them, without touching anyone who unsubscribed, then
// send an existing draft campaign only after a person approves it, and at most once.
package approvedcampaignsend

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "MailchimpApprovedCampaignSend"
	// ConnectionName is the static Dex Web connection for Mailchimp.
	ConnectionName = "mailchimp-audience"
	// SendCampaignPermission is required by every Action that can lead to a campaign send or decline it.
	SendCampaignPermission = "mailchimp-campaign.send"
	// MaxContacts bounds the contacts one Flow enrolls.
	MaxContacts = 25

	recordCampaignLaunchStepType    = "RecordCampaignLaunch"
	readLaunchContactStepType       = "ReadLaunchContact"
	planLaunchContactStepType       = "PlanLaunchContact"
	upsertLaunchContactStepType     = "UpsertLaunchContact"
	recordUpsertedContactStepType   = "RecordUpsertedContact"
	recordRejectedContactStepType   = "RecordRejectedContact"
	tagLaunchContactStepType        = "TagLaunchContact"
	recordTaggedContactStepType     = "RecordTaggedContact"
	countSubscribedAudienceStepType = "CountSubscribedAudience"
	awaitSendApprovalStepType       = "AwaitSendApproval"
	sendLaunchCampaignStepType      = "SendLaunchCampaign"
	recordCampaignSentStepType      = "RecordCampaignSent"
	recordCampaignNotSentStepType   = "RecordCampaignNotSent"
	recordUncertainSendStepType     = "RecordUncertainSend"
	recordDeclinedSendStepType      = "RecordDeclinedSend"

	maximumTagBytes = 100
)

// Campaign launch phases stored in the mailchimp-campaign-launch-phase Attribute.
const (
	// PhaseEnrolling means the Flow is reading, upserting, and tagging the contacts.
	PhaseEnrolling = "enrolling"
	// PhaseAwaitingApproval means the contacts are enrolled and a person must approve or decline the send.
	PhaseAwaitingApproval = "awaitingApproval"
	// PhaseSending means a sendCampaign Step is about to run or running.
	PhaseSending = "sending"
	// PhaseSent means Mailchimp accepted this Flow's send.
	PhaseSent = "sent"
	// PhaseAlreadySent means Mailchimp showed the campaign sending or sent, so this Flow sent nothing more.
	PhaseAlreadySent = "alreadySent"
	// PhaseNotSent means the campaign could not be sent: not a draft, another audience, missing, or rejected.
	PhaseNotSent = "notSent"
	// PhaseNeedsReview means the send was dispatched with an unknown outcome; a person checks Mailchimp.
	PhaseNeedsReview = "needsReview"
	// PhaseDeclined means a person declined the send.
	PhaseDeclined = "declined"
)

// Contact actions recorded per contact.
const (
	// ContactEnrolled means the contact was upserted and tagged.
	ContactEnrolled = "enrolled"
	// ContactSuppressed means the contact unsubscribed, was cleaned, or was archived, so the Flow left it untouched.
	ContactSuppressed = "suppressed"
	// ContactRejected means Mailchimp rejected the upsert, such as a contact in a compliance state.
	ContactRejected = "rejected"
)

var (
	campaignLaunchPhaseAttribute = dex.DefineAttribute[string]("mailchimp-campaign-launch-phase")
	campaignLaunchAttribute      = dex.DefineAttribute[CampaignLaunch]("mailchimp-campaign-launch")

	launchLocks = []dex.AttributeLock{dex.LockAttribute(campaignLaunchPhaseAttribute), dex.LockAttribute(campaignLaunchAttribute)}

	errPersonRequired = errors.New("a name of the person acting is required, on one line")
)

// Input is the launch entered in Dex Web Start Flow.
type Input struct {
	// ListID is the Mailchimp audience ID, such as 57afe96172.
	ListID string `json:"listId"`
	// CampaignID is the draft campaign to send after approval, such as 42694e9e57.
	CampaignID string `json:"campaignId"`
	// Tag is added to every enrolled contact, such as spring-launch.
	Tag string `json:"tag"`
	// StatusIfNew is the status of a contact Mailchimp creates: pending, which sends Mailchimp's
	// confirmation email, or subscribed for contacts who already gave permission.
	StatusIfNew mailchimp.MemberStatus `json:"statusIfNew"`
	// Contacts are 1 to 25 people to enroll.
	Contacts []ContactInput `json:"contacts"`
}

// ContactInput is one person to enroll.
type ContactInput struct {
	// EmailAddress is the person's bare email address.
	EmailAddress string `json:"emailAddress"`
	// FirstName is written to the FNAME merge field; blank leaves it unchanged.
	FirstName string `json:"firstName,omitempty"`
	// LastName is written to the LNAME merge field; blank leaves it unchanged.
	LastName string `json:"lastName,omitempty"`
}

// LaunchRequest is the validated launch every later Step reads.
type LaunchRequest struct {
	// ListID is the audience ID.
	ListID string `json:"listId"`
	// CampaignID is the campaign to send.
	CampaignID string `json:"campaignId"`
	// Tag is added to every enrolled contact.
	Tag string `json:"tag"`
	// StatusIfNew is the status of a contact Mailchimp creates.
	StatusIfNew mailchimp.MemberStatus `json:"statusIfNew"`
	// Contacts are the validated contacts.
	Contacts []ContactInput `json:"contacts"`
}

// ContactLookup is one getMember call.
type ContactLookup struct {
	// ListID is the audience ID.
	ListID string `json:"listId"`
	// EmailAddress is the contact's address.
	EmailAddress string `json:"emailAddress"`
}

// ContactUpsert is one upsertMember call.
type ContactUpsert struct {
	// ListID is the audience ID.
	ListID string `json:"listId"`
	// Contact is the person to write.
	Contact ContactInput `json:"contact"`
	// StatusIfNew is the status of a contact Mailchimp creates.
	StatusIfNew mailchimp.MemberStatus `json:"statusIfNew"`
}

// ContactTagging is one updateMemberTags call.
type ContactTagging struct {
	// ListID is the audience ID.
	ListID string `json:"listId"`
	// EmailAddress is the contact's address.
	EmailAddress string `json:"emailAddress"`
	// Tag is the tag to add.
	Tag string `json:"tag"`
}

// AudienceCount is one listMembers call that counts the audience's subscribed contacts.
type AudienceCount struct {
	// ListID is the audience ID.
	ListID string `json:"listId"`
}

// CampaignSend is one sendCampaign call.
type CampaignSend struct {
	// CampaignID is the campaign to send.
	CampaignID string `json:"campaignId"`
	// ListID is the audience the approved campaign must target.
	ListID string `json:"listId"`
}

// SendDecision records who approved, declined, or rechecked the send.
type SendDecision struct {
	// DecidedBy names the person.
	DecidedBy string `json:"decidedBy"`
	// Note is the person's note, or empty.
	Note string `json:"note,omitempty"`
}

// ContactOutcome is what the Flow did with one contact.
type ContactOutcome struct {
	// EmailAddress is the contact's address.
	EmailAddress string `json:"emailAddress"`
	// SubscriberHash is Mailchimp's MD5 subscriber hash of the address.
	SubscriberHash string `json:"subscriberHash"`
	// Action is enrolled, suppressed, or rejected.
	Action string `json:"action"`
	// PreviousStatus is the status before the Flow, or empty for a new contact.
	PreviousStatus mailchimp.MemberStatus `json:"previousStatus,omitempty"`
	// Status is the status Mailchimp returned after the upsert.
	Status mailchimp.MemberStatus `json:"status,omitempty"`
	// FailureMessage is the connector's safe reason for a rejected contact.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// UncertainSend records a dispatched send whose outcome Mailchimp did not confirm.
type UncertainSend struct {
	// CallID is the connector call identity of the uncertain send.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the connector's safe description, which never holds Mailchimp text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// CampaignLaunch is the Flow's durable record of one launch.
type CampaignLaunch struct {
	// Request is the validated launch.
	Request LaunchRequest `json:"request"`
	// Phase mirrors the mailchimp-campaign-launch-phase Attribute.
	Phase string `json:"phase"`
	// NextContactIndex is the index of the contact being enrolled.
	NextContactIndex int `json:"nextContactIndex"`
	// PreviousStatus is the current contact's status before the upsert, or empty when new.
	PreviousStatus mailchimp.MemberStatus `json:"previousStatus,omitempty"`
	// Contacts are the finished contacts in order.
	Contacts []ContactOutcome `json:"contacts,omitempty"`
	// SubscribedMemberCount is the audience's subscribed contacts when approval was requested.
	SubscribedMemberCount int `json:"subscribedMemberCount"`
	// Approval names who approved the send.
	Approval *SendDecision `json:"approval,omitempty"`
	// Decline names who declined the send.
	Decline *SendDecision `json:"decline,omitempty"`
	// SendExecutions counts sendCampaign Step executions: the approved send plus each recheck.
	SendExecutions int `json:"sendExecutions"`
	// Campaign is the campaign as sendCampaign read it.
	Campaign *mailchimp.Campaign `json:"campaign,omitempty"`
	// NotSentReason is the connector's safe reason for a campaign that was not sent.
	NotSentReason string `json:"notSentReason,omitempty"`
	// UncertainSend describes the latest uncertain send while review is pending.
	UncertainSend *UncertainSend `json:"uncertainSend,omitempty"`
}

// SendDecisionInput is the form of the approve, decline, and recheck Actions.
type SendDecisionInput struct {
	// DecidedBy names the person acting.
	DecidedBy string `json:"decidedBy"`
	// Note is an optional note; nil adds none.
	Note *string `json:"note,omitempty"`
}

// Flow enrolls contacts in a Mailchimp audience and sends a draft campaign after approval.
type Flow struct {
	dex.FlowDefaults
	connection mailchimp.Connection
}

// NewFlow binds the Mailchimp Connection at registration time.
func NewFlow(connection mailchimp.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Mailchimp connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCampaignLaunch{}),
		dex.DefineStep(mailchimp.NewGetMemberStep(mailchimp.GetMemberStepConfig[ContactLookup]{
			StepType: readLaunchContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mailchimp", GroupLabel: "Mailchimp",
				Explanation: "Read the contact by the MD5 hash of its lowercased address to see whether it unsubscribed.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMemberInput,
			Found:    sdkgo.GoTo(planLaunchContact{}),
			NotFound: sdkgo.GoTo(planLaunchContact{}),
		})),
		dex.DefineStep(planLaunchContact{}),
		dex.DefineStep(mailchimp.NewUpsertMemberStep(mailchimp.UpsertMemberStepConfig[ContactUpsert]{
			StepType: upsertLaunchContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mailchimp", GroupLabel: "Mailchimp",
				Explanation: "Add or update the contact with statusIfNew only, so an existing contact keeps its status; a repeated PUT writes the same values.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpsertMemberInput,
			Upserted:         sdkgo.GoTo(recordUpsertedContact{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedContact{}),
		})),
		dex.DefineStep(recordUpsertedContact{}),
		dex.DefineStep(recordRejectedContact{}),
		dex.DefineStep(mailchimp.NewUpdateMemberTagsStep(mailchimp.UpdateMemberTagsStepConfig[ContactTagging]{
			StepType: tagLaunchContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mailchimp", GroupLabel: "Mailchimp",
				Explanation: "Declare the launch tag active on the contact; a repeated declaration leaves the same tags.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateMemberTagsInput,
			Updated: sdkgo.GoTo(recordTaggedContact{}),
		})),
		dex.DefineStep(recordTaggedContact{}),
		dex.DefineStep(mailchimp.NewListMembersStep(mailchimp.ListMembersStepConfig[AudienceCount]{
			StepType: countSubscribedAudienceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mailchimp", GroupLabel: "Mailchimp",
				Explanation: "Count the audience's subscribed contacts for the approver with a one-contact page.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListMembersInput,
			Listed: sdkgo.GoTo(awaitSendApproval{}),
		})),
		dex.DefineStep(awaitSendApproval{}),
		dex.DefineStep(mailchimp.NewSendCampaignStep(mailchimp.SendCampaignStepConfig[CampaignSend]{
			StepType: sendLaunchCampaignStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "mailchimp", GroupLabel: "Mailchimp",
				Explanation: "Read the approved campaign and send it only if it is still a draft for this audience, at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSendCampaignInput,
			Sent:             sdkgo.GoTo(recordCampaignSent{}),
			AlreadySent:      sdkgo.GoTo(recordCampaignSent{}),
			NotSendable:      sdkgo.GoTo(recordCampaignNotSent{}),
			NotFound:         sdkgo.GoTo(recordCampaignNotSent{}),
			ProviderRejected: sdkgo.GoTo(recordCampaignNotSent{}),
			Uncertain:        sdkgo.GoTo(recordUncertainSend{}),
		})),
		dex.DefineStep(recordCampaignSent{}),
		dex.DefineStep(recordCampaignNotSent{}),
		dex.DefineStep(recordUncertainSend{}),
		dex.DefineStep(recordDeclinedSend{}),
	}
}

// GetRPCs returns the approval and review Actions, the record read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ApproveCampaignSend, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Approve campaign send",
				dex.WhenAttributeMatches(campaignLaunchPhaseAttribute, dex.AttributeMatchEqual(PhaseAwaitingApproval)),
				dex.ActionRequiresPermission(SendCampaignPermission),
			),
			LockAttributes: launchLocks,
		}),
		dex.DefineRPC(flow.DeclineCampaignSend, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Decline campaign send",
				dex.WhenAttributeMatches(campaignLaunchPhaseAttribute, dex.AttributeMatchEqual(PhaseAwaitingApproval)),
				dex.ActionRequiresPermission(SendCampaignPermission),
			),
			LockAttributes: launchLocks,
		}),
		dex.DefineRPC(flow.RecheckCampaignSend, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Recheck campaign and send if still a draft",
				dex.WhenAttributeMatches(campaignLaunchPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReview)),
				dex.ActionRequiresPermission(SendCampaignPermission),
			),
			LockAttributes: launchLocks,
		}),
		dex.DefineRPC(flow.GetCampaignLaunch, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and record Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignLaunchPhaseAttribute, campaignLaunchAttribute}}
}

// MapToGetMemberInput reads one contact.
func MapToGetMemberInput(lookup ContactLookup) mailchimp.GetMemberInput {
	return mailchimp.GetMemberInput{ListID: lookup.ListID, EmailAddress: lookup.EmailAddress}
}

// MapToUpsertMemberInput writes the contact with statusIfNew and no status, so Mailchimp never
// changes an existing contact's status, and with FNAME and LNAME when given.
func MapToUpsertMemberInput(upsert ContactUpsert) mailchimp.UpsertMemberInput {
	mergeFields := map[string]any{}
	if upsert.Contact.FirstName != "" {
		mergeFields["FNAME"] = upsert.Contact.FirstName
	}
	if upsert.Contact.LastName != "" {
		mergeFields["LNAME"] = upsert.Contact.LastName
	}
	if len(mergeFields) == 0 {
		mergeFields = nil
	}
	return mailchimp.UpsertMemberInput{
		ListID: upsert.ListID, EmailAddress: upsert.Contact.EmailAddress, StatusIfNew: upsert.StatusIfNew, MergeFields: mergeFields,
	}
}

// MapToUpdateMemberTagsInput adds the launch tag.
func MapToUpdateMemberTagsInput(tagging ContactTagging) mailchimp.UpdateMemberTagsInput {
	return mailchimp.UpdateMemberTagsInput{ListID: tagging.ListID, EmailAddress: tagging.EmailAddress, AddTags: []string{tagging.Tag}}
}

// MapToListMembersInput reads one subscribed contact, whose page carries the subscribed total.
func MapToListMembersInput(count AudienceCount) mailchimp.ListMembersInput {
	return mailchimp.ListMembersInput{ListID: count.ListID, Status: mailchimp.MemberStatusSubscribed, PageSize: 1}
}

// MapToSendCampaignInput sends the approved campaign only while it targets the launch audience.
func MapToSendCampaignInput(send CampaignSend) mailchimp.SendCampaignInput {
	return mailchimp.SendCampaignInput{CampaignID: send.CampaignID, ExpectedListID: send.ListID}
}

// IsSuppressedStatus reports a contact the Flow must not touch: it unsubscribed, bounced, or was archived.
func IsSuppressedStatus(status mailchimp.MemberStatus) bool {
	return status == mailchimp.MemberStatusUnsubscribed || status == mailchimp.MemberStatusCleaned || status == mailchimp.MemberStatusArchived
}

// ApproveCampaignSend sends the campaign once. Repeating it, or approving after another person did,
// changes nothing, because only the awaitingApproval phase schedules the send.
//
// dex:input field-name:decidedBy value-type:string source:user required:true description:"Name of the person approving the send"
// dex:input field-name:note value-type:string source:user required:false description:"Optional approval note"
func (*Flow) ApproveCampaignSend(ctx dex.Context, input SendDecisionInput) (*dex.RPCResult[dex.None], error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseAwaitingApproval {
		return &dex.RPCResult[dex.None]{}, nil
	}
	decision, err := BuildSendDecision(input)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Approval = PhaseSending, &decision
	record.SendExecutions++
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[CampaignSend](sendLaunchCampaignStepType), newCampaignSend(record))},
	}, nil
}

// DeclineCampaignSend completes the Flow without sending.
//
// dex:input field-name:decidedBy value-type:string source:user required:true description:"Name of the person declining the send"
// dex:input field-name:note value-type:string source:user required:false description:"Optional reason"
func (*Flow) DeclineCampaignSend(ctx dex.Context, input SendDecisionInput) (*dex.RPCResult[dex.None], error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseAwaitingApproval {
		return &dex.RPCResult[dex.None]{}, nil
	}
	decision, err := BuildSendDecision(input)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Decline = PhaseDeclined, &decision
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[SendDecision](recordDeclinedSendStepType), decision)},
	}, nil
}

// RecheckCampaignSend runs sendCampaign again as a new Step execution after an uncertain send. Its
// read selects alreadySent while Mailchimp shows the campaign sending or sent, so use it only after
// Mailchimp's Campaigns page shows the campaign still as a draft or as sent.
//
// dex:input field-name:decidedBy value-type:string source:user required:true description:"Name of the person who checked the campaign in Mailchimp"
// dex:input field-name:note value-type:string source:user required:false description:"What Mailchimp showed"
func (*Flow) RecheckCampaignSend(ctx dex.Context, input SendDecisionInput) (*dex.RPCResult[dex.None], error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseNeedsReview {
		return &dex.RPCResult[dex.None]{}, nil
	}
	if _, err := BuildSendDecision(input); err != nil {
		return nil, err
	}
	record.Phase, record.UncertainSend = PhaseSending, nil
	record.SendExecutions++
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[CampaignSend](sendLaunchCampaignStepType), newCampaignSend(record))},
	}, nil
}

// GetCampaignLaunch returns the current record.
func (*Flow) GetCampaignLaunch(ctx dex.Context, _ dex.None) (*dex.RPCResult[CampaignLaunch], error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[CampaignLaunch]{Output: record}, nil
}

// GetDexSummary returns the phase and record for Dex Web lists.
//
// dex:field attribute-key:mailchimp-campaign-launch-phase value-type:string editable:false description:"Campaign launch phase"
// dex:field attribute-key:mailchimp-campaign-launch value-type:json editable:false description:"Audience, campaign, contacts, approval, and send outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, campaignLaunchPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, campaignLaunchAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"mailchimp-campaign-launch-phase": phase,
		"mailchimp-campaign-launch":       record,
	}}, nil
}

// GetDexDisplay returns the phase and record for the Dex Web run view.
//
// dex:field attribute-key:mailchimp-campaign-launch-phase value-type:string editable:false description:"Campaign launch phase" ui-slot:status
// dex:field attribute-key:mailchimp-campaign-launch value-type:json editable:false description:"Each contact's outcome, the subscribed count the approver saw, the approval, and the campaign as read before the send"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, campaignLaunchPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, campaignLaunchAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"mailchimp-campaign-launch-phase": phase,
		"mailchimp-campaign-launch":       record,
	}}, nil
}

// BuildLaunchRequest validates Start Flow input so no connector Step receives an unusable launch.
func BuildLaunchRequest(input Input) (LaunchRequest, error) {
	request := LaunchRequest{
		ListID: strings.TrimSpace(input.ListID), CampaignID: strings.TrimSpace(input.CampaignID),
		Tag: strings.TrimSpace(input.Tag), StatusIfNew: input.StatusIfNew,
	}
	switch {
	case !isMailchimpID(request.ListID):
		return LaunchRequest{}, errors.New("listId must be the Mailchimp audience ID, letters and digits such as 57afe96172")
	case !isMailchimpID(request.CampaignID):
		return LaunchRequest{}, errors.New("campaignId must be the Mailchimp campaign ID, letters and digits such as 42694e9e57")
	case request.Tag == "" || len(request.Tag) > maximumTagBytes || strings.ContainsAny(request.Tag, "\r\n\t"):
		return LaunchRequest{}, fmt.Errorf("tag is required: one line of at most %d bytes, such as spring-launch", maximumTagBytes)
	case request.StatusIfNew != mailchimp.MemberStatusPending && request.StatusIfNew != mailchimp.MemberStatusSubscribed:
		return LaunchRequest{}, errors.New("statusIfNew must be pending, which sends Mailchimp's confirmation email, or subscribed for contacts who gave permission")
	case len(input.Contacts) == 0 || len(input.Contacts) > MaxContacts:
		return LaunchRequest{}, fmt.Errorf("contacts must hold 1 to %d people", MaxContacts)
	}
	seen := map[string]bool{}
	for _, contact := range input.Contacts {
		cleaned := ContactInput{
			EmailAddress: strings.TrimSpace(contact.EmailAddress), FirstName: strings.TrimSpace(contact.FirstName), LastName: strings.TrimSpace(contact.LastName),
		}
		address, err := mail.ParseAddress(cleaned.EmailAddress)
		if err != nil || address.Name != "" || address.Address != cleaned.EmailAddress {
			return LaunchRequest{}, fmt.Errorf("contact email address %q must be one bare address such as jane@example.com", contact.EmailAddress)
		}
		if strings.ContainsAny(cleaned.FirstName+cleaned.LastName, "\r\n") {
			return LaunchRequest{}, fmt.Errorf("contact %s names must be one line", cleaned.EmailAddress)
		}
		hash := mailchimp.SubscriberHash(cleaned.EmailAddress)
		if seen[hash] {
			return LaunchRequest{}, fmt.Errorf("contact %s appears more than once; Mailchimp ignores letter case", cleaned.EmailAddress)
		}
		seen[hash] = true
		request.Contacts = append(request.Contacts, cleaned)
	}
	return request, nil
}

// BuildSendDecision validates an Action form.
func BuildSendDecision(input SendDecisionInput) (SendDecision, error) {
	decision := SendDecision{DecidedBy: strings.TrimSpace(input.DecidedBy)}
	if input.Note != nil {
		decision.Note = strings.TrimSpace(*input.Note)
	}
	if decision.DecidedBy == "" || strings.ContainsAny(decision.DecidedBy, "\r\n") {
		return SendDecision{}, errPersonRequired
	}
	return decision, nil
}

// dex:group group-id:enrollment group-label:"Enrollment"
// dex:explanation text:"Validate the launch and record it before calling Mailchimp."
type recordCampaignLaunch struct {
	dex.StepDefaults
}

func (recordCampaignLaunch) GetStepType() string { return recordCampaignLaunchStepType }

func (recordCampaignLaunch) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordCampaignLaunch) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCampaignLaunch) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildLaunchRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := CampaignLaunch{Request: request, Phase: PhaseEnrolling}
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ContactLookup](readLaunchContactStepType), newContactLookup(record)), nil
}

// dex:group group-id:enrollment group-label:"Enrollment"
// dex:explanation text:"Leave an unsubscribed, cleaned, or archived contact untouched; otherwise upsert it."
type planLaunchContact struct {
	dex.StepDefaultsNoWaitFor[mailchimp.GetMemberResult]
}

func (planLaunchContact) GetStepType() string { return planLaunchContactStepType }

func (planLaunchContact) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (planLaunchContact) Execute(ctx dex.Context, result mailchimp.GetMemberResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contact := record.Request.Contacts[record.NextContactIndex]
	record.PreviousStatus = ""
	if result.Branch == mailchimp.GetMemberBranchFound {
		record.PreviousStatus = result.Value.Status
	}
	if IsSuppressedStatus(record.PreviousStatus) {
		record.Contacts = append(record.Contacts, ContactOutcome{
			EmailAddress: contact.EmailAddress, SubscriberHash: mailchimp.SubscriberHash(contact.EmailAddress), Action: ContactSuppressed,
			PreviousStatus: record.PreviousStatus, Status: record.PreviousStatus,
		})
		record.NextContactIndex++
		if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		if record.NextContactIndex < len(record.Request.Contacts) {
			return dex.GoTo(sdkgo.StepRef[ContactLookup](readLaunchContactStepType), newContactLookup(record)), nil
		}
		return dex.GoTo(sdkgo.StepRef[AudienceCount](countSubscribedAudienceStepType), AudienceCount{ListID: record.Request.ListID}), nil
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ContactUpsert](upsertLaunchContactStepType), ContactUpsert{
		ListID: record.Request.ListID, Contact: contact, StatusIfNew: record.Request.StatusIfNew,
	}), nil
}

// dex:group group-id:enrollment group-label:"Enrollment"
// dex:explanation text:"Record the contact's status after the upsert and tag it."
type recordUpsertedContact struct {
	dex.StepDefaultsNoWaitFor[mailchimp.UpsertMemberResult]
}

func (recordUpsertedContact) GetStepType() string { return recordUpsertedContactStepType }

func (recordUpsertedContact) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordUpsertedContact) Execute(ctx dex.Context, result mailchimp.UpsertMemberResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contact := record.Request.Contacts[record.NextContactIndex]
	record.Contacts = append(record.Contacts, ContactOutcome{
		EmailAddress: contact.EmailAddress, SubscriberHash: result.Value.Member.SubscriberHash, Action: ContactEnrolled,
		PreviousStatus: record.PreviousStatus, Status: result.Value.Member.Status,
	})
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ContactTagging](tagLaunchContactStepType), ContactTagging{
		ListID: record.Request.ListID, EmailAddress: contact.EmailAddress, Tag: record.Request.Tag,
	}), nil
}

// dex:group group-id:enrollment group-label:"Enrollment"
// dex:explanation text:"Record a contact Mailchimp rejected and continue with the next contact."
type recordRejectedContact struct {
	dex.StepDefaultsNoWaitFor[mailchimp.UpsertMemberResult]
}

func (recordRejectedContact) GetStepType() string { return recordRejectedContactStepType }

func (recordRejectedContact) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordRejectedContact) Execute(ctx dex.Context, result mailchimp.UpsertMemberResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contact := record.Request.Contacts[record.NextContactIndex]
	record.Contacts = append(record.Contacts, ContactOutcome{
		EmailAddress: contact.EmailAddress, SubscriberHash: mailchimp.SubscriberHash(contact.EmailAddress), Action: ContactRejected,
		PreviousStatus: record.PreviousStatus, FailureMessage: failureMessage(result.Failure),
	})
	record.NextContactIndex++
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if record.NextContactIndex < len(record.Request.Contacts) {
		return dex.GoTo(sdkgo.StepRef[ContactLookup](readLaunchContactStepType), newContactLookup(record)), nil
	}
	return dex.GoTo(sdkgo.StepRef[AudienceCount](countSubscribedAudienceStepType), AudienceCount{ListID: record.Request.ListID}), nil
}

// dex:group group-id:enrollment group-label:"Enrollment"
// dex:explanation text:"Continue with the next contact, or count the audience once every contact is done."
type recordTaggedContact struct {
	dex.StepDefaultsNoWaitFor[mailchimp.UpdateMemberTagsResult]
}

func (recordTaggedContact) GetStepType() string { return recordTaggedContactStepType }

func (recordTaggedContact) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordTaggedContact) Execute(ctx dex.Context, _ mailchimp.UpdateMemberTagsResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.NextContactIndex++
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if record.NextContactIndex < len(record.Request.Contacts) {
		return dex.GoTo(sdkgo.StepRef[ContactLookup](readLaunchContactStepType), newContactLookup(record)), nil
	}
	return dex.GoTo(sdkgo.StepRef[AudienceCount](countSubscribedAudienceStepType), AudienceCount{ListID: record.Request.ListID}), nil
}

// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Record the subscribed count and wait for a person to approve or decline the send."
type awaitSendApproval struct {
	dex.StepDefaultsNoWaitFor[mailchimp.ListMembersResult]
}

func (awaitSendApproval) GetStepType() string { return awaitSendApprovalStepType }

func (awaitSendApproval) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (awaitSendApproval) Execute(ctx dex.Context, result mailchimp.ListMembersResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.SubscribedMemberCount, record.PreviousStatus = PhaseAwaitingApproval, result.Value.TotalItems, ""
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:send group-label:"Campaign send"
// dex:explanation text:"Record the send Mailchimp accepted, or that the campaign was already sending or sent, and complete."
type recordCampaignSent struct {
	dex.StepDefaultsNoWaitFor[mailchimp.SendCampaignResult]
}

func (recordCampaignSent) GetStepType() string { return recordCampaignSentStepType }

func (recordCampaignSent) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordCampaignSent) Execute(ctx dex.Context, result mailchimp.SendCampaignResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Campaign = PhaseSent, &result.Value.Campaign
	if result.Branch == mailchimp.SendCampaignBranchAlreadySent {
		record.Phase = PhaseAlreadySent
	}
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:send group-label:"Campaign send"
// dex:explanation text:"Complete without a send when the campaign is not a draft for this audience, is missing, or Mailchimp rejected it."
type recordCampaignNotSent struct {
	dex.StepDefaultsNoWaitFor[mailchimp.SendCampaignResult]
}

func (recordCampaignNotSent) GetStepType() string { return recordCampaignNotSentStepType }

func (recordCampaignNotSent) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordCampaignNotSent) Execute(ctx dex.Context, result mailchimp.SendCampaignResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.NotSentReason = PhaseNotSent, string(result.Branch)+": "+failureMessage(result.Failure)
	if result.Value.Campaign.ID != "" {
		record.Campaign = &result.Value.Campaign
	}
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Park an unknown send for a person instead of sending the campaign again."
type recordUncertainSend struct {
	dex.StepDefaultsNoWaitFor[mailchimp.SendCampaignResult]
}

func (recordUncertainSend) GetStepType() string { return recordUncertainSendStepType }

func (recordUncertainSend) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordUncertainSend) Execute(ctx dex.Context, result mailchimp.SendCampaignResult) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseNeedsReview
	record.UncertainSend = &UncertainSend{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		record.UncertainSend.FailureKind, record.UncertainSend.FailureMessage = result.Failure.Kind, result.Failure.Message
	}
	if result.Value.Campaign.ID != "" {
		record.Campaign = &result.Value.Campaign
	}
	if err := campaignLaunchPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := campaignLaunchAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Complete without sending after a person declined the send."
type recordDeclinedSend struct {
	dex.StepDefaultsNoWaitFor[SendDecision]
}

func (recordDeclinedSend) GetStepType() string { return recordDeclinedSendStepType }

func (recordDeclinedSend) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: launchLocks}
}

func (recordDeclinedSend) Execute(ctx dex.Context, _ SendDecision) (*dex.StepDecision, error) {
	record, err := campaignLaunchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

func newContactLookup(record CampaignLaunch) ContactLookup {
	return ContactLookup{ListID: record.Request.ListID, EmailAddress: record.Request.Contacts[record.NextContactIndex].EmailAddress}
}

func newCampaignSend(record CampaignLaunch) CampaignSend {
	return CampaignSend{CampaignID: record.Request.CampaignID, ListID: record.Request.ListID}
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

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

func isMailchimpID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		isLetterOrDigit := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')
		if !isLetterOrDigit {
			return false
		}
	}
	return true
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[SendDecisionInput, dex.None] = (*Flow)(nil).ApproveCampaignSend
var _ dex.RPC[SendDecisionInput, dex.None] = (*Flow)(nil).DeclineCampaignSend
var _ dex.RPC[SendDecisionInput, dex.None] = (*Flow)(nil).RecheckCampaignSend
var _ dex.RPC[dex.None, CampaignLaunch] = (*Flow)(nil).GetCampaignLaunch
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
