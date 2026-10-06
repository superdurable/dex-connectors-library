// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package envelopesigning sends a DocuSign envelope from a template, waits durably for the signers, and
// records the outcome. A Connect envelope event delivered through the envelopeEventReceived Trigger
// resumes the wait at once; a slow status poll is the fallback, and an envelope still unsigned at the
// signing deadline is voided as compensation. A completed envelope's combined PDF is digested, and
// stored by the application's CombinedDocumentStore, outside every Dex payload.
package envelopesigning

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ConnectionName is the static Dex Web connection that sends envelopes and verifies Connect events.
	ConnectionName = "docusign-esignature"
	// EnvelopeOutcomeTriggerBinding is the envelopeEventReceived binding whose events resume the wait.
	EnvelopeOutcomeTriggerBinding = "envelope-outcomes"
	// CorrelationCustomFieldName is the envelope custom field that carries the signing request ID back
	// in Connect events, so the Trigger finds the waiting Flow.
	CorrelationCustomFieldName = "dexSigningRequestId"
	// FlowIDPrefix precedes the signing request ID in every Flow ID.
	FlowIDPrefix = "docusign-signing-"

	sendEnvelopeStepType           = "SendEnvelope"
	checkEnvelopeStatusStepType    = "CheckEnvelopeStatus"
	downloadSignedDocumentStepType = "DownloadSignedDocument"
	voidExpiredEnvelopeStepType    = "VoidExpiredEnvelope"
	maximumSigners                 = 10
)

var (
	signingStateAttribute = dex.DefineAttribute[SigningState]("docusign-signing-state")
	envelopeEvents        = dex.DefineChannel[EnvelopeEventInput]("docusign-envelope-events")

	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

	errEnvelopeNotRecorded = errors.New("the Flow has not recorded its envelope yet; the event stays pending")
)

// SigningStatus is the signing Flow's phase.
type SigningStatus string

const (
	// StatusCreating means the envelope is being created and sent.
	StatusCreating SigningStatus = "creating"
	// StatusWaitingForSignature means the Flow waits for Connect or the next status poll.
	StatusWaitingForSignature SigningStatus = "waitingForSignature"
	// StatusCheckingStatus means the fallback poll is reading the envelope.
	StatusCheckingStatus SigningStatus = "checkingStatus"
	// StatusDownloadingDocument means the envelope completed and its combined PDF is being digested.
	StatusDownloadingDocument SigningStatus = "downloadingDocument"
	// StatusVoidingExpiredEnvelope means the deadline passed and the envelope is being voided.
	StatusVoidingExpiredEnvelope SigningStatus = "voidingExpiredEnvelope"
	// StatusCompleted means every signer signed and the document's digest is recorded.
	StatusCompleted SigningStatus = "completed"
	// StatusDeclined means a signer declined.
	StatusDeclined SigningStatus = "declined"
	// StatusVoided means the envelope was voided outside this Flow or expired in DocuSign.
	StatusVoided SigningStatus = "voided"
	// StatusExpired means the signing deadline passed and this Flow voided the envelope.
	StatusExpired SigningStatus = "expired"
	// StatusFailed means a DocuSign call failed; FailureMessage says which.
	StatusFailed SigningStatus = "failed"
)

// SigningRequest is the Flow's start input. Start it with the Flow ID FlowIDForRequest(RequestID).
type SigningRequest struct {
	// RequestID is the application's stable ID for this signature request, 1 to 64 letters, digits,
	// dots, underscores, or hyphens, such as an opportunity ID.
	RequestID string `json:"requestId"`
	// TemplateID is the DocuSign server template's GUID.
	TemplateID string `json:"templateId"`
	// Signers fills the template's roles, 1 to 10 of them, in the routing order the template defines
	// unless a role sets its own.
	Signers []docusign.TemplateRole `json:"signers"`
	// EmailSubject overrides the template's email subject; blank keeps it.
	EmailSubject string `json:"emailSubject,omitempty"`
}

