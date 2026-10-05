// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package supportrequest demonstrates every Jira Service Management operation in one Flow started from Dex
// Web Start Flow: find the customer by email, look for their open request with the same summary, raise one
// on their behalf only when none exists, read it back with its SLAs, label it, add an internal note and a
// public reply, and move it to a destination status. An uncertain create is parked for an operator instead
// of being raised again.
package supportrequest

import (
	"errors"
	"strings"
	"time"

	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "JiraServiceManagementSupportRequest"
	// ConnectionName is the static Dex Web connection for Jira Service Management.
	ConnectionName = "jsm-support"
	// ReconcileSupportRequestPermission is required by both reconciliation Actions.
	ReconcileSupportRequestPermission = "jsm-support-request.reconcile"

	recordSupportRequestStepType        = "RecordSupportRequest"
	findRequesterStepType               = "FindRequester"
	evaluateRequesterStepType           = "EvaluateRequester"
	recordUnknownRequesterStepType      = "RecordUnknownRequester"
	findOpenDuplicateTicketsStepType    = "FindOpenDuplicateTickets"
	evaluateDuplicateTicketsStepType    = "EvaluateDuplicateTickets"
	createSupportTicketStepType         = "CreateSupportTicket"
	recordCreatedTicketStepType         = "RecordCreatedTicket"
	recordRejectedTicketStepType        = "RecordRejectedTicket"
	recordUncertainTicketStepType       = "RecordUncertainTicket"
	readBackSupportTicketStepType       = "ReadBackSupportTicket"
	verifySupportTicketStepType         = "VerifySupportTicket"
	recordMissingSupportTicketStepType  = "RecordMissingSupportTicket"
	planNextSupportActionStepType       = "PlanNextSupportAction"
	labelSupportTicketStepType          = "LabelSupportTicket"
	recordTicketLabelsStepType          = "RecordTicketLabels"
	addInternalNoteStepType             = "AddInternalNote"
	recordInternalNoteStepType          = "RecordInternalNote"
	sendPublicReplyStepType             = "SendPublicReply"
	recordPublicReplyStepType           = "RecordPublicReply"
	moveSupportTicketStepType           = "MoveSupportTicket"
	recordUnavailableTransitionStepType = "RecordUnavailableTransition"
	completeSupportRequestStepType      = "CompleteSupportRequest"

	maximumDuplicateCandidates = 20
)

// Support request phases stored in the jsm-support-request-phase Attribute.
const (
	// PhaseFindingRequester means the Flow is looking up the customer by email.
	PhaseFindingRequester = "findingRequester"
	// PhaseRequesterNotFound means no customer of the service desk has the email address; nothing was raised.
	PhaseRequesterNotFound = "requesterNotFound"
	// PhaseSearching means the Flow is looking for the customer's open request with the same summary.
	PhaseSearching = "searching"
	// PhaseCreating means a createTicket Step is about to run or running.
	PhaseCreating = "creating"
	// PhaseVerifyingTicket means the Flow is reading back the created or reused request.
	PhaseVerifyingTicket = "verifyingTicket"
	// PhaseNeedsReconciliation means the create outcome is unknown and an operator must confirm or retry.
	PhaseNeedsReconciliation = "needsReconciliation"
	// PhaseVerifyingReportedTicket means the Flow is reading the request key an operator reported.
	PhaseVerifyingReportedTicket = "verifyingReportedTicket"
	// PhaseLabeling means the Flow is setting the request's labels and priority.
	PhaseLabeling = "labeling"
	// PhaseNoting means the Flow is adding the internal note.
	PhaseNoting = "noting"
	// PhaseReplying means the Flow is sending the public reply.
	PhaseReplying = "replying"
	// PhaseTransitioning means the Flow is moving the request to the destination status.
	PhaseTransitioning = "transitioning"
	// PhaseHandled means every requested action finished.
	PhaseHandled = "handled"
	// PhaseTransitionUnavailable means the workflow offers no transition to the destination status.
	PhaseTransitionUnavailable = "transitionUnavailable"
	// PhaseRejected means Jira Service Management conclusively rejected the create and nothing was raised.
	PhaseRejected = "rejected"
)

// Reconciliation notes explain why a reported request key was not adopted.
const (
	// NoteReportedTicketMissing means Jira has no visible request with the reported key.
	NoteReportedTicketMissing = "Jira has no visible request with the reported key"
	// NoteReportedTicketMismatch means the reported request is in another desk, for another customer, or has another summary.
	NoteReportedTicketMismatch = "the reported request is in another service desk, for another customer, or has another summary"
)

var (
	supportPhaseAttribute   = dex.DefineAttribute[string]("jsm-support-request-phase")
	supportRequestAttribute = dex.DefineAttribute[SupportRequest]("jsm-support-request")
)

var errReportedTicketKeyInvalid = errors.New("issueKey must be a request key such as ITH-42")

