// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// linearSignatureHeader carries the hex HMAC-SHA256 of the raw body under the webhook's signing secret.
	linearSignatureHeader = "Linear-Signature"
	issueWebhookType      = "Issue"
	// maxUpdatedFields bounds the changed property names one event carries.
	maxUpdatedFields = 50

	// IssueEventActionCreate is the action of a newly created issue.
	IssueEventActionCreate = "create"
	// IssueEventActionUpdate is the action of a changed issue, including an archived or trashed one.
	IssueEventActionUpdate = "update"
	// IssueEventActionRemove is the action of a deleted issue.
	IssueEventActionRemove = "remove"
)

var (
	errLinearSignatureInvalid = errors.New("Linear-Signature is missing or does not match, or webhookTimestamp is stale")
	errLinearWebhookMalformed = errors.New("Linear webhook body is malformed")
	webhookTokenPattern       = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	supportedIssueActions     = []string{IssueEventActionCreate, IssueEventActionUpdate, IssueEventActionRemove}
)

// IssueEventReceivedTriggerConfiguration filters the Issue webhook events one binding records. The zero value
// records every Issue event of every team. It reduces what a binding stores; the application's
// TriggerFilter remains its admission rule.
type IssueEventReceivedTriggerConfiguration struct {
	// Actions lists create, update, and remove; empty accepts every action.
	Actions []string `json:"actions,omitempty"`
	// TeamID accepts only issues of this team UUID, such as the teamPicker unit's teamId; blank accepts every team.
	TeamID string `json:"teamId,omitempty"`
}

// IssueEventActor is who caused an Issue event: a user, an OAuth application, or an integration.
type IssueEventActor struct {
	// ID is the actor's UUID.
	ID string `json:"id"`
	// Name is the actor's display name.
	Name string `json:"name,omitempty"`
	// Type is Linear's actor type, such as user, OauthClient, or integration, passed through unchanged.
	Type string `json:"type,omitempty"`
}

// IssueEventIssue is the issue as the webhook described it, without its description.
type IssueEventIssue struct {
	IssueSummary
	// Number is the issue's number within its team.
	Number int `json:"number"`
	// ProjectID is the issue's project UUID, or blank.
	ProjectID string `json:"projectId,omitempty"`
	// CycleID is the issue's cycle UUID, or blank.
	CycleID string `json:"cycleId,omitempty"`
	// ParentID is the parent issue UUID of a sub-issue, or blank.
	ParentID string `json:"parentId,omitempty"`
	// IsTrashed reports that the issue is in the trash.
	IsTrashed bool `json:"trashed,omitempty"`
}

// IssueEvent is one verified Linear Issue webhook. The Trigger event ID is the action, the issue UUID, and
// the issue's updatedAt in Unix milliseconds, such as update:2f6b7c1e-...:1790000000000, so a redelivery of
// one change keeps its ID even though Linear signs each delivery with a new webhookTimestamp.
type IssueEvent struct {
	// Action is create, update, or remove; another Linear action is passed through unchanged.
	Action string `json:"action"`
	// OccurredAt is when Linear created the event payload.
	OccurredAt time.Time `json:"occurredAt"`
	// WebhookID is the Linear webhook that sent the event.
	WebhookID string `json:"webhookId"`
	// OrganizationID is the Linear workspace's UUID.
	OrganizationID string `json:"organizationId"`
	// Actor is who caused the event, or nil when Linear names none.
	Actor *IssueEventActor `json:"actor,omitempty"`
	// Issue is the issue after the event.
	Issue IssueEventIssue `json:"issue"`
	// UpdatedFields names the properties an update changed, sorted, at most 50; previous values are omitted.
	UpdatedFields []string `json:"updatedFields,omitempty"`
	// PreviousStateID is the workflow state an update moved the issue out of, or blank.
	PreviousStateID string `json:"previousStateId,omitempty"`
}