// SigningPolicy sets how long the Flow waits and how often it polls without Connect.
type SigningPolicy struct {
	// StatusPollInterval is the fallback poll period. DocuSign allows one status read per envelope every
	// 15 minutes and recommends hours when Connect is configured.
	StatusPollInterval time.Duration
	// SigningDeadline is how long after sending the Flow waits before it voids the envelope.
	SigningDeadline time.Duration
	// VoidReason is shown to the signers when the deadline voids the envelope, 1 to 200 characters.
	VoidReason string
}

// DefaultSigningPolicy polls every six hours and voids an envelope still unsigned after 14 days.
func DefaultSigningPolicy() SigningPolicy {
	return SigningPolicy{StatusPollInterval: 6 * time.Hour, SigningDeadline: 14 * 24 * time.Hour, VoidReason: "The signing deadline passed."}
}

// SigningState is the Flow's durable record and its completion output.
type SigningState struct {
	// Request is the validated start input.
	Request SigningRequest `json:"request"`
	// Status is the Flow's phase.
	Status SigningStatus `json:"status"`
	// EnvelopeID is the sent envelope's GUID once DocuSign created it.
	EnvelopeID string `json:"envelopeId,omitempty"`
	// SentAt is when the Flow recorded the sent envelope.
	SentAt *time.Time `json:"sentAt,omitempty"`
	// Deadline is when an unsigned envelope is voided.
	Deadline *time.Time `json:"deadline,omitempty"`
	// EnvelopeStatus is the last envelope status DocuSign reported.
	EnvelopeStatus docusign.EnvelopeStatus `json:"envelopeStatus,omitempty"`
	// ReceivedEventIDs are the Connect event IDs already accepted, so a redelivery is a duplicate.
	ReceivedEventIDs []string `json:"receivedEventIds,omitempty"`
	// StatusPollCount counts fallback status reads.
	StatusPollCount int `json:"statusPollCount,omitempty"`
	// ResumedBy is connect or statusPoll: what ended the wait.
	ResumedBy string `json:"resumedBy,omitempty"`
	// Document is the signed combined PDF's digest and stored location.
	Document *docusign.CombinedDocument `json:"document,omitempty"`
	// FailureMessage is the safe message of a failed DocuSign call.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// EnvelopeEventInput is one Connect envelope event delivered to the waiting Flow.
type EnvelopeEventInput struct {
	// EventID is the Trigger event ID, the envelope ID and the event name.
	EventID string `json:"eventId"`
	// EnvelopeID is the envelope the event is about.
	EnvelopeID string `json:"envelopeId"`
	// Event is envelope-completed, envelope-declined, or envelope-voided.
	Event string `json:"event"`
}

// ReceiveEnvelopeEventResult says what the Flow did with one delivered event.
type ReceiveEnvelopeEventResult struct {
	// IsAccepted is true when the event was queued for the wait.
	IsAccepted bool `json:"isAccepted"`
	// IsDuplicate is true when the event was accepted earlier.
	IsDuplicate bool `json:"isDuplicate,omitempty"`
	// Status is the Flow's phase when the event arrived.
	Status SigningStatus `json:"status"`
}

// EnvelopeWait is the input of every wait for the signers.
type EnvelopeWait struct {
	// EnvelopeID is the envelope being signed.
	EnvelopeID string `json:"envelopeId"`
	// PollAt is when the fallback status poll runs if no Connect event arrives first.
	PollAt time.Time `json:"pollAt"`
}

// EnvelopeClosure ends the Flow for a declined or voided envelope.
type EnvelopeClosure struct {
	// Status is StatusDeclined or StatusVoided.
	Status SigningStatus `json:"status"`
	// ResumedBy is connect or statusPoll.
	ResumedBy string `json:"resumedBy"`
}

// EnvelopeSigningFlow sends one envelope and waits for its outcome.
type EnvelopeSigningFlow struct {
	dex.FlowDefaults
	connection docusign.Connection
	policy     SigningPolicy
}

// NewEnvelopeSigningFlow binds the DocuSign Connection and the signing policy at registration time.
func NewEnvelopeSigningFlow(connection docusign.Connection, policy SigningPolicy) (*EnvelopeSigningFlow, error) {
	if policy.StatusPollInterval <= 0 || policy.SigningDeadline <= 0 {
		return nil, fmt.Errorf("signing policy needs a positive status poll interval and signing deadline")
	}
	if len(policy.VoidReason) == 0 || len([]rune(policy.VoidReason)) > 200 {
		return nil, fmt.Errorf("signing policy void reason must be 1 to 200 characters")
	}
	return &EnvelopeSigningFlow{connection: connection, policy: policy}, nil
}

// FlowIDForRequest returns the Flow ID of a signing request.
func FlowIDForRequest(requestID string) string {
	return FlowIDPrefix + requestID
}

// GetSteps returns the send, wait, poll, download, void, and outcome Steps.
func (flow *EnvelopeSigningFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(RecordSigningRequest{}),
		dex.DefineStep(docusign.NewCreateEnvelopeFromTemplateStep(docusign.CreateEnvelopeFromTemplateStepConfig[SigningRequest]{
			StepType: sendEnvelopeStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "docusign", GroupLabel: "DocuSign", Explanation: "Create the envelope from the template and send it, at most once.",
			},
			MapToOperationInput: MapToCreateEnvelopeInput,
			Created:             sdkgo.GoTo(RecordEnvelopeSent{policy: flow.policy}),
			ProviderRejected:    sdkgo.GoTo(RecordSendFailure{}),
			Uncertain:           sdkgo.GoTo(RecordSendFailure{}),
			Defect:              sdkgo.GoTo(RecordSendFailure{}),
		})),
		dex.DefineStep(RecordEnvelopeSent{policy: flow.policy}),
		dex.DefineStep(RecordSendFailure{}),
		dex.DefineStep(AwaitEnvelopeOutcome{}),
		dex.DefineStep(docusign.NewGetEnvelopeStep(docusign.GetEnvelopeStepConfig[EnvelopeWait]{
			StepType: checkEnvelopeStatusStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "docusign", GroupLabel: "DocuSign", Explanation: "Read the envelope's status when no Connect event arrived before the poll time.",
			},
			MapToOperationInput: MapToGetEnvelopeInput,
			Found:               sdkgo.GoTo(RecordEnvelopeStatus{policy: flow.policy}),
			NotFound:            sdkgo.GoTo(RecordEnvelopeStatus{policy: flow.policy}),
			ProviderRejected:    sdkgo.GoTo(RecordEnvelopeStatus{policy: flow.policy}),
			InvalidResponse:     sdkgo.GoTo(RecordEnvelopeStatus{policy: flow.policy}),
			Defect:              sdkgo.GoTo(RecordEnvelopeStatus{policy: flow.policy}),
		})),
		dex.DefineStep(RecordEnvelopeStatus{policy: flow.policy}),
		dex.DefineStep(docusign.NewDownloadCombinedDocumentStep(docusign.DownloadCombinedDocumentStepConfig[EnvelopeWait]{
			StepType: downloadSignedDocumentStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "docusign", GroupLabel: "DocuSign", Explanation: "Digest and store the signed combined PDF with its certificate of completion.",
			},
			MapToOperationInput: MapToDownloadCombinedDocumentInput,
			Downloaded:          sdkgo.GoTo(RecordSignedDocument{}),
			NotFound:            sdkgo.GoTo(RecordDocumentFailure{}),
			TooLarge:            sdkgo.GoTo(RecordDocumentFailure{}),
			ProviderRejected:    sdkgo.GoTo(RecordDocumentFailure{}),
			InvalidResponse:     sdkgo.GoTo(RecordDocumentFailure{}),
			Defect:              sdkgo.GoTo(RecordDocumentFailure{}),
		})),
		dex.DefineStep(RecordSignedDocument{}),
		dex.DefineStep(RecordDocumentFailure{}),
		dex.DefineStep(RecordEnvelopeClosed{}),
		dex.DefineStep(docusign.NewVoidEnvelopeStep(docusign.VoidEnvelopeStepConfig[EnvelopeWait]{
			StepType: voidExpiredEnvelopeStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "docusign", GroupLabel: "DocuSign", Explanation: "Void the envelope that is still unsigned at the signing deadline.",
			},
			MapToOperationInput: func(wait EnvelopeWait) docusign.VoidEnvelopeInput {
				return docusign.VoidEnvelopeInput{EnvelopeID: wait.EnvelopeID, VoidedReason: flow.policy.VoidReason}
			},
			Voided:           sdkgo.GoTo(RecordEnvelopeExpired{}),
			NotVoidable:      sdkgo.GoTo(RecordVoidRefusal{}),
			NotFound:         sdkgo.GoTo(RecordVoidRefusal{}),
			ProviderRejected: sdkgo.GoTo(RecordVoidRefusal{}),
			InvalidResponse:  sdkgo.GoTo(RecordVoidRefusal{}),
			Defect:           sdkgo.GoTo(RecordVoidRefusal{}),
		})),
		dex.DefineStep(RecordEnvelopeExpired{}),
		dex.DefineStep(RecordVoidRefusal{}),
	}
}