// Input is the support request entered in Dex Web Start Flow.
type Input struct {
	// RequesterEmail is the customer's email address; the request is raised on their behalf.
	RequesterEmail string `json:"requesterEmail"`
	// Summary is the request title, at most 255 characters; the customer's open request with the same summary is reused.
	Summary string `json:"summary"`
	// Description is the request text used when a new request is raised.
	Description string `json:"description,omitempty"`
	// ServiceDeskID names the service desk, such as 10, when none was picked in Dex Web.
	ServiceDeskID string `json:"serviceDeskId,omitempty"`
	// ProjectKey names the service desk's project, such as ITH, when no service desk was picked in Dex Web.
	ProjectKey string `json:"projectKey,omitempty"`
	// RequestTypeID names the request type, such as 25, when none was picked in Dex Web.
	RequestTypeID string `json:"requestTypeId,omitempty"`
	// Labels lists labels to add, without whitespace; blank adds none.
	Labels []string `json:"labels,omitempty"`
	// PriorityName sets the priority, such as High; blank keeps it.
	PriorityName string `json:"priorityName,omitempty"`
	// InternalNote is added as an internal note only agents see; blank adds none.
	InternalNote string `json:"internalNote,omitempty"`
	// PublicReply is sent to the customer as a public reply; blank sends none.
	PublicReply string `json:"publicReply,omitempty"`
	// DestinationStatusName moves the request to this status, such as In progress; blank leaves it.
	DestinationStatusName string `json:"destinationStatusName,omitempty"`
}

// DeskSelection is the value the service desk picker saves for the FindRequester Step.
type DeskSelection struct {
	// ServiceDeskID is the picked service desk's numeric ID.
	ServiceDeskID string `json:"serviceDeskId,omitempty"`
	// ProjectKey is the key of the picked desk's project.
	ProjectKey string `json:"projectKey,omitempty"`
	// ServiceDeskName is the picked desk's display name.
	ServiceDeskName string `json:"serviceDeskName,omitempty"`
}

// RequestTypeSelection is the value the request type picker saves for the CreateSupportTicket Step.
type RequestTypeSelection struct {
	// ServiceDeskID is the service desk the request type belongs to.
	ServiceDeskID string `json:"serviceDeskId,omitempty"`
	// RequestTypeID is the picked request type's numeric ID.
	RequestTypeID string `json:"requestTypeId,omitempty"`
	// RequestTypeName is the picked request type's name.
	RequestTypeName string `json:"requestTypeName,omitempty"`
}

// ValidatedRequest is the validated request every later Step reads.
type ValidatedRequest struct {
	// ServiceDeskID is the service desk the request belongs to.
	ServiceDeskID string `json:"serviceDeskId"`
	// ProjectKey is the key of the service desk's project.
	ProjectKey string `json:"projectKey"`
	// RequestTypeID is the request type of a new request.
	RequestTypeID string `json:"requestTypeId"`
	// RequesterEmail is the trimmed customer email address.
	RequesterEmail string `json:"requesterEmail"`
	// Summary is the trimmed request title.
	Summary string `json:"summary"`
	// Description is the request text.
	Description string `json:"description,omitempty"`
	// Labels lists labels to add.
	Labels []string `json:"labels,omitempty"`
	// PriorityName is the priority to set.
	PriorityName string `json:"priorityName,omitempty"`
	// InternalNote is the internal note to add.
	InternalNote string `json:"internalNote,omitempty"`
	// PublicReply is the public reply to send.
	PublicReply string `json:"publicReply,omitempty"`
	// DestinationStatusName is the status to move the request to.
	DestinationStatusName string `json:"destinationStatusName,omitempty"`
}

// DuplicateSearch is the input of the FindOpenDuplicateTickets Step.
type DuplicateSearch struct {
	// ProjectKey is the service desk's project.
	ProjectKey string `json:"projectKey"`
	// RequesterAccountID is the customer whose open requests are searched.
	RequesterAccountID string `json:"requesterAccountId"`
	// Summary is the summary phrase to search for.
	Summary string `json:"summary"`
}

// TicketReference identifies the request the next Step reads or changes.
type TicketReference struct {
	// IssueKey is the request key, such as ITH-42.
	IssueKey string `json:"issueKey"`
}

// LabelChange is one labels and priority change for the updateTicket Step.
type LabelChange struct {
	// IssueKey is the request to change.
	IssueKey string `json:"issueKey"`
	// Labels lists labels to add.
	Labels []string `json:"labels,omitempty"`
	// PriorityName is the priority to set.
	PriorityName string `json:"priorityName,omitempty"`
}

// CommentRequest is one comment for an addComment Step.
type CommentRequest struct {
	// IssueKey is the request to comment on.
	IssueKey string `json:"issueKey"`
	// Body is the comment text.
	Body string `json:"body"`
}

// StatusChangeRequest is one move for the transitionTicket Step.
type StatusChangeRequest struct {
	// IssueKey is the request to move.
	IssueKey string `json:"issueKey"`
	// DestinationStatusName is the status to move it to.
	DestinationStatusName string `json:"destinationStatusName"`
}

// UncertainCreate records a dispatched create whose outcome Jira Service Management did not confirm.
type UncertainCreate struct {
	// CallID is the connector call identity of the uncertain create.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome; search the desk around it.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
}

// CommentOutcome records one comment the Flow added.
type CommentOutcome struct {
	// CommentID is the comment's ID once Jira Service Management confirmed it.
	CommentID string `json:"commentId,omitempty"`
	// IsPublic is the comment's visibility.
	IsPublic bool `json:"isPublic"`
	// IsOutcomeUnknown reports a comment that may or may not exist; it is never re-sent.
	IsOutcomeUnknown bool `json:"isOutcomeUnknown,omitempty"`
}

