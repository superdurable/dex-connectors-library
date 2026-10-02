// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package incidentacknowledgement demonstrates every Microsoft Teams operation in one Flow started from
// Dex Web Start Flow: post an incident update to a channel, reply in its thread with the status, read the
// thread's replies on a durable Timer until a person acknowledges, and escalate to a chat when nobody does.
package incidentacknowledgement

import (
	"errors"
	"html"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "TeamsIncidentAcknowledgement"
	// ConnectionName is the static Dex Web connection for Microsoft Teams.
	ConnectionName = "microsoft-teams"

	recordIncidentUpdateStepType    = "RecordIncidentUpdate"
	postIncidentUpdateStepType      = "PostIncidentUpdate"
	recordIncidentPostStepType      = "RecordIncidentPost"
	recordRejectedPostStepType      = "RecordRejectedPost"
	recordUncertainPostStepType     = "RecordUncertainPost"
	postStatusReplyStepType         = "PostStatusReply"
	recordStatusReplyStepType       = "RecordStatusReply"
	waitForAcknowledgementStepType  = "WaitForAcknowledgement"
	readThreadRepliesStepType       = "ReadThreadReplies"
	checkAcknowledgementStepType    = "CheckAcknowledgement"
	recordUnreadableRepliesStepType = "RecordUnreadableReplies"
	escalateToChatStepType          = "EscalateToChat"
	recordEscalationStepType        = "RecordEscalation"

	defaultAcknowledgementPhrase = "ack"
	maximumPhraseCharacters      = 64
	maximumTitleCharacters       = 200
	replyPageSize                = 50
	replyTextCharacters          = 2000
)

// Incident phases stored in the teams-incident-phase Attribute.
const (
	// PhasePostingUpdate means the channel post or the status reply is about to run or running.
	PhasePostingUpdate = "postingUpdate"
	// PhaseAwaitingAcknowledgement means the Flow reads the thread's replies on a Timer.
	PhaseAwaitingAcknowledgement = "awaitingAcknowledgement"
	// PhaseEscalating means nobody acknowledged and the escalation chat message is about to run or running.
	PhaseEscalating = "escalating"
	// PhaseAcknowledged means a person replied with the acknowledgement phrase.
	PhaseAcknowledged = "acknowledged"
	// PhaseEscalated means nobody acknowledged and the escalation chat message ran.
	PhaseEscalated = "escalated"
	// PhaseUnacknowledged means nobody acknowledged and no escalation chat was picked.
	PhaseUnacknowledged = "unacknowledged"
	// PhaseRejected means Microsoft Teams refused the channel post; nothing was posted.
	PhaseRejected = "rejected"
	// PhasePostOutcomeUnknown means the channel post may or may not exist; it is never re-sent.
	PhasePostOutcomeUnknown = "postOutcomeUnknown"
	// PhaseRepliesUnreadable means the thread's replies cannot be read, for example without administrator consent.
	PhaseRepliesUnreadable = "repliesUnreadable"
)

var (
	incidentPhaseAttribute = dex.DefineAttribute[string]("teams-incident-phase")
	incidentAttribute      = dex.DefineAttribute[IncidentAcknowledgement]("teams-incident")
)

// Input is the incident update entered in Dex Web Start Flow.
type Input struct {
	// IncidentID names the incident, such as INC-1042.
	IncidentID string `json:"incidentId"`
	// Title is a one-line incident title.
	Title string `json:"title"`
	// Severity is a short label such as SEV2.
	Severity string `json:"severity"`
	// Summary is the update text; line breaks are kept.
	Summary string `json:"summary"`
	// AcknowledgementPhrase is the word or phrase a person replies with; blank means ack.
	AcknowledgementPhrase string `json:"acknowledgementPhrase,omitempty"`
}