// GetRPCs returns the Connect event RPC, the state read RPC, and the Dex Web views.
func (flow *EnvelopeSigningFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ReceiveEnvelopeEvent, &dex.RPCOptions{LockAttributes: signingStateLocks()}),
		dex.DefineRPC(flow.GetSigningState, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the signing state Attribute and the envelope event Channel.
func (*EnvelopeSigningFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{signingStateAttribute},
		Channels:   []dex.ChannelDef{envelopeEvents},
	}
}

// GetConnectorTriggerBindings declares the envelopeEventReceived binding that resumes this Flow.
func (*EnvelopeSigningFlow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		docusign.DefineEnvelopeEventReceivedTriggerBinding(docusign.EnvelopeEventReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: EnvelopeOutcomeTriggerBinding,
		}),
	}
}

// ReceiveEnvelopeEvent queues one Connect event for the wait. A redelivered event is a duplicate, an event
// for another envelope or after the outcome is consumed without effect, and an event that overtook the
// recorded envelope stays pending through an error.
func (*EnvelopeSigningFlow) ReceiveEnvelopeEvent(ctx dex.Context, input EnvelopeEventInput) (*dex.RPCResult[ReceiveEnvelopeEventResult], error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case slices.Contains(state.ReceivedEventIDs, input.EventID):
		return &dex.RPCResult[ReceiveEnvelopeEventResult]{Output: ReceiveEnvelopeEventResult{IsDuplicate: true, Status: state.Status}}, nil
	case state.EnvelopeID == "":
		return nil, errEnvelopeNotRecorded
	case input.EnvelopeID != state.EnvelopeID || !isWaiting(state.Status):
		return &dex.RPCResult[ReceiveEnvelopeEventResult]{Output: ReceiveEnvelopeEventResult{Status: state.Status}}, nil
	}
	state.ReceivedEventIDs = append(state.ReceivedEventIDs, input.EventID)
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	if err := envelopeEvents.Publish(ctx, input); err != nil {
		return nil, err
	}
	return &dex.RPCResult[ReceiveEnvelopeEventResult]{Output: ReceiveEnvelopeEventResult{IsAccepted: true, Status: state.Status}}, nil
}