// SupportRequest is the Flow's durable record of one handled support request.
type SupportRequest struct {
	// Request is the validated request.
	Request ValidatedRequest `json:"request"`
	// Phase mirrors the jsm-support-request-phase Attribute.
	Phase string `json:"phase"`
	// RequesterAccountID is the customer the request is raised for.
	RequesterAccountID string `json:"requesterAccountId,omitempty"`
	// CreateAttempts counts createTicket Step executions: the first create plus each approved retry.
	CreateAttempts int `json:"createAttempts"`
	// IsExistingTicketReused reports that search found the customer's open request with the same summary.
	IsExistingTicketReused bool `json:"isExistingTicketReused,omitempty"`
	// Ticket is the latest read-back of the request.
	Ticket *jiraservicemanagement.Ticket `json:"ticket,omitempty"`
	// SLANames lists the request's SLAs as read back, with an ongoing cycle marked.
	SLANames []string `json:"slaNames,omitempty"`
	// Labels lists the request's labels after the label Step.
	Labels []string `json:"labels,omitempty"`
	// IsTicketUpdated reports that the label Step finished.
	IsTicketUpdated bool `json:"isTicketUpdated,omitempty"`
	// InternalNote records the internal note once its Step finished.
	InternalNote *CommentOutcome `json:"internalNote,omitempty"`
	// PublicReply records the public reply once its Step finished.
	PublicReply *CommentOutcome `json:"publicReply,omitempty"`
	// Status is the request's status after the transition Step.
	Status *jiraservicemanagement.TicketStatus `json:"status,omitempty"`
	// AvailableTransitionNames lists the transitions offered when none reached the destination.
	AvailableTransitionNames []string `json:"availableTransitionNames,omitempty"`
	// RejectedFieldIDs lists the fields named when the create was rejected.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
	// ProviderErrorKey is the machine-readable key of a rejected create.
	ProviderErrorKey string `json:"providerErrorKey,omitempty"`
	// UncertainCreate describes the latest uncertain create while reconciliation is pending.
	UncertainCreate *UncertainCreate `json:"uncertainCreate,omitempty"`
	// ReconciliationNote explains why a reported request key was not adopted.
	ReconciliationNote string `json:"reconciliationNote,omitempty"`
}

// ConfirmCreatedTicketInput reports the request an operator found after an uncertain create.
type ConfirmCreatedTicketInput struct {
	// IssueKey is the key of the request found in the service desk, such as ITH-42.
	IssueKey string `json:"issueKey"`
}

// Flow handles one Jira Service Management support request.
type Flow struct {
	dex.FlowDefaults
	connection  jiraservicemanagement.Connection
	desk        DeskSelection
	requestType RequestTypeSelection
}

// NewFlow binds the connection and the picked service desk and request type at registration time. A blank
// selection uses each Start Flow input's serviceDeskId, projectKey, and requestTypeId.
func NewFlow(connection jiraservicemanagement.Connection, desk DeskSelection, requestType RequestTypeSelection) *Flow {
	return &Flow{connection: connection, desk: desk, requestType: requestType}
}

// DeskSelectionConfigurationRef identifies the service desk picker value saved in Dex Web.
func DeskSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: jiraservicemanagement.ConnectorID, ConnectionName: ConnectionName, OperationID: "findCustomerByEmail",
		FlowType: FlowType, StepType: findRequesterStepType,
	}
}

// RequestTypeSelectionConfigurationRef identifies the request type picker value saved in Dex Web.
func RequestTypeSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: jiraservicemanagement.ConnectorID, ConnectionName: ConnectionName, OperationID: "createTicket",
		FlowType: FlowType, StepType: createSupportTicketStepType,
	}
}