// ChannelSelection is the value the team and channel pickers save for the PostIncidentUpdate Step.
type ChannelSelection struct {
	// TeamID is the picked team's GUID.
	TeamID string `json:"teamId"`
	// TeamName is the picked team's display name.
	TeamName string `json:"teamName,omitempty"`
	// ChannelID is the picked channel's ID.
	ChannelID string `json:"channelId"`
	// ChannelName is the picked channel's display name.
	ChannelName string `json:"channelName,omitempty"`
}

// EscalationChatSelection is the value the chat picker saves for the EscalateToChat Step; blank skips escalation.
type EscalationChatSelection struct {
	// ChatID is the picked chat's ID.
	ChatID string `json:"chatId,omitempty"`
	// ChatName is the picked chat's label.
	ChatName string `json:"chatName,omitempty"`
}

// AcknowledgementPolicy bounds how long the Flow waits for an acknowledgement.
type AcknowledgementPolicy struct {
	// CheckInterval is the durable Timer before each read of the thread's replies; at least one second.
	CheckInterval time.Duration
	// MaximumChecks is the number of reads before the Flow escalates or completes as unacknowledged.
	MaximumChecks int
}

// DefaultAcknowledgementPolicy reads the replies every 30 seconds for ten minutes.
func DefaultAcknowledgementPolicy() AcknowledgementPolicy {
	return AcknowledgementPolicy{CheckInterval: 30 * time.Second, MaximumChecks: 20}
}

// IncidentUpdate is the validated update every later Step reads.
type IncidentUpdate struct {
	// IncidentID names the incident.
	IncidentID string `json:"incidentId"`
	// Title is the one-line title.
	Title string `json:"title"`
	// Severity is the severity label.
	Severity string `json:"severity"`
	// Summary is the update text.
	Summary string `json:"summary"`
	// AcknowledgementPhrase is the lowercase phrase a person replies with.
	AcknowledgementPhrase string `json:"acknowledgementPhrase"`
}

// StatusReplyRequest is the status reply the postThreadReply Step sends.
type StatusReplyRequest struct {
	// TeamID is the team of the incident post.
	TeamID string `json:"teamId"`
	// ChannelID is the channel of the incident post.
	ChannelID string `json:"channelId"`
	// RootMessageID is the incident post.
	RootMessageID string `json:"rootMessageId"`
	// Content is the reply text.
	Content string `json:"content"`
}

// ThreadCheck identifies the thread whose replies the Flow reads.
type ThreadCheck struct {
	// TeamID is the team of the incident post.
	TeamID string `json:"teamId"`
	// ChannelID is the channel of the incident post.
	ChannelID string `json:"channelId"`
	// RootMessageID is the incident post.
	RootMessageID string `json:"rootMessageId"`
}

// EscalationRequest is the chat message the postChatMessage Step sends.
type EscalationRequest struct {
	// ChatID is the escalation chat.
	ChatID string `json:"chatId"`
	// Content is the message text.
	Content string `json:"content"`
}

// Acknowledgement is the reply that acknowledged the incident.
type Acknowledgement struct {
	// MessageID is the reply's ID.
	MessageID string `json:"messageId"`
	// UserID is the Microsoft Entra object ID of the person who replied.
	UserID string `json:"userId"`
	// DisplayName is that person's display name.
	DisplayName string `json:"displayName,omitempty"`
	// RepliedAt is when Teams stored the reply.
	RepliedAt time.Time `json:"repliedAt"`
}