// GetSigningState returns the signing record, also after the Flow ends; it is empty until the start
// Step records the request.
func (*EnvelopeSigningFlow) GetSigningState(ctx dex.Context, _ dex.None) (*dex.RPCResult[SigningState], error) {
	state, err := optionalSigningState(ctx)
	if err != nil || state == nil {
		return &dex.RPCResult[SigningState]{}, err
	}
	return &dex.RPCResult[SigningState]{Output: *state}, nil
}

// GetDexSummary returns the signing record for the Dex Web run list.
//
// dex:field attribute-key:docusign-signing-state value-type:json editable:false description:"Signing status, envelope, and document digest"
func (*EnvelopeSigningFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := optionalSigningState(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"docusign-signing-state": state}}, nil
}

// GetDexDisplay returns the signing record for the Dex Web run detail.
//
// dex:field attribute-key:docusign-signing-state value-type:json editable:false description:"Request, envelope status, received Connect events, and signed document"
func (*EnvelopeSigningFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := optionalSigningState(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"docusign-signing-state": state}}, nil
}

// AcceptEnvelopeEvent is the application's admission rule: only an envelope outcome that carries a
// valid signing request ID reaches a Flow.
func AcceptEnvelopeEvent(event sdkgo.TriggerEvent[docusign.EnvelopeEvent]) bool {
	requestID, hasRequestID := event.Payload.CustomFieldValue(CorrelationCustomFieldName)
	return hasRequestID && requestIDPattern.MatchString(requestID) && event.Payload.EnvelopeID != ""
}