// ValidateSelections fails when both pickers are saved for different service desks.
func ValidateSelections(desk DeskSelection, requestType RequestTypeSelection) error {
	deskID, requestTypeDeskID := strings.TrimSpace(desk.ServiceDeskID), strings.TrimSpace(requestType.ServiceDeskID)
	if deskID != "" && requestTypeDeskID != "" && deskID != requestTypeDeskID {
		return errors.New("the picked request type belongs to another service desk; pick it again from the picked service desk")
	}
	return nil
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Jira Service Management connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSupportRequest{desk: flow.desk, requestType: flow.requestType}),
		dex.DefineStep(jiraservicemanagement.NewFindCustomerByEmailStep(jiraservicemanagement.FindCustomerByEmailStepConfig[ValidatedRequest]{
			StepType: findRequesterStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Find the service desk's customer with the requester's email address."},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "supportDesk", UnitID: jiraservicemanagement.UIUnitServiceDeskPicker, Label: "Support service desk",
				Description: "Choose the service desk whose customers are looked up and whose requests are searched and raised; the unit saves the desk ID, its project key, and its name. Leave it unsaved to use each Start Flow input's serviceDeskId and projectKey. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: jiraservicemanagement.UIServiceDeskPickerPortServiceDeskID, JSONPointer: "/serviceDeskId"},
					{Port: jiraservicemanagement.UIServiceDeskPickerPortProjectKey, JSONPointer: "/projectKey"},
					{Port: jiraservicemanagement.UIServiceDeskPickerPortServiceDeskName, JSONPointer: "/serviceDeskName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToFindCustomerByEmailInput,
			Found:    sdkgo.GoTo(evaluateRequester{}),
			NotFound: sdkgo.GoTo(recordUnknownRequester{}),
		})),
		dex.DefineStep(evaluateRequester{}),
		dex.DefineStep(recordUnknownRequester{}),
		dex.DefineStep(jiraservicemanagement.NewSearchTicketsStep(jiraservicemanagement.SearchTicketsStepConfig[DuplicateSearch]{
			StepType: findOpenDuplicateTicketsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Search the desk for the customer's open requests whose summary contains the requested summary."},
			Connection:  flow.connection, MapToOperationInput: MapToSearchTicketsInput,
			Searched: sdkgo.GoTo(evaluateDuplicateTickets{}),
		})),
		dex.DefineStep(evaluateDuplicateTickets{}),
		dex.DefineStep(jiraservicemanagement.NewCreateTicketStep(jiraservicemanagement.CreateTicketStepConfig[SupportRequest]{
			StepType: createSupportTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Raise the request once for the customer; an unknown outcome is reconciled, never raised again automatically."},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "supportRequestType", UnitID: jiraservicemanagement.UIUnitRequestTypePicker, Label: "Support request type",
				Description: "Choose the service desk and then the request type that new support requests use, such as Get IT help; the unit saves the desk ID, the request type ID, and its name, and the desk must match the Support service desk when both are saved. Leave it unsaved to use each Start Flow input's requestTypeId. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: jiraservicemanagement.UIRequestTypePickerPortServiceDeskID, JSONPointer: "/serviceDeskId"},
					{Port: jiraservicemanagement.UIRequestTypePickerPortRequestTypeID, JSONPointer: "/requestTypeId"},
					{Port: jiraservicemanagement.UIRequestTypePickerPortRequestTypeName, JSONPointer: "/requestTypeName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreateTicketInput,
			Created:          sdkgo.GoTo(recordCreatedTicket{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedTicket{}),
			Uncertain:        sdkgo.GoTo(recordUncertainTicket{}),
		})),
		dex.DefineStep(recordCreatedTicket{}),
		dex.DefineStep(recordRejectedTicket{}),
		dex.DefineStep(recordUncertainTicket{}),
		dex.DefineStep(jiraservicemanagement.NewGetTicketStep(jiraservicemanagement.GetTicketStepConfig[TicketReference]{
			StepType: readBackSupportTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Read the request back with its comments and SLAs to confirm its desk, customer, and summary."},
			Connection:  flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found:    sdkgo.GoTo(verifySupportTicket{}),
			NotFound: sdkgo.GoTo(recordMissingSupportTicket{}),
		})),
		dex.DefineStep(verifySupportTicket{}),
		dex.DefineStep(recordMissingSupportTicket{}),
		dex.DefineStep(planNextSupportAction{}),
		dex.DefineStep(jiraservicemanagement.NewUpdateTicketStep(jiraservicemanagement.UpdateTicketStepConfig[LabelChange]{
			StepType: labelSupportTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Add the labels and set the priority, skipping values the request already holds."},
			Connection:  flow.connection, MapToOperationInput: MapToUpdateTicketInput,
			Updated: sdkgo.GoTo(recordTicketLabels{}),
		})),
		dex.DefineStep(recordTicketLabels{}),
		dex.DefineStep(jiraservicemanagement.NewAddCommentStep(jiraservicemanagement.AddCommentStepConfig[CommentRequest]{
			StepType: addInternalNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Add the internal note once; an unknown outcome is recorded, never re-sent."},
			Connection:  flow.connection, MapToOperationInput: MapToInternalNoteInput,
			Added:     sdkgo.GoTo(recordInternalNote{}),
			Uncertain: sdkgo.GoTo(recordInternalNote{}),
		})),
		dex.DefineStep(recordInternalNote{}),
		dex.DefineStep(jiraservicemanagement.NewAddCommentStep(jiraservicemanagement.AddCommentStepConfig[CommentRequest]{
			StepType: sendPublicReplyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Send the public reply to the customer once; an unknown outcome is recorded, never re-sent."},
			Connection:  flow.connection, MapToOperationInput: MapToPublicReplyInput,
			Added:     sdkgo.GoTo(recordPublicReply{}),
			Uncertain: sdkgo.GoTo(recordPublicReply{}),
		})),
		dex.DefineStep(recordPublicReply{}),
		dex.DefineStep(jiraservicemanagement.NewTransitionTicketStep(jiraservicemanagement.TransitionTicketStepConfig[StatusChangeRequest]{
			StepType: moveSupportTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Move the request to the destination status, skipping a move Jira already made."},
			Connection:  flow.connection, MapToOperationInput: MapToTransitionTicketInput,
			Transitioned:          sdkgo.GoTo(completeSupportRequest{}),
			TransitionUnavailable: sdkgo.GoTo(recordUnavailableTransition{}),
		})),
		dex.DefineStep(recordUnavailableTransition{}),
		dex.DefineStep(completeSupportRequest{}),
	}
}

// GetRPCs returns the reconciliation Actions, the support request read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	reconciliationLocks := []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.ConfirmCreatedTicket, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Confirm created request",
				dex.WhenAttributeMatches(supportPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileSupportRequestPermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.ApproveTicketCreateRetry, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Raise request again",
				dex.WhenAttributeMatches(supportPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileSupportRequestPermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.GetSupportRequest, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and support request Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{supportPhaseAttribute, supportRequestAttribute}}
}

// MapToFindCustomerByEmailInput looks the requester up among the service desk's customers.
func MapToFindCustomerByEmailInput(request ValidatedRequest) jiraservicemanagement.FindCustomerByEmailInput {
	return jiraservicemanagement.FindCustomerByEmailInput{ServiceDeskID: request.ServiceDeskID, Email: request.RequesterEmail}
}

// MapToSearchTicketsInput searches the customer's open requests with the summary as a phrase.
func MapToSearchTicketsInput(search DuplicateSearch) jiraservicemanagement.SearchTicketsInput {
	return jiraservicemanagement.SearchTicketsInput{
		ProjectKey: search.ProjectKey, ReporterAccountIDs: []string{search.RequesterAccountID}, SummaryPhrase: search.Summary,
		StatusCategories: []jiraservicemanagement.StatusCategoryKey{jiraservicemanagement.StatusCategoryToDo, jiraservicemanagement.StatusCategoryInProgress},
		PageSize:         maximumDuplicateCandidates,
	}
}

// MapToCreateTicketInput raises one request for the customer.
func MapToCreateTicketInput(record SupportRequest) jiraservicemanagement.CreateTicketInput {
	return jiraservicemanagement.CreateTicketInput{
		ServiceDeskID: record.Request.ServiceDeskID, RequestTypeID: record.Request.RequestTypeID, Summary: record.Request.Summary,
		Description: record.Request.Description, RaiseOnBehalfOfAccountID: record.RequesterAccountID,
	}
}

// MapToGetTicketInput reads one request with its first comments and its SLAs.
func MapToGetTicketInput(reference TicketReference) jiraservicemanagement.GetTicketInput {
	return jiraservicemanagement.GetTicketInput{IssueIDOrKey: reference.IssueKey}
}

// MapToUpdateTicketInput adds the labels and sets the priority.
func MapToUpdateTicketInput(change LabelChange) jiraservicemanagement.UpdateTicketInput {
	return jiraservicemanagement.UpdateTicketInput{IssueIDOrKey: change.IssueKey, AddLabels: change.Labels, PriorityName: change.PriorityName}
}

// MapToInternalNoteInput adds an internal note that only agents see.
func MapToInternalNoteInput(request CommentRequest) jiraservicemanagement.AddCommentInput {
	return jiraservicemanagement.AddCommentInput{IssueIDOrKey: request.IssueKey, Body: request.Body, IsPublic: false}
}

// MapToPublicReplyInput sends a public reply that the customer sees.
func MapToPublicReplyInput(request CommentRequest) jiraservicemanagement.AddCommentInput {
	return jiraservicemanagement.AddCommentInput{IssueIDOrKey: request.IssueKey, Body: request.Body, IsPublic: true}
}

// MapToTransitionTicketInput moves the request by destination status, so a retried Step recognizes a move.
func MapToTransitionTicketInput(request StatusChangeRequest) jiraservicemanagement.TransitionTicketInput {
	return jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: request.IssueKey, DestinationStatusName: request.DestinationStatusName}
}