type linearWebhookEnvelope struct {
	Action         string                     `json:"action"`
	Type           string                     `json:"type"`
	CreatedAt      string                     `json:"createdAt"`
	Data           json.RawMessage            `json:"data"`
	UpdatedFrom    map[string]json.RawMessage `json:"updatedFrom"`
	WebhookID      string                     `json:"webhookId"`
	OrganizationID string                     `json:"organizationId"`
	Actor          *IssueEventActor           `json:"actor"`
}

type issueWebhookWire struct {
	issueWire
	TeamID     string  `json:"teamId"`
	StateID    string  `json:"stateId"`
	AssigneeID *string `json:"assigneeId"`
	ProjectID  *string `json:"projectId"`
	CycleID    *string `json:"cycleId"`
	ParentID   *string `json:"parentId"`
}

// Validate checks that Actions holds distinct supported actions and TeamID is a UUID.
func (configuration IssueEventReceivedTriggerConfiguration) Validate() error {
	seen := map[string]bool{}
	for _, action := range configuration.Actions {
		if !slices.Contains(supportedIssueActions, action) || seen[action] {
			return errors.New("issueEventReceived actions must be distinct values from create, update, and remove")
		}
		seen[action] = true
	}
	if configuration.TeamID != "" && !isLinearUUID(configuration.TeamID) {
		return errors.New("issueEventReceived teamId must be a Linear team UUID")
	}
	return nil
}

// acceptsEvent applies the binding's action and team filters.
func (configuration IssueEventReceivedTriggerConfiguration) acceptsEvent(event sdkgo.TriggerEvent[IssueEvent]) bool {
	if len(configuration.Actions) > 0 && !slices.Contains(configuration.Actions, event.Payload.Action) {
		return false
	}
	return configuration.TeamID == "" || strings.EqualFold(configuration.TeamID, event.Payload.Issue.Team.ID)
}

// IssueEventReceivedWebhookHandler returns the connection's webhook endpoint for an application to mount at
// the public HTTPS URL of its Linear webhook. Every issueEventReceived Trigger built from this Connection
// feeds from it, and it answers 503 while none of them runs, so Linear retries.
func (connection Connection) IssueEventReceivedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.issueEventReceivedWebhookEndpoint(connection.reference)
}

func (client *Client) issueEventReceivedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration IssueEventReceivedTriggerConfiguration,
) sdkgo.TriggerSource[IssueEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.issueEventReceivedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(configuration.acceptsEvent)
}

// issueEventReceivedWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) issueEventReceivedWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, IssueEvent], error) {
	client.issueEventEndpointsMu.Lock()
	defer client.issueEventEndpointsMu.Unlock()
	if endpoint, isFound := client.issueEventEndpoints[connection]; isFound {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, IssueEvent]{
		ConnectorID: ConnectorID, TriggerName: IssueEventReceivedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, CredentialRefresh: client.refreshDriver,
		MaxBodyBytes: client.webhookMaxBodyBytes, VerifyRequest: client.verifyIssueWebhookRequest,
		DecodeEvent: decodeIssueWebhookRequest, Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("Linear webhook endpoint: %w", err)
	}
	client.issueEventEndpoints[connection] = endpoint
	return endpoint, nil
}

// verifyIssueWebhookRequest answers 503 without a signing secret, so Linear keeps retrying the delivery.
func (client *Client) verifyIssueWebhookRequest(request webhooktrigger.Request, credentials Credentials) error {
	signingSecret := credentials.WebhookSigningSecret.Reveal()
	if signingSecret == "" {
		return webhooktrigger.ErrVerificationUnavailable
	}
	return verifyLinearWebhook(request.Body, request.Header.Get(linearSignatureHeader), signingSecret, request.ReceivedAt, client.webhookSignatureTolerance)
}