// IncidentAcknowledgement is the Flow's durable record of one incident update.
type IncidentAcknowledgement struct {
	// Update is the validated update.
	Update IncidentUpdate `json:"update"`
	// Phase mirrors the teams-incident-phase Attribute.
	Phase string `json:"phase"`
	// TeamID is the picked team.
	TeamID string `json:"teamId"`
	// ChannelID is the picked channel.
	ChannelID string `json:"channelId"`
	// ChannelName is the picked channel's display name.
	ChannelName string `json:"channelName,omitempty"`
	// RootMessageID is the incident post.
	RootMessageID string `json:"rootMessageId,omitempty"`
	// RootMessageURL opens the incident post in Teams.
	RootMessageURL string `json:"rootMessageUrl,omitempty"`
	// PosterUserID is the connected account; its own replies never count as an acknowledgement.
	PosterUserID string `json:"posterUserId,omitempty"`
	// IsRootConfirmedByReadBack reports that the post's response was lost and the connector found it.
	IsRootConfirmedByReadBack bool `json:"isRootConfirmedByReadBack,omitempty"`
	// StatusReplyID is the status reply, when Teams confirmed it.
	StatusReplyID string `json:"statusReplyId,omitempty"`
	// IsStatusReplyOutcomeUnknown reports a status reply Teams may or may not have stored; it is never re-sent.
	IsStatusReplyOutcomeUnknown bool `json:"isStatusReplyOutcomeUnknown,omitempty"`
	// IsStatusReplyRejected reports a status reply Teams refused; the Flow still waits on the thread.
	IsStatusReplyRejected bool `json:"isStatusReplyRejected,omitempty"`
	// ReplyChecks counts the reads of the thread's replies.
	ReplyChecks int `json:"replyChecks"`
	// Acknowledgement is the acknowledging reply.
	Acknowledgement *Acknowledgement `json:"acknowledgement,omitempty"`
	// EscalationChatID is the chat the escalation went to.
	EscalationChatID string `json:"escalationChatId,omitempty"`
	// EscalationMessageID is the escalation chat message, when Teams confirmed it.
	EscalationMessageID string `json:"escalationMessageId,omitempty"`
	// IsEscalationOutcomeUnknown reports an escalation Teams may or may not have stored; it is never re-sent.
	IsEscalationOutcomeUnknown bool `json:"isEscalationOutcomeUnknown,omitempty"`
	// FailureKind is the safe failure category of a refused post, a refused escalation, or unreadable replies.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
}

// Flow posts one incident update and waits for a person to acknowledge it.
type Flow struct {
	dex.FlowDefaults
	connection teams.Connection
	channel    ChannelSelection
	escalation EscalationChatSelection
	policy     AcknowledgementPolicy
}

// NewFlow binds the Teams Connection, the picked channel, the optional escalation chat, and the policy at
// registration time. It panics when the policy's interval is below one second or it allows no checks.
func NewFlow(connection teams.Connection, channel ChannelSelection, escalation EscalationChatSelection, policy *AcknowledgementPolicy) *Flow {
	if policy == nil || policy.CheckInterval < time.Second || policy.MaximumChecks < 1 {
		panic("incident acknowledgement requires a check interval of at least one second and at least one check")
	}
	channel.TeamID, channel.ChannelID = strings.TrimSpace(channel.TeamID), strings.TrimSpace(channel.ChannelID)
	escalation.ChatID = strings.TrimSpace(escalation.ChatID)
	return &Flow{connection: connection, channel: channel, escalation: escalation, policy: *policy}
}

// ChannelSelectionConfigurationRef identifies the team and channel picker values saved in Dex Web.
func ChannelSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: teams.ConnectorID, ConnectionName: ConnectionName, OperationID: "postChannelMessage",
		FlowType: FlowType, StepType: postIncidentUpdateStepType,
	}
}