// FindActiveCustomer returns the first active customer of the lookup; Atlassian can hold several accounts per address.
func FindActiveCustomer(customers []jiraservicemanagement.Customer) (jiraservicemanagement.Customer, bool) {
	for _, customer := range customers {
		if customer.IsActive {
			return customer, true
		}
	}
	return jiraservicemanagement.Customer{}, false
}

// FindSameSummaryTicket returns the first open request whose summary equals summary without case or
// surrounding whitespace. Jira text search is partial, so near-duplicates are rejected here.
func FindSameSummaryTicket(tickets []jiraservicemanagement.Ticket, summary string) (jiraservicemanagement.Ticket, bool) {
	for _, ticket := range tickets {
		if ticket.Status.CategoryKey != jiraservicemanagement.StatusCategoryDone && strings.EqualFold(strings.TrimSpace(ticket.Summary), strings.TrimSpace(summary)) {
			return ticket, true
		}
	}
	return jiraservicemanagement.Ticket{}, false
}

// ConfirmCreatedTicket adopts a request an operator found in the service desk after an uncertain create.
// The Flow reads that key with getTicket and adopts it only when its desk, customer, and summary match.
//
// dex:input field-name:issueKey value-type:string source:user required:true description:"Key of the request found in the service desk, such as ITH-42"
func (*Flow) ConfirmCreatedTicket(ctx dex.Context, input ConfirmCreatedTicketInput) (*dex.RPCResult[dex.None], error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	issueKey := strings.ToUpper(strings.TrimSpace(input.IssueKey))
	if !isRequestKey(issueKey) {
		return nil, errReportedTicketKeyInvalid
	}
	record.Phase = PhaseVerifyingReportedTicket
	record.ReconciliationNote = ""
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[TicketReference](readBackSupportTicketStepType), TicketReference{IssueKey: issueKey})},
	}, nil
}