// verifyLinearWebhook checks the HMAC first, so the webhookTimestamp it then reads is authentic.
func verifyLinearWebhook(body []byte, signatureHeader string, signingSecret string, now time.Time, tolerance time.Duration) error {
	signature, err := hex.DecodeString(strings.TrimSpace(signatureHeader))
	if err != nil || len(signature) != sha256.Size {
		return errLinearSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write(body) // A hash.Hash write never fails.
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errLinearSignatureInvalid
	}
	var envelope struct {
		WebhookTimestamp *float64 `json:"webhookTimestamp"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.WebhookTimestamp == nil ||
		*envelope.WebhookTimestamp <= 0 || *envelope.WebhookTimestamp > math.MaxInt64/2 {
		return errLinearSignatureInvalid
	}
	sentAt := time.UnixMilli(int64(*envelope.WebhookTimestamp))
	if age := now.Sub(sentAt); age > tolerance || age < -tolerance {
		return errLinearSignatureInvalid
	}
	return nil
}

// decodeIssueWebhookRequest decodes a verified delivery. Another entity type, such as Comment or Project,
// is acknowledged without a record.
func decodeIssueWebhookRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[IssueEvent], bool, error) {
	var envelope linearWebhookEnvelope
	if err := json.Unmarshal(request.Body, &envelope); err != nil || envelope.Type == "" || !webhookTokenPattern.MatchString(envelope.Action) {
		return sdkgo.TriggerEvent[IssueEvent]{}, false, errLinearWebhookMalformed
	}
	if envelope.Type != issueWebhookType {
		return sdkgo.TriggerEvent[IssueEvent]{}, false, nil
	}
	occurredAt, err := parseLinearTime(envelope.CreatedAt)
	if err != nil || isJSONNull(envelope.Data) {
		return sdkgo.TriggerEvent[IssueEvent]{}, false, errLinearWebhookMalformed
	}
	issue, err := decodeWebhookIssue(envelope.Data)
	if err != nil {
		return sdkgo.TriggerEvent[IssueEvent]{}, false, err
	}
	event := IssueEvent{
		Action: envelope.Action, OccurredAt: occurredAt, WebhookID: envelope.WebhookID, OrganizationID: envelope.OrganizationID,
		Actor: envelope.Actor, Issue: issue,
	}
	for field := range envelope.UpdatedFrom {
		if webhookTokenPattern.MatchString(field) && len(event.UpdatedFields) < maxUpdatedFields {
			event.UpdatedFields = append(event.UpdatedFields, field)
		}
	}
	slices.Sort(event.UpdatedFields)
	var previousStateID string
	if raw, hasState := envelope.UpdatedFrom["stateId"]; hasState && json.Unmarshal(raw, &previousStateID) == nil && isLinearUUID(previousStateID) {
		event.PreviousStateID = previousStateID
	}
	return sdkgo.TriggerEvent[IssueEvent]{
		ID:         envelope.Action + ":" + issue.ID + ":" + strconv.FormatInt(issue.UpdatedAt.UnixMilli(), 10),
		OccurredAt: occurredAt, Payload: event,
	}, true, nil
}

// decodeWebhookIssue reads the webhook's issue, whose IDs arrive both as fields and as nested objects.
func decodeWebhookIssue(raw json.RawMessage) (IssueEventIssue, error) {
	var wire issueWebhookWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return IssueEventIssue{}, errLinearWebhookMalformed
	}
	if wire.Team == nil && isLinearUUID(wire.TeamID) {
		wire.Team = &idNameWire{ID: wire.TeamID}
	}
	if wire.State == nil && isLinearUUID(wire.StateID) {
		wire.State = &idNameWire{ID: wire.StateID}
	}
	if wire.Assignee == nil && wire.AssigneeID != nil {
		wire.Assignee = &userReferenceWire{ID: *wire.AssigneeID}
	}
	summary, err := decodeIssueSummary(wire.issueWire)
	if err != nil || wire.Number < 0 || wire.Number > math.MaxInt32 {
		return IssueEventIssue{}, errLinearWebhookMalformed
	}
	issue := IssueEventIssue{IssueSummary: summary, Number: int(wire.Number), IsTrashed: wire.Trashed != nil && *wire.Trashed}
	for _, reference := range []struct {
		value  *string
		target *string
	}{{wire.ProjectID, &issue.ProjectID}, {wire.CycleID, &issue.CycleID}, {wire.ParentID, &issue.ParentID}} {
		if reference.value != nil {
			*reference.target = *reference.value
		}
	}
	return issue, nil
}