// ResolveFlowID finds the waiting Flow from the envelope's correlation custom field.
func ResolveFlowID(event sdkgo.TriggerEvent[docusign.EnvelopeEvent]) string {
	requestID, _ := event.Payload.CustomFieldValue(CorrelationCustomFieldName)
	return FlowIDForRequest(requestID)
}

// MapToEnvelopeEventInput copies the event's identity into the RPC input.
func MapToEnvelopeEventInput(event sdkgo.TriggerEvent[docusign.EnvelopeEvent]) EnvelopeEventInput {
	return EnvelopeEventInput{EventID: event.ID, EnvelopeID: event.Payload.EnvelopeID, Event: event.Payload.Event}
}

// MapToCreateEnvelopeInput sends the template to the signers and tags the envelope with the request ID.
func MapToCreateEnvelopeInput(request SigningRequest) docusign.CreateEnvelopeFromTemplateInput {
	return docusign.CreateEnvelopeFromTemplateInput{
		TemplateID: request.TemplateID, TemplateRoles: request.Signers, EmailSubject: request.EmailSubject,
		CustomFields: []docusign.EnvelopeCustomField{{Name: CorrelationCustomFieldName, Value: request.RequestID}},
	}
}

// MapToGetEnvelopeInput reads the envelope being waited for.
func MapToGetEnvelopeInput(wait EnvelopeWait) docusign.GetEnvelopeInput {
	return docusign.GetEnvelopeInput{EnvelopeID: wait.EnvelopeID}
}

// MapToDownloadCombinedDocumentInput downloads the signed documents with the certificate of completion.
func MapToDownloadCombinedDocumentInput(wait EnvelopeWait) docusign.DownloadCombinedDocumentInput {
	return docusign.DownloadCombinedDocumentInput{EnvelopeID: wait.EnvelopeID, IncludeCertificate: true}
}

// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Validate the signing request and persist it before creating the envelope."
type RecordSigningRequest struct {
	dex.StepDefaultsNoWaitFor[SigningRequest]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordSigningRequest) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute records the request; an invalid one fails the Flow without calling DocuSign.
func (RecordSigningRequest) Execute(ctx dex.Context, request SigningRequest) (*dex.StepDecision, error) {
	if err := validateSigningRequest(ctx.FlowID(), request); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := signingStateAttribute.Set(ctx, SigningState{Request: request, Status: StatusCreating}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SigningRequest](sendEnvelopeStepType), request), nil
}

// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Record the sent envelope and its signing deadline, then start waiting for the signers."
type RecordEnvelopeSent struct {
	dex.StepDefaultsNoWaitFor[docusign.CreateEnvelopeFromTemplateResult]
	policy SigningPolicy
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordEnvelopeSent) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute stores the envelope ID, so Connect events for it are accepted from now on.
func (step RecordEnvelopeSent) Execute(ctx dex.Context, result docusign.CreateEnvelopeFromTemplateResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	deadline := now.Add(step.policy.SigningDeadline)
	state.EnvelopeID, state.EnvelopeStatus, state.Status = result.Value.EnvelopeID, result.Value.Status, StatusWaitingForSignature
	state.SentAt, state.Deadline = &now, &deadline
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(AwaitEnvelopeOutcome{}, nextEnvelopeWait(state, step.policy, now)), nil
}

// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Persist why DocuSign rejected the envelope or why its creation is uncertain, and fail the Flow."
type RecordSendFailure struct {
	dex.StepDefaultsNoWaitFor[docusign.CreateEnvelopeFromTemplateResult]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordSendFailure) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute fails the Flow; an uncertain create asks the operator to look for the envelope first.
func (RecordSendFailure) Execute(ctx dex.Context, result docusign.CreateEnvelopeFromTemplateResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status = StatusFailed
	state.FailureMessage = describeFailure("creating the envelope", result.Branch, result.Failure)
	if result.Branch == docusign.CreateEnvelopeFromTemplateBranchUncertain {
		state.FailureMessage += "; search DocuSign for an envelope with custom field " + CorrelationCustomFieldName + " before sending again"
	}
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.ForceFail(state.FailureMessage), nil
}

// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Wait durably for a Connect envelope event or the fallback status poll time."
type AwaitEnvelopeOutcome struct {
	dex.StepDefaults
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (AwaitEnvelopeOutcome) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// WaitFor races the next Connect event against the poll time.
func (AwaitEnvelopeOutcome) WaitFor(_ dex.Context, wait EnvelopeWait) (*dex.Wait, error) {
	return dex.AnyOf(
		dex.Timer(max(time.Until(wait.PollAt), time.Second)),
		envelopeEvents.ForOne(),
	), nil
}

// Execute routes a Connect outcome at once and otherwise reads the envelope's status.
func (AwaitEnvelopeOutcome) Execute(ctx dex.Context, wait EnvelopeWait) (*dex.StepDecision, error) {
	events, err := envelopeEvents.GetConditionResults(ctx)
	if err != nil {
		return nil, err
	}
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 && events[0].Event == docusign.ConnectEventEnvelopeCompleted {
		state.Status, state.EnvelopeStatus, state.ResumedBy = StatusDownloadingDocument, docusign.EnvelopeStatusCompleted, "connect"
		if err := signingStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[EnvelopeWait](downloadSignedDocumentStepType), wait), nil
	}
	if len(events) > 0 {
		return dex.GoTo(RecordEnvelopeClosed{}, EnvelopeClosure{Status: closedStatusOfEvent(events[0].Event), ResumedBy: "connect"}), nil
	}
	state.Status = StatusCheckingStatus
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[EnvelopeWait](checkEnvelopeStatusStepType), wait), nil
}

// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Route a final status read by the poll, void at the deadline, or wait again."
type RecordEnvelopeStatus struct {
	dex.StepDefaultsNoWaitFor[docusign.GetEnvelopeResult]
	policy SigningPolicy
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordEnvelopeStatus) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute treats a failed read as no news: the Flow keeps waiting until the deadline.
func (step RecordEnvelopeStatus) Execute(ctx dex.Context, result docusign.GetEnvelopeResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.StatusPollCount++
	now := time.Now().UTC()
	wait := nextEnvelopeWait(state, step.policy, now)
	if result.Branch == docusign.GetEnvelopeBranchFound {
		state.EnvelopeStatus = result.Value.Status
	}
	switch {
	case result.Branch == docusign.GetEnvelopeBranchFound && result.Value.Status == docusign.EnvelopeStatusCompleted:
		state.Status, state.ResumedBy = StatusDownloadingDocument, "statusPoll"
		if err := signingStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[EnvelopeWait](downloadSignedDocumentStepType), wait), nil
	case result.Branch == docusign.GetEnvelopeBranchFound && result.Value.Status.IsTerminal():
		if err := signingStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(RecordEnvelopeClosed{}, EnvelopeClosure{Status: closedStatusOfEnvelope(result.Value.Status), ResumedBy: "statusPoll"}), nil
	}
	if state.Deadline != nil && !now.Before(*state.Deadline) {
		state.Status = StatusVoidingExpiredEnvelope
		if err := signingStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[EnvelopeWait](voidExpiredEnvelopeStepType), wait), nil
	}
	state.Status = StatusWaitingForSignature
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(AwaitEnvelopeOutcome{}, wait), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record the signed document's digest and stored location and complete the Flow."
type RecordSignedDocument struct {
	dex.StepDefaultsNoWaitFor[docusign.DownloadCombinedDocumentResult]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordSignedDocument) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute completes the Flow with the document's digest; the PDF itself stays in the document store.