// EscalationChatSelectionConfigurationRef identifies the chat picker value saved in Dex Web.
func EscalationChatSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: teams.ConnectorID, ConnectionName: ConnectionName, OperationID: "postChatMessage",
		FlowType: FlowType, StepType: escalateToChatStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Microsoft Teams connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordIncidentUpdate{}),
		dex.DefineStep(teams.NewPostChannelMessageStep(teams.PostChannelMessageStepConfig[IncidentUpdate]{
			StepType: postIncidentUpdateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "teams", GroupLabel: "Microsoft Teams",
				Explanation: "Post the incident update to the picked channel once; an unknown outcome is never re-sent.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "incidentTeam", UnitID: teams.UIUnitTeamPicker, Label: "Incident team", Required: true,
					Description: "Choose the team whose channel receives incident updates; the picker lists the teams the connected account belongs to and stores the team's GUID and name. The Worker does not start until a team and channel are saved; restart it after saving.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: teams.UITeamPickerPortTeamID, JSONPointer: "/teamId"},
						{Port: teams.UITeamPickerPortTeamName, JSONPointer: "/teamName"},
					},
				},
				{
					ID: "incidentChannel", UnitID: teams.UIUnitChannelPicker, Label: "Incident channel", Required: true,
					Description: "Choose the channel of the incident team where the update is posted and the Flow reads replies for the acknowledgement; save the team first. The picker stores the channel ID and name, and the connected account must be a member of a private or shared channel.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: teams.UIChannelPickerPortTeamID, JSONPointer: "/teamId"},
						{Port: teams.UIChannelPickerPortChannelID, JSONPointer: "/channelId"},
						{Port: teams.UIChannelPickerPortChannelName, JSONPointer: "/channelName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToPostChannelMessageInput,
			Sent:             sdkgo.GoTo(recordIncidentPost{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedPost{}),
			Uncertain:        sdkgo.GoTo(recordUncertainPost{}),
		})),
		dex.DefineStep(recordIncidentPost{}),
		dex.DefineStep(recordRejectedPost{}),
		dex.DefineStep(recordUncertainPost{}),
		dex.DefineStep(teams.NewPostThreadReplyStep(teams.PostThreadReplyStepConfig[StatusReplyRequest]{
			StepType: postStatusReplyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "teams", GroupLabel: "Microsoft Teams",
				Explanation: "Reply in the incident thread with the status and how to acknowledge, once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToPostThreadReplyInput,
			Sent:             sdkgo.GoTo(recordStatusReply{}),
			ProviderRejected: sdkgo.GoTo(recordStatusReply{}),
			Uncertain:        sdkgo.GoTo(recordStatusReply{}),
		})),
		dex.DefineStep(recordStatusReply{}),
		dex.DefineStep(waitForAcknowledgement{checkInterval: flow.policy.CheckInterval}),
		dex.DefineStep(teams.NewListThreadRepliesStep(teams.ListThreadRepliesStepConfig[ThreadCheck]{
			StepType: readThreadRepliesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "teams", GroupLabel: "Microsoft Teams",
				Explanation: "Read the newest 50 replies in the incident thread.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListThreadRepliesInput,
			Read:             sdkgo.GoTo(checkAcknowledgement{maximumChecks: flow.policy.MaximumChecks, escalationChatID: flow.escalation.ChatID}),
			NotFound:         sdkgo.GoTo(recordUnreadableReplies{}),
			ProviderRejected: sdkgo.GoTo(recordUnreadableReplies{}),
			InvalidResponse:  sdkgo.GoTo(recordUnreadableReplies{}),
		})),
		dex.DefineStep(checkAcknowledgement{maximumChecks: flow.policy.MaximumChecks, escalationChatID: flow.escalation.ChatID}),
		dex.DefineStep(recordUnreadableReplies{}),
		dex.DefineStep(teams.NewPostChatMessageStep(teams.PostChatMessageStepConfig[EscalationRequest]{
			StepType: escalateToChatStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "teams", GroupLabel: "Microsoft Teams",
				Explanation: "Tell the escalation chat that nobody acknowledged the incident, once.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "escalationChat", UnitID: teams.UIUnitChatPicker, Label: "Escalation chat",
				Description: "Optionally choose the chat, such as the on-call manager's one-on-one chat, that is told when nobody acknowledges the incident before the Flow's last reply check; the picker stores the chat ID and label. Leave it unsaved to complete as unacknowledged without escalating. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: teams.UIChatPickerPortChatID, JSONPointer: "/chatId"},
					{Port: teams.UIChatPickerPortChatName, JSONPointer: "/chatName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToPostChatMessageInput,
			Sent:             sdkgo.GoTo(recordEscalation{}),
			ProviderRejected: sdkgo.GoTo(recordEscalation{}),
			Uncertain:        sdkgo.GoTo(recordEscalation{}),
		})),
		dex.DefineStep(recordEscalation{}),
	}
}