// ApproveTicketCreateRetry raises the request again after an operator confirmed the desk has no such request.
// The retry is a new Step execution with a new connector call ID.
func (*Flow) ApproveTicketCreateRetry(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	record.Phase = PhaseCreating
	record.CreateAttempts++
	record.UncertainCreate = nil
	record.ReconciliationNote = ""
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[SupportRequest](createSupportTicketStepType), record)},
	}, nil
}

// GetSupportRequest returns the current support request record.
func (*Flow) GetSupportRequest(ctx dex.Context, _ dex.None) (*dex.RPCResult[SupportRequest], error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[SupportRequest]{Output: record}, nil
}

// GetDexSummary returns the phase and record for Dex Web lists.
//
// dex:field attribute-key:jsm-support-request-phase value-type:string editable:false description:"Support request phase"
// dex:field attribute-key:jsm-support-request value-type:json editable:false description:"Customer, request key, labels, comments, and status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, supportPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, supportRequestAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"jsm-support-request-phase": phase,
		"jsm-support-request":       record,
	}}, nil
}

// GetDexDisplay returns the phase and record for the Dex Web run view.
//
// dex:field attribute-key:jsm-support-request-phase value-type:string editable:false description:"Support request phase" ui-slot:status
// dex:field attribute-key:jsm-support-request value-type:json editable:false description:"Request, read-back request with SLAs, labels, comments, status, and any uncertain create awaiting reconciliation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, supportPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, supportRequestAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"jsm-support-request-phase": phase,
		"jsm-support-request":       record,
	}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate the request and record it with the picked service desk and request type before calling Jira Service Management."
type recordSupportRequest struct {
	dex.StepDefaults
	desk        DeskSelection
	requestType RequestTypeSelection
}

func (recordSupportRequest) GetStepType() string { return recordSupportRequestStepType }

func (recordSupportRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordSupportRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordSupportRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildValidatedRequest(step.desk, step.requestType, input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := SupportRequest{Request: request, Phase: PhaseFindingRequester}
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ValidatedRequest](findRequesterStepType), request), nil
}

// BuildValidatedRequest validates Start Flow input. A picked service desk and request type win over the input's IDs.
func BuildValidatedRequest(desk DeskSelection, requestType RequestTypeSelection, input Input) (ValidatedRequest, error) {
	request := ValidatedRequest{
		ServiceDeskID: firstNonBlank(desk.ServiceDeskID, requestType.ServiceDeskID, input.ServiceDeskID),
		ProjectKey:    firstNonBlank(desk.ProjectKey, input.ProjectKey), RequestTypeID: firstNonBlank(requestType.RequestTypeID, input.RequestTypeID),
		RequesterEmail: strings.TrimSpace(input.RequesterEmail), Summary: strings.TrimSpace(input.Summary), Description: input.Description,
		Labels: input.Labels, PriorityName: strings.TrimSpace(input.PriorityName), InternalNote: strings.TrimSpace(input.InternalNote),
		PublicReply: strings.TrimSpace(input.PublicReply), DestinationStatusName: strings.TrimSpace(input.DestinationStatusName),
	}
	switch {
	case request.ServiceDeskID == "" || request.ProjectKey == "":
		return ValidatedRequest{}, errors.New("pick a service desk on the FindRequester Step or set serviceDeskId and projectKey")
	case request.RequestTypeID == "":
		return ValidatedRequest{}, errors.New("pick a request type on the CreateSupportTicket Step or set requestTypeId")
	case request.RequesterEmail == "":
		return ValidatedRequest{}, errors.New("requesterEmail is required")
	case request.Summary == "":
		return ValidatedRequest{}, errors.New("summary is required")
	case strings.ContainsAny(request.Summary, "\r\n"):
		return ValidatedRequest{}, errors.New("summary must be one line")
	}
	return request, nil
}

// dex:group group-id:requester group-label:"Requester"
// dex:explanation text:"Use the first active customer with the email address, or stop when every match is inactive."
type evaluateRequester struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.FindCustomerByEmailResult]
}

func (evaluateRequester) GetStepType() string { return evaluateRequesterStepType }

func (evaluateRequester) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (evaluateRequester) Execute(ctx dex.Context, result jiraservicemanagement.FindCustomerByEmailResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	customer, isFound := FindActiveCustomer(result.Value.Customers)
	if !isFound {
		record.Phase = PhaseRequesterNotFound
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(record), nil
	}
	record.RequesterAccountID = customer.AccountID
	record.Phase = PhaseSearching
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DuplicateSearch](findOpenDuplicateTicketsStepType), DuplicateSearch{
		ProjectKey: record.Request.ProjectKey, RequesterAccountID: customer.AccountID, Summary: record.Request.Summary,
	}), nil
}

// dex:group group-id:requester group-label:"Requester"
// dex:explanation text:"Complete without raising a request when the service desk has no customer with the email address."
type recordUnknownRequester struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.FindCustomerByEmailResult]
}

func (recordUnknownRequester) GetStepType() string { return recordUnknownRequesterStepType }

func (recordUnknownRequester) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordUnknownRequester) Execute(ctx dex.Context, _ jiraservicemanagement.FindCustomerByEmailResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseRequesterNotFound
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Reuse the customer's open request with exactly the same summary, otherwise raise a new one."
type evaluateDuplicateTickets struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.SearchTicketsResult]
}