func (RecordSignedDocument) Execute(ctx dex.Context, result docusign.DownloadCombinedDocumentResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	document := result.Value
	state.Status, state.Document = StatusCompleted, &document
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Persist why the signed document could not be downloaded or stored, and fail the Flow."
type RecordDocumentFailure struct {
	dex.StepDefaultsNoWaitFor[docusign.DownloadCombinedDocumentResult]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordDocumentFailure) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute fails the Flow; the envelope stays completed in DocuSign.
func (RecordDocumentFailure) Execute(ctx dex.Context, result docusign.DownloadCombinedDocumentResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status = StatusFailed
	state.FailureMessage = describeFailure("downloading the signed document", result.Branch, result.Failure)
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.ForceFail(state.FailureMessage), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record a declined or voided envelope and complete the Flow with that business outcome."
type RecordEnvelopeClosed struct {
	dex.StepDefaultsNoWaitFor[EnvelopeClosure]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordEnvelopeClosed) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute completes the Flow: a decline or a void is an outcome, not a failure.
func (RecordEnvelopeClosed) Execute(ctx dex.Context, closure EnvelopeClosure) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status, state.ResumedBy = closure.Status, closure.ResumedBy
	state.EnvelopeStatus = map[SigningStatus]docusign.EnvelopeStatus{StatusDeclined: docusign.EnvelopeStatusDeclined}[closure.Status]
	if state.EnvelopeStatus == "" {
		state.EnvelopeStatus = docusign.EnvelopeStatusVoided
	}
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record that the deadline voided the envelope and complete the Flow as expired."
type RecordEnvelopeExpired struct {
	dex.StepDefaultsNoWaitFor[docusign.VoidEnvelopeResult]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordEnvelopeExpired) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute completes the Flow; WasAlreadyVoided means someone else voided it first, with the same effect.
func (RecordEnvelopeExpired) Execute(ctx dex.Context, _ docusign.VoidEnvelopeResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status, state.EnvelopeStatus = StatusExpired, docusign.EnvelopeStatusVoided
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Download an envelope that completed at the deadline, close a declined one, or fail on another void refusal."
type RecordVoidRefusal struct {
	dex.StepDefaultsNoWaitFor[docusign.VoidEnvelopeResult]
}

// GetStepOptions locks the signing state against a concurrent Connect event.
func (RecordVoidRefusal) GetStepOptions() *dex.StepOptions { return signingStateStepOptions() }

// Execute handles the race of a signer finishing just as the deadline voids the envelope.
func (RecordVoidRefusal) Execute(ctx dex.Context, result docusign.VoidEnvelopeResult) (*dex.StepDecision, error) {
	state, err := signingStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	wait := EnvelopeWait{EnvelopeID: state.EnvelopeID, PollAt: time.Now().UTC()}
	isNotVoidable := result.Branch == docusign.VoidEnvelopeBranchNotVoidable
	switch {
	case isNotVoidable && result.Value.Status == docusign.EnvelopeStatusCompleted:
		state.Status, state.EnvelopeStatus, state.ResumedBy = StatusDownloadingDocument, docusign.EnvelopeStatusCompleted, "statusPoll"
		if err := signingStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[EnvelopeWait](downloadSignedDocumentStepType), wait), nil
	case isNotVoidable && result.Value.Status == docusign.EnvelopeStatusDeclined:
		return dex.GoTo(RecordEnvelopeClosed{}, EnvelopeClosure{Status: StatusDeclined, ResumedBy: "statusPoll"}), nil
	}
	state.Status = StatusFailed
	state.FailureMessage = describeFailure("voiding the expired envelope", result.Branch, result.Failure)
	if err := signingStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.ForceFail(state.FailureMessage), nil
}

// nextEnvelopeWait polls after the policy interval, or at the deadline when that comes first.
func nextEnvelopeWait(state SigningState, policy SigningPolicy, now time.Time) EnvelopeWait {
	pollAt := now.Add(policy.StatusPollInterval)
	if state.Deadline != nil && state.Deadline.Before(pollAt) {
		pollAt = *state.Deadline
	}
	return EnvelopeWait{EnvelopeID: state.EnvelopeID, PollAt: pollAt}
}

// closedStatusOfEvent maps envelope-declined to declined and envelope-voided to voided.
func closedStatusOfEvent(event string) SigningStatus {
	if event == docusign.ConnectEventEnvelopeDeclined {
		return StatusDeclined
	}
	return StatusVoided
}

// closedStatusOfEnvelope maps a declined or voided envelope status to the Flow's outcome.
func closedStatusOfEnvelope(status docusign.EnvelopeStatus) SigningStatus {
	if status == docusign.EnvelopeStatusDeclined {
		return StatusDeclined
	}
	return StatusVoided
}

// isWaiting reports whether a Connect event can still change the Flow's outcome.
func isWaiting(status SigningStatus) bool {
	return status == StatusWaitingForSignature || status == StatusCheckingStatus
}

// describeFailure names the branch and the connector's safe failure message.
func describeFailure(action string, branch sdkgo.BranchID, failure *sdkgo.Failure) string {
	message := action + " selected " + string(branch)
	if failure != nil {
		message += ": " + failure.Message
	}
	return message
}

// validateSigningRequest checks the start input and that the Flow ID was derived from it.
func validateSigningRequest(flowID string, request SigningRequest) error {
	switch {
	case !requestIDPattern.MatchString(request.RequestID):
		return errors.New("requestId must be 1 to 64 letters, digits, dots, underscores, or hyphens")
	case flowID != FlowIDForRequest(request.RequestID):
		return fmt.Errorf("start the Flow with the ID %s so Connect events find it", FlowIDForRequest(request.RequestID))
	case request.TemplateID == "":
		return errors.New("templateId is required")
	case len(request.Signers) == 0 || len(request.Signers) > maximumSigners:
		return fmt.Errorf("signers must fill 1 to %d template roles", maximumSigners)
	}
	return nil
}

func signingStateLocks() []dex.AttributeLock {
	return []dex.AttributeLock{dex.LockAttribute(signingStateAttribute)}
}

func signingStateStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: signingStateLocks()}
}

// optionalSigningState returns nil before the start Step recorded the request.
func optionalSigningState(ctx dex.Context) (*SigningState, error) {
	state, err := signingStateAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

var _ dex.Flow = (*EnvelopeSigningFlow)(nil)
var _ dex.RPC[EnvelopeEventInput, ReceiveEnvelopeEventResult] = (*EnvelopeSigningFlow)(nil).ReceiveEnvelopeEvent
var _ dex.RPC[dex.None, SigningState] = (*EnvelopeSigningFlow)(nil).GetSigningState
var _ dex.RPC[dex.None, map[string]any] = (*EnvelopeSigningFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*EnvelopeSigningFlow)(nil).GetDexDisplay