// GetRPCs returns the incident read RPC and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetIncidentAcknowledgement, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and incident Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{incidentPhaseAttribute, incidentAttribute}}
}

// MapToPostChannelMessageInput posts the update to the picked channel with the incident as the subject.
func (flow *Flow) MapToPostChannelMessageInput(update IncidentUpdate) teams.PostChannelMessageInput {
	return teams.PostChannelMessageInput{
		TeamID: flow.channel.TeamID, ChannelID: flow.channel.ChannelID,
		Subject: update.IncidentID + " [" + update.Severity + "]: " + update.Title,
		Content: BuildIncidentUpdateHTML(update), ContentType: teams.ContentTypeHTML, Importance: teams.MessageImportanceHigh,
	}
}

// MapToPostThreadReplyInput replies to the incident post with the status text.
func MapToPostThreadReplyInput(request StatusReplyRequest) teams.PostThreadReplyInput {
	return teams.PostThreadReplyInput{TeamID: request.TeamID, ChannelID: request.ChannelID, MessageID: request.RootMessageID, Content: request.Content}
}

// MapToListThreadRepliesInput reads the newest page of replies with bounded text.
func MapToListThreadRepliesInput(check ThreadCheck) teams.ListThreadRepliesInput {
	return teams.ListThreadRepliesInput{
		TeamID: check.TeamID, ChannelID: check.ChannelID, MessageID: check.RootMessageID,
		PageSize: replyPageSize, MaxTextCharacters: replyTextCharacters,
	}
}

// MapToPostChatMessageInput sends the escalation text to the picked chat as urgent.
func MapToPostChatMessageInput(request EscalationRequest) teams.PostChatMessageInput {
	return teams.PostChatMessageInput{ChatID: request.ChatID, Content: request.Content, Importance: teams.MessageImportanceUrgent}
}

// BuildIncidentUpdateHTML renders the summary as escaped HTML that keeps its line breaks.
func BuildIncidentUpdateHTML(update IncidentUpdate) string {
	lines := strings.Split(strings.ReplaceAll(update.Summary, "\r\n", "\n"), "\n")
	for index, line := range lines {
		lines[index] = html.EscapeString(line)
	}
	return "<p>" + strings.Join(lines, "<br>") + "</p>"
}

// BuildStatusReplyText tells the channel how to acknowledge.
func BuildStatusReplyText(update IncidentUpdate) string {
	return "Status: " + update.Severity + " incident " + update.IncidentID + " is being investigated. Reply " +
		update.AcknowledgementPhrase + " in this thread to acknowledge."
}

// BuildIncidentUpdate validates Start Flow input.
func BuildIncidentUpdate(input Input) (IncidentUpdate, error) {
	update := IncidentUpdate{
		IncidentID: strings.TrimSpace(input.IncidentID), Title: strings.TrimSpace(input.Title),
		Severity: strings.TrimSpace(input.Severity), Summary: strings.TrimSpace(input.Summary),
		AcknowledgementPhrase: strings.ToLower(strings.TrimSpace(input.AcknowledgementPhrase)),
	}
	if update.AcknowledgementPhrase == "" {
		update.AcknowledgementPhrase = defaultAcknowledgementPhrase
	}
	switch {
	case update.IncidentID == "" || strings.ContainsAny(update.IncidentID, " \r\n"):
		return IncidentUpdate{}, errors.New("incidentId is required and has no spaces")
	case update.Title == "" || strings.ContainsAny(update.Title, "\r\n") || utf8.RuneCountInString(update.Title) > maximumTitleCharacters:
		return IncidentUpdate{}, errors.New("title is required, one line, and at most 200 characters")
	case update.Severity == "" || strings.ContainsAny(update.Severity, " \r\n[]"):
		return IncidentUpdate{}, errors.New("severity is required, such as SEV2")
	case update.Summary == "":
		return IncidentUpdate{}, errors.New("summary is required")
	case strings.ContainsAny(update.AcknowledgementPhrase, "\r\n") || utf8.RuneCountInString(update.AcknowledgementPhrase) > maximumPhraseCharacters:
		return IncidentUpdate{}, errors.New("acknowledgementPhrase is one line of at most 64 characters")
	}
	return update, nil
}