func (evaluateDuplicateTickets) GetStepType() string { return evaluateDuplicateTicketsStepType }

func (evaluateDuplicateTickets) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (evaluateDuplicateTickets) Execute(ctx dex.Context, result jiraservicemanagement.SearchTicketsResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if ticket, isFound := FindSameSummaryTicket(result.Value.Tickets, record.Request.Summary); isFound {
		record.Phase = PhaseVerifyingTicket
		record.IsExistingTicketReused = true
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[TicketReference](readBackSupportTicketStepType), TicketReference{IssueKey: ticket.Key}), nil
	}
	record.Phase = PhaseCreating
	record.CreateAttempts = 1
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SupportRequest](createSupportTicketStepType), record), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the raised request key and read the request back."
type recordCreatedTicket struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.CreateTicketResult]
}

func (recordCreatedTicket) GetStepType() string { return recordCreatedTicketStepType }

func (recordCreatedTicket) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordCreatedTicket) Execute(ctx dex.Context, result jiraservicemanagement.CreateTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseVerifyingTicket
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TicketReference](readBackSupportTicketStepType), TicketReference{IssueKey: result.Value.IssueKey}), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Complete with the rejected field IDs and error key after Jira Service Management conclusively refused the create."
type recordRejectedTicket struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.CreateTicketResult]
}

func (recordRejectedTicket) GetStepType() string { return recordRejectedTicketStepType }

func (recordRejectedTicket) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordRejectedTicket) Execute(ctx dex.Context, result jiraservicemanagement.CreateTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseRejected
	record.RejectedFieldIDs, record.ProviderErrorKey = result.Value.RejectedFieldIDs, result.Value.ProviderErrorKey
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Park an unknown create for an operator instead of raising the request again."
type recordUncertainTicket struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.CreateTicketResult]
}

func (recordUncertainTicket) GetStepType() string { return recordUncertainTicketStepType }

func (recordUncertainTicket) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordUncertainTicket) Execute(ctx dex.Context, result jiraservicemanagement.CreateTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseNeedsReconciliation
	record.UncertainCreate = &UncertainCreate{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		record.UncertainCreate.FailureKind = result.Failure.Kind
	}
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Adopt the read-back request, or return a mismatched reported request to the operator."
type verifySupportTicket struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.GetTicketResult]
}

func (verifySupportTicket) GetStepType() string { return verifySupportTicketStepType }

func (verifySupportTicket) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (verifySupportTicket) Execute(ctx dex.Context, result jiraservicemanagement.GetTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	ticket := result.Value.Ticket
	isSameRequest := ticket.ProjectKey == record.Request.ProjectKey && strings.EqualFold(strings.TrimSpace(ticket.Summary), record.Request.Summary) &&
		ticket.Reporter != nil && ticket.Reporter.AccountID == record.RequesterAccountID
	if record.Phase == PhaseVerifyingReportedTicket && !isSameRequest {
		record.Phase = PhaseNeedsReconciliation
		record.ReconciliationNote = NoteReportedTicketMismatch
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.DeadEnd(), nil
	}
	record.Ticket = &ticket
	record.UncertainCreate = nil
	record.SLANames = DescribeSLAs(result.Value.SLAs)
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(planNextSupportAction{}, TicketReference{IssueKey: ticket.Key}), nil
}

// DescribeSLAs names each SLA and marks a running, paused, or breached ongoing cycle, such as
// "Time to resolution (running, breached)".
func DescribeSLAs(slas []jiraservicemanagement.TicketSLA) []string {
	var descriptions []string
	for _, sla := range slas {
		description := sla.Name
		if cycle := sla.OngoingCycle; cycle != nil {
			states := []string{"running"}
			if cycle.IsPaused {
				states = []string{"paused"}
			}
			if cycle.IsBreached {
				states = append(states, "breached")
			}
			description += " (" + strings.Join(states, ", ") + ")"
		}
		descriptions = append(descriptions, description)
	}
	return descriptions
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Return a reported request key Jira cannot find to the operator."
type recordMissingSupportTicket struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.GetTicketResult]
}

func (recordMissingSupportTicket) GetStepType() string { return recordMissingSupportTicketStepType }

func (recordMissingSupportTicket) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordMissingSupportTicket) Execute(ctx dex.Context, _ jiraservicemanagement.GetTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseVerifyingReportedTicket {
		return dex.ForceFail("the support request can no longer be read from Jira Service Management"), nil
	}
	record.Phase = PhaseNeedsReconciliation
	record.ReconciliationNote = NoteReportedTicketMissing
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:actions group-label:"Actions"
// dex:explanation text:"Choose the next unfinished action: labels and priority, internal note, public reply, move, or completion."
type planNextSupportAction struct {
	dex.StepDefaultsNoWaitFor[TicketReference]
}

func (planNextSupportAction) GetStepType() string { return planNextSupportActionStepType }

func (planNextSupportAction) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (planNextSupportAction) Execute(ctx dex.Context, reference TicketReference) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	request := record.Request
	switch {
	case (len(request.Labels) > 0 || request.PriorityName != "") && !record.IsTicketUpdated:
		record.Phase = PhaseLabeling
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[LabelChange](labelSupportTicketStepType), LabelChange{
			IssueKey: reference.IssueKey, Labels: request.Labels, PriorityName: request.PriorityName,
		}), nil
	case request.InternalNote != "" && record.InternalNote == nil:
		record.Phase = PhaseNoting
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[CommentRequest](addInternalNoteStepType), CommentRequest{IssueKey: reference.IssueKey, Body: request.InternalNote}), nil
	case request.PublicReply != "" && record.PublicReply == nil:
		record.Phase = PhaseReplying
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[CommentRequest](sendPublicReplyStepType), CommentRequest{IssueKey: reference.IssueKey, Body: request.PublicReply}), nil
	case request.DestinationStatusName != "":
		record.Phase = PhaseTransitioning
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[StatusChangeRequest](moveSupportTicketStepType), StatusChangeRequest{
			IssueKey: reference.IssueKey, DestinationStatusName: request.DestinationStatusName,
		}), nil
	default:
		record.Phase = PhaseHandled
		if record.Ticket != nil {
			record.Status = &record.Ticket.Status
		}
		if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := supportRequestAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(record), nil
	}
}