// FindAcknowledgement returns the earliest reply by a person other than the poster whose text contains the
// phrase as whole words; deleted replies, app and system messages, and the status reply never count.
func FindAcknowledgement(replies []teams.Message, record IncidentAcknowledgement) (Acknowledgement, bool) {
	var found *teams.Message
	for index := range replies {
		reply := &replies[index]
		if reply.MessageType != "message" || reply.IsDeleted || reply.Sender.UserID == "" ||
			reply.Sender.UserID == record.PosterUserID || reply.ID == record.StatusReplyID ||
			!ContainsWholePhrase(reply.Text, record.Update.AcknowledgementPhrase) {
			continue
		}
		if found == nil || reply.CreatedAt.Before(found.CreatedAt) {
			found = reply
		}
	}
	if found == nil {
		return Acknowledgement{}, false
	}
	return Acknowledgement{MessageID: found.ID, UserID: found.Sender.UserID, DisplayName: found.Sender.DisplayName, RepliedAt: found.CreatedAt}, true
}

// ContainsWholePhrase matches phrase case-insensitively where it is not part of a longer word, so ack does
// not match back or acknowledged.
func ContainsWholePhrase(text string, phrase string) bool {
	text, phrase = strings.ToLower(text), strings.ToLower(phrase)
	if phrase == "" {
		return false
	}
	for start := 0; start <= len(text)-len(phrase); {
		index := strings.Index(text[start:], phrase)
		if index < 0 {
			return false
		}
		begin, end := start+index, start+index+len(phrase)
		before, _ := utf8.DecodeLastRuneInString(text[:begin])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !isWordCharacter(before) && !isWordCharacter(after) {
			return true
		}
		start = begin + 1
	}
	return false
}

func isWordCharacter(character rune) bool {
	return character != utf8.RuneError && (unicode.IsLetter(character) || unicode.IsDigit(character))
}

// GetIncidentAcknowledgement returns the current incident record.
func (*Flow) GetIncidentAcknowledgement(ctx dex.Context, _ dex.None) (*dex.RPCResult[IncidentAcknowledgement], error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[IncidentAcknowledgement]{Output: record}, nil
}

// GetDexSummary returns the phase and incident record for Dex Web lists.
//
// dex:field attribute-key:teams-incident-phase value-type:string editable:false description:"Incident phase"
// dex:field attribute-key:teams-incident value-type:json editable:false description:"Incident, channel post, acknowledgement, and escalation"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, incidentPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, incidentAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"teams-incident-phase": phase,
		"teams-incident":       record,
	}}, nil
}

// GetDexDisplay returns the phase and incident record for the Dex Web run view.
//
// dex:field attribute-key:teams-incident-phase value-type:string editable:false description:"Incident phase" ui-slot:status
// dex:field attribute-key:teams-incident value-type:json editable:false description:"Update, channel post and status reply, reply checks, the acknowledging person, and any escalation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, incidentPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, incidentAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"teams-incident-phase": phase,
		"teams-incident":       record,
	}}, nil
}

// dex:group group-id:incident group-label:"Incident"
// dex:explanation text:"Validate the incident update and record it before posting to Teams."
type recordIncidentUpdate struct {
	dex.StepDefaults
}

func (recordIncidentUpdate) GetStepType() string { return recordIncidentUpdateStepType }

func (recordIncidentUpdate) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordIncidentUpdate) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordIncidentUpdate) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	update, err := BuildIncidentUpdate(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := IncidentAcknowledgement{Update: update, Phase: PhasePostingUpdate}
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IncidentUpdate](postIncidentUpdateStepType), update), nil
}