// dex:group group-id:actions group-label:"Actions"
// dex:explanation text:"Record the request's labels after the label Step and plan the next action."
type recordTicketLabels struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.UpdateTicketResult]
}

func (recordTicketLabels) GetStepType() string { return recordTicketLabelsStepType }

func (recordTicketLabels) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordTicketLabels) Execute(ctx dex.Context, result jiraservicemanagement.UpdateTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Labels, record.IsTicketUpdated = result.Value.Labels, true
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(planNextSupportAction{}, TicketReference{IssueKey: result.Value.IssueKey}), nil
}

// dex:group group-id:actions group-label:"Actions"
// dex:explanation text:"Record the internal note ID, or that its outcome is unknown, then plan the next action without re-sending."
type recordInternalNote struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.AddCommentResult]
}

func (recordInternalNote) GetStepType() string { return recordInternalNoteStepType }

func (recordInternalNote) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordInternalNote) Execute(ctx dex.Context, result jiraservicemanagement.AddCommentResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.InternalNote = &CommentOutcome{
		CommentID: result.Value.CommentID, IsPublic: result.Value.IsPublic, IsOutcomeUnknown: result.Branch == jiraservicemanagement.AddCommentBranchUncertain,
	}
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(planNextSupportAction{}, TicketReference{IssueKey: result.Value.IssueIDOrKey}), nil
}

// dex:group group-id:actions group-label:"Actions"
// dex:explanation text:"Record the public reply ID, or that its outcome is unknown, then plan the next action without re-sending."
type recordPublicReply struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.AddCommentResult]
}

func (recordPublicReply) GetStepType() string { return recordPublicReplyStepType }

func (recordPublicReply) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordPublicReply) Execute(ctx dex.Context, result jiraservicemanagement.AddCommentResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.PublicReply = &CommentOutcome{
		CommentID: result.Value.CommentID, IsPublic: result.Value.IsPublic, IsOutcomeUnknown: result.Branch == jiraservicemanagement.AddCommentBranchUncertain,
	}
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(planNextSupportAction{}, TicketReference{IssueKey: result.Value.IssueIDOrKey}), nil
}

// dex:group group-id:actions group-label:"Actions"
// dex:explanation text:"Complete with the offered transitions when none reaches the destination status."
type recordUnavailableTransition struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.TransitionTicketResult]
}

func (recordUnavailableTransition) GetStepType() string { return recordUnavailableTransitionStepType }

func (recordUnavailableTransition) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (recordUnavailableTransition) Execute(ctx dex.Context, result jiraservicemanagement.TransitionTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseTransitionUnavailable
	status := result.Value.Status
	record.Status = &status
	record.AvailableTransitionNames = nil
	for _, transition := range result.Value.AvailableTransitions {
		record.AvailableTransitionNames = append(record.AvailableTransitionNames, transition.Name+" -> "+transition.DestinationStatus.Name)
	}
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:actions group-label:"Actions"
// dex:explanation text:"Record the request's destination status and complete the support request."
type completeSupportRequest struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.TransitionTicketResult]
}

func (completeSupportRequest) GetStepType() string { return completeSupportRequestStepType }

func (completeSupportRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(supportPhaseAttribute), dex.LockAttribute(supportRequestAttribute)}}
}

func (completeSupportRequest) Execute(ctx dex.Context, result jiraservicemanagement.TransitionTicketResult) (*dex.StepDecision, error) {
	record, err := supportRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	status := result.Value.Status
	record.Status = &status
	record.Phase = PhaseHandled
	if err := supportPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := supportRequestAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// isRequestKey accepts an uppercase request key such as ITH-42.
func isRequestKey(value string) bool {
	projectKey, number, hasSeparator := strings.Cut(value, "-")
	if !hasSeparator || projectKey == "" || number == "" || number[0] == '0' || projectKey[0] < 'A' || projectKey[0] > 'Z' || len(number) > 18 {
		return false
	}
	for _, character := range projectKey {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	for _, character := range number {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[ConfirmCreatedTicketInput, dex.None] = (*Flow)(nil).ConfirmCreatedTicket
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).ApproveTicketCreateRetry
var _ dex.RPC[dex.None, SupportRequest] = (*Flow)(nil).GetSupportRequest
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