// dex:group group-id:posting group-label:"Posting"
// dex:explanation text:"Record the incident post and its sender, then reply in its thread."
type recordIncidentPost struct {
	dex.StepDefaultsNoWaitFor[teams.PostChannelMessageResult]
}

func (recordIncidentPost) GetStepType() string { return recordIncidentPostStepType }

func (recordIncidentPost) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (recordIncidentPost) Execute(ctx dex.Context, result teams.PostChannelMessageResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.TeamID, record.ChannelID, record.RootMessageID = result.Value.TeamID, result.Value.ChannelID, result.Value.MessageID
	record.RootMessageURL, record.PosterUserID = result.Value.WebURL, result.Value.Sender.UserID
	record.IsRootConfirmedByReadBack = result.Value.IsConfirmedByReadBack
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[StatusReplyRequest](postStatusReplyStepType), StatusReplyRequest{
		TeamID: record.TeamID, ChannelID: record.ChannelID, RootMessageID: record.RootMessageID, Content: BuildStatusReplyText(record.Update),
	}), nil
}

// dex:group group-id:posting group-label:"Posting"
// dex:explanation text:"Complete as rejected; Teams refused the post and nothing was posted."
type recordRejectedPost struct {
	dex.StepDefaultsNoWaitFor[teams.PostChannelMessageResult]
}

func (recordRejectedPost) GetStepType() string { return recordRejectedPostStepType }

func (recordRejectedPost) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (recordRejectedPost) Execute(ctx dex.Context, result teams.PostChannelMessageResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.TeamID, record.ChannelID = result.Value.TeamID, result.Value.ChannelID
	record.Phase, record.FailureKind = PhaseRejected, failureKindOf(result.Failure)
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:posting group-label:"Posting"
// dex:explanation text:"Complete without re-sending; the post's outcome is unknown and must be checked in Teams."
type recordUncertainPost struct {
	dex.StepDefaultsNoWaitFor[teams.PostChannelMessageResult]
}

func (recordUncertainPost) GetStepType() string { return recordUncertainPostStepType }

func (recordUncertainPost) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (recordUncertainPost) Execute(ctx dex.Context, result teams.PostChannelMessageResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.TeamID, record.ChannelID = result.Value.TeamID, result.Value.ChannelID
	record.Phase, record.FailureKind = PhasePostOutcomeUnknown, failureKindOf(result.Failure)
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:posting group-label:"Posting"
// dex:explanation text:"Record the status reply's outcome, then wait for an acknowledgement in the thread."
type recordStatusReply struct {
	dex.StepDefaultsNoWaitFor[teams.PostThreadReplyResult]
}

func (recordStatusReply) GetStepType() string { return recordStatusReplyStepType }

func (recordStatusReply) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (recordStatusReply) Execute(ctx dex.Context, result teams.PostThreadReplyResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	switch result.Branch {
	case teams.PostThreadReplyBranchSent:
		record.StatusReplyID = result.Value.MessageID
	case teams.PostThreadReplyBranchUncertain:
		record.IsStatusReplyOutcomeUnknown = true
	default:
		record.IsStatusReplyRejected = true
		record.FailureKind = failureKindOf(result.Failure)
	}
	record.Phase = PhaseAwaitingAcknowledgement
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(waitForAcknowledgement{}, threadCheckOf(record)), nil
}

// dex:group group-id:acknowledgement group-label:"Acknowledgement"
// dex:explanation text:"Wait on a durable Timer before reading the thread's replies again."
type waitForAcknowledgement struct {
	dex.StepDefaults
	checkInterval time.Duration
}

func (waitForAcknowledgement) GetStepType() string { return waitForAcknowledgementStepType }

func (step waitForAcknowledgement) WaitFor(dex.Context, ThreadCheck) (*dex.Wait, error) {
	return dex.Until(dex.Timer(step.checkInterval)), nil
}

func (waitForAcknowledgement) Execute(_ dex.Context, check ThreadCheck) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ThreadCheck](readThreadRepliesStepType), check), nil
}

// dex:group group-id:acknowledgement group-label:"Acknowledgement"
// dex:explanation text:"Finish on an acknowledgement, read again, or escalate after the last read."
type checkAcknowledgement struct {
	dex.StepDefaultsNoWaitFor[teams.ListThreadRepliesResult]
	maximumChecks    int
	escalationChatID string
}

func (checkAcknowledgement) GetStepType() string { return checkAcknowledgementStepType }

func (checkAcknowledgement) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (step checkAcknowledgement) Execute(ctx dex.Context, result teams.ListThreadRepliesResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.ReplyChecks++
	acknowledgement, isAcknowledged := FindAcknowledgement(result.Value.Replies, record)
	switch {
	case isAcknowledged:
		record.Acknowledgement, record.Phase = &acknowledgement, PhaseAcknowledged
	case record.ReplyChecks < step.maximumChecks:
	case step.escalationChatID == "":
		record.Phase = PhaseUnacknowledged
	default:
		record.Phase, record.EscalationChatID = PhaseEscalating, step.escalationChatID
	}
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	switch record.Phase {
	case PhaseAwaitingAcknowledgement:
		return dex.GoTo(waitForAcknowledgement{}, threadCheckOf(record)), nil
	case PhaseEscalating:
		return dex.GoTo(sdkgo.StepRef[EscalationRequest](escalateToChatStepType), EscalationRequest{
			ChatID:  record.EscalationChatID,
			Content: "Nobody has acknowledged " + record.Update.Severity + " incident " + record.Update.IncidentID + ": " + record.Update.Title + ". " + record.RootMessageURL,
		}), nil
	default:
		return dex.GracefulComplete(record), nil
	}
}

// dex:group group-id:acknowledgement group-label:"Acknowledgement"
// dex:explanation text:"Complete as repliesUnreadable, for example without administrator consent for reading replies."
type recordUnreadableReplies struct {
	dex.StepDefaultsNoWaitFor[teams.ListThreadRepliesResult]
}

func (recordUnreadableReplies) GetStepType() string { return recordUnreadableRepliesStepType }

func (recordUnreadableReplies) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (recordUnreadableReplies) Execute(ctx dex.Context, result teams.ListThreadRepliesResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.FailureKind = PhaseRepliesUnreadable, failureKindOf(result.Failure)
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:escalation group-label:"Escalation"
// dex:explanation text:"Record the escalation chat message's outcome and complete as escalated."
type recordEscalation struct {
	dex.StepDefaultsNoWaitFor[teams.PostChatMessageResult]
}

func (recordEscalation) GetStepType() string { return recordEscalationStepType }

func (recordEscalation) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: incidentLocks()}
}

func (recordEscalation) Execute(ctx dex.Context, result teams.PostChatMessageResult) (*dex.StepDecision, error) {
	record, err := incidentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	switch result.Branch {
	case teams.PostChatMessageBranchSent:
		record.EscalationMessageID = result.Value.MessageID
	case teams.PostChatMessageBranchUncertain:
		record.IsEscalationOutcomeUnknown = true
	default:
		record.FailureKind = failureKindOf(result.Failure)
	}
	record.Phase = PhaseEscalated
	if err := incidentPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := incidentAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

func threadCheckOf(record IncidentAcknowledgement) ThreadCheck {
	return ThreadCheck{TeamID: record.TeamID, ChannelID: record.ChannelID, RootMessageID: record.RootMessageID}
}

func incidentLocks() []dex.AttributeLock {
	return []dex.AttributeLock{dex.LockAttribute(incidentPhaseAttribute), dex.LockAttribute(incidentAttribute)}
}

func failureKindOf(failure *sdkgo.Failure) sdkgo.FailureKind {
	if failure == nil {
		return ""
	}
	return failure.Kind
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

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, IncidentAcknowledgement] = (*Flow)(nil).GetIncidentAcknowledgement
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
