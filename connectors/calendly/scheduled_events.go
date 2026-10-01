// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// ScheduledEventStatusActive is a scheduled event or invitee that is still booked.
	ScheduledEventStatusActive = "active"
	// ScheduledEventStatusCanceled is a scheduled event or invitee that was canceled.
	ScheduledEventStatusCanceled = "canceled"

	// ScheduledEventSortStartTimeAscending lists the earliest start time first.
	ScheduledEventSortStartTimeAscending = "start_time:asc"
	// ScheduledEventSortStartTimeDescending lists the latest start time first.
	ScheduledEventSortStartTimeDescending = "start_time:desc"

	// maximumCancellationReasonRunes is the documented maximum length of a cancellation reason.
	maximumCancellationReasonRunes = 10000
)

// ScheduledEvent is the connector-safe subset of a Calendly scheduled event. Every time is UTC.
// Conference passwords, internal meeting notes, and guest emails are not copied.
type ScheduledEvent struct {
	// URI is the event's stable Calendly URI, such as https://api.calendly.com/scheduled_events/<id>.
	URI string `json:"uri"`
	// Name is the event name, usually the event type's name.
	Name string `json:"name,omitempty"`
	// Status is active or canceled.
	Status string `json:"status"`
	// StartTime is when the event starts.
	StartTime time.Time `json:"startTime"`
	// EndTime is when the event ends.
	EndTime time.Time `json:"endTime"`
	// EventTypeURI is the URI of the event type the invitee booked.
	EventTypeURI string `json:"eventTypeUri,omitempty"`
	// Location describes where the event takes place.
	Location ScheduledEventLocation `json:"location"`
	// InviteesCounter counts the event's invitees.
	InviteesCounter ScheduledEventInviteesCounter `json:"inviteesCounter"`
	// Hosts lists the Calendly users who host the event.
	Hosts []ScheduledEventHost `json:"hosts,omitempty"`
	// Cancellation describes the cancellation of a canceled event.
	Cancellation *Cancellation `json:"cancellation,omitempty"`
	// CreatedAt is when the event was booked.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// UpdatedAt is when the event last changed.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// ScheduledEventLocation is the location kind and its public details. Conference data such as passwords
// is never copied.
type ScheduledEventLocation struct {
	// Type is Calendly's location kind, such as physical, zoom, google_conference, or custom.
	Type string `json:"type,omitempty"`
	// Location is the address, phone number, or description for a location kind that has one.
	Location string `json:"location,omitempty"`
	// JoinURL is the conference link once Calendly has created it.
	JoinURL string `json:"joinUrl,omitempty"`
	// Status is the conference provisioning status, such as pushed, for conference locations.
	Status string `json:"status,omitempty"`
}

// ScheduledEventInviteesCounter counts an event's invitees.
type ScheduledEventInviteesCounter struct {
	// Total counts every invitee, including canceled ones.
	Total int `json:"total"`
	// Active counts the invitees who have not canceled.
	Active int `json:"active"`
	// Limit is the maximum number of active invitees the event accepts.
	Limit int `json:"limit"`
}

// ScheduledEventHost is one Calendly user who hosts an event.
type ScheduledEventHost struct {
	// UserURI is the host's Calendly user URI.
	UserURI string `json:"userUri"`
	// Email is the host's email address.
	Email string `json:"email,omitempty"`
	// Name is the host's display name.
	Name string `json:"name,omitempty"`
}

// Cancellation describes who canceled an event or an invitee, and why.
type Cancellation struct {
	// CanceledBy is the name of the person who canceled.
	CanceledBy string `json:"canceledBy,omitempty"`
	// Reason is the cancellation reason, when one was given.
	Reason string `json:"reason,omitempty"`
	// CancelerType is host or invitee.
	CancelerType string `json:"cancelerType,omitempty"`
	// CreatedAt is when the cancellation happened.
	CreatedAt time.Time `json:"createdAt,omitzero"`
}

// ListScheduledEventsInput selects one page of scheduled events.
type ListScheduledEventsInput struct {
	// UserURI limits the list to one user's events, such as https://api.calendly.com/users/<id>. With
	// OrganizationURI it selects one member of an organization the token administers.
	UserURI string `json:"userUri,omitempty"`
	// OrganizationURI lists every event of an organization, which needs an owner or admin token. When both
	// URIs are blank the connector lists the connected user's own events.
	OrganizationURI string `json:"organizationUri,omitempty"`
	// MinStartTime is the required inclusive lower bound of the start-time window.
	MinStartTime time.Time `json:"minStartTime"`
	// MaxStartTime is the required upper bound of the start-time window; it must be after MinStartTime.
	MaxStartTime time.Time `json:"maxStartTime"`
	// Status is active or canceled; blank lists both.
	Status string `json:"status,omitempty"`
	// InviteeEmail limits the list to events with this invitee; blank lists every invitee's events.
	InviteeEmail string `json:"inviteeEmail,omitempty"`
	// Sort is start_time:asc or start_time:desc; blank uses start_time:asc so pages are stable.
	Sort string `json:"sort,omitempty"`
	// PageSize is 1 through 100 events per page; zero uses Calendly's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page; blank reads the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// ScheduledEventPage is one page of scheduled events and the scope that was listed.
type ScheduledEventPage struct {
	// Events are the page's scheduled events in the requested order.
	Events []ScheduledEvent `json:"events"`
	// NextPageToken reads the next page; blank means this is the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// UserURI is the user whose events were listed, including the connected user when the input named none.
	UserURI string `json:"userUri,omitempty"`
	// OrganizationURI is the organization whose events were listed, when one was named.
	OrganizationURI string `json:"organizationUri,omitempty"`
	// MinStartTime echoes the window's lower bound, so a reader can tell which window was checked.
	MinStartTime time.Time `json:"minStartTime"`
	// MaxStartTime echoes the window's upper bound.
	MaxStartTime time.Time `json:"maxStartTime"`
}

// GetScheduledEventInput identifies one scheduled event.
type GetScheduledEventInput struct {
	// ScheduledEventURI is the event's Calendly URI, such as the scheduledEvent.uri of an InviteeEvent.
	ScheduledEventURI string `json:"scheduledEventUri"`
}

// CancelScheduledEventInput identifies the scheduled event to cancel.
type CancelScheduledEventInput struct {
	// ScheduledEventURI is the event's Calendly URI.
	ScheduledEventURI string `json:"scheduledEventUri"`
	// Reason is shown to the invitees in Calendly's cancellation notice; blank sends none. At most 10,000
	// characters.
	Reason string `json:"reason,omitempty"`
}

// ScheduledEventCancellation reports a canceled scheduled event.
type ScheduledEventCancellation struct {
	// ScheduledEventURI is the canceled event's URI.
	ScheduledEventURI string `json:"scheduledEventUri"`
	// AlreadyCanceled is true when the event was canceled before this call, including by a duplicate
	// dispatch of the same Step.
	AlreadyCanceled bool `json:"alreadyCanceled"`
	// Cancellation describes the cancellation, when Calendly returned it.
	Cancellation Cancellation `json:"cancellation"`
}

// ListScheduledEventsOperation implements the listScheduledEvents Query. Build it with Client.ListScheduledEvents.
type ListScheduledEventsOperation struct{ client *Client }

// GetScheduledEventOperation implements the getScheduledEvent Query. Build it with Client.GetScheduledEvent.
type GetScheduledEventOperation struct{ client *Client }

// CancelScheduledEventOperation implements the cancelScheduledEvent Mutation. Build it with
// Client.CancelScheduledEvent.
type CancelScheduledEventOperation struct{ client *Client }

type calendlyScheduledEvent struct {
	URI              string                    `json:"uri"`
	Name             *string                   `json:"name"`
	Status           string                    `json:"status"`
	StartTime        string                    `json:"start_time"`
	EndTime          string                    `json:"end_time"`
	EventType        *string                   `json:"event_type"`
	Location         *calendlyLocation         `json:"location"`
	InviteesCounter  *calendlyInviteesCounter  `json:"invitees_counter"`
	EventMemberships []calendlyEventMembership `json:"event_memberships"`
	Cancellation     *calendlyCancellation     `json:"cancellation"`
	CreatedAt        *string                   `json:"created_at"`
	UpdatedAt        *string                   `json:"updated_at"`
}

type calendlyLocation struct {
	Type     string          `json:"type"`
	Location json.RawMessage `json:"location"`
	JoinURL  *string         `json:"join_url"`
	Status   *string         `json:"status"`
}

type calendlyInviteesCounter struct {
	Total  float64 `json:"total"`
	Active float64 `json:"active"`
	Limit  float64 `json:"limit"`
}

type calendlyEventMembership struct {
	User      string `json:"user"`
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name"`
}

type calendlyCancellation struct {
	CanceledBy   string  `json:"canceled_by"`
	Reason       *string `json:"reason"`
	CancelerType string  `json:"canceler_type"`
	CreatedAt    *string `json:"created_at"`
}

// Definition returns the immutable listScheduledEvents operation definition.
func (ListScheduledEventsOperation) Definition() sdkgo.QueryDefinition {
	return ListScheduledEventsDefinition
}

// Invoke reads one page of GET /scheduled_events. Both window bounds are required; with no user or
// organization URI the connector first reads GET /users/me and lists the connected user's events.
func (operation ListScheduledEventsOperation) Invoke(call sdkgo.Call, input ListScheduledEventsInput) sdkgo.QueryAttempt[ScheduledEventPage] {
	operationID := ListScheduledEventsDefinition.Operation.OperationID
	client := operation.client
	if err := input.validate(); err != nil {
		return sdkgo.NewQueryBranch(ListScheduledEventsBranchDefect, ScheduledEventPage{},
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[ScheduledEventPage](operationID, ListScheduledEventsBranchDefect, err)
	}
	branches := queryFailureBranches{
		providerRejected: ListScheduledEventsBranchProviderRejected, invalidResponse: ListScheduledEventsBranchInvalidResponse,
	}
	userURI, organizationURI := input.UserURI, input.OrganizationURI
	if userURI == "" && organizationURI == "" {
		currentUser, response, err := client.resolveCurrentUser(call, &credentials)
		if attempt, isTerminal := classifyQueryExchange[ScheduledEventPage](client, operationID, response, err, branches, ""); isTerminal {
			return attempt
		}
		userURI = currentUser.userURI
	}
	response, err := client.sendCalendlyRequest(call, &credentials, http.MethodGet, "/scheduled_events",
		input.query(userURI, organizationURI), nil, nil)
	if attempt, isTerminal := classifyQueryExchange[ScheduledEventPage](client, operationID, response, err, branches, ""); isTerminal {
		return attempt
	}
	var decoded struct {
		Collection []calendlyScheduledEvent `json:"collection"`
		Pagination calendlyPagination       `json:"pagination"`
	}
	page := ScheduledEventPage{
		UserURI: userURI, OrganizationURI: organizationURI,
		MinStartTime: input.MinStartTime.UTC(), MaxStartTime: input.MaxStartTime.UTC(),
	}
	err = decodeCalendlyJSON(response.body, &decoded)
	if err == nil {
		page.NextPageToken, err = decoded.Pagination.nextPageToken()
	}
	if err == nil {
		page.Events, err = convertScheduledEvents(decoded.Collection)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(ListScheduledEventsBranchInvalidResponse, ScheduledEventPage{},
			calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt(""))
	}
	return sdkgo.NewQueryBranch(ListScheduledEventsBranchListed, page, nil, client.calendlyReceipt(""))
}

// Definition returns the immutable getScheduledEvent operation definition.
func (GetScheduledEventOperation) Definition() sdkgo.QueryDefinition {
	return GetScheduledEventDefinition
}

// Invoke reads GET /scheduled_events/{id}. A 404 selects notFound.
func (operation GetScheduledEventOperation) Invoke(call sdkgo.Call, input GetScheduledEventInput) sdkgo.QueryAttempt[ScheduledEvent] {
	operationID := GetScheduledEventDefinition.Operation.OperationID
	client := operation.client
	eventID, err := parseScheduledEventURI(input.ScheduledEventURI)
	if err != nil {
		return sdkgo.NewQueryBranch(GetScheduledEventBranchDefect, ScheduledEvent{},
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[ScheduledEvent](operationID, GetScheduledEventBranchDefect, err)
	}
	response, err := client.sendCalendlyRequest(call, &credentials, http.MethodGet, "/scheduled_events/"+eventID, nil, nil, nil)
	branches := queryFailureBranches{
		notFound: GetScheduledEventBranchNotFound, providerRejected: GetScheduledEventBranchProviderRejected,
		invalidResponse: GetScheduledEventBranchInvalidResponse,
	}
	if attempt, isTerminal := classifyQueryExchange[ScheduledEvent](client, operationID, response, err, branches, input.ScheduledEventURI); isTerminal {
		return attempt
	}
	event, err := decodeScheduledEventResource(response.body, input.ScheduledEventURI)
	if err != nil {
		return sdkgo.NewQueryBranch(GetScheduledEventBranchInvalidResponse, ScheduledEvent{},
			calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt(input.ScheduledEventURI))
	}
	return sdkgo.NewQueryBranch(GetScheduledEventBranchFound, event, nil, client.calendlyReceipt(event.URI))
}

// Definition returns the immutable cancelScheduledEvent operation definition.
func (CancelScheduledEventOperation) Definition() sdkgo.MutationDefinition {
	return CancelScheduledEventDefinition
}

// IdempotencyKey returns the call ID. Calendly has no idempotency header; the operation is safe to repeat
// because canceling an already-canceled event selects canceled.
func (CancelScheduledEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelScheduledEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke posts POST /scheduled_events/{id}/cancellation. Calendly answers 403 for an event that is already
// canceled, so a 403 reads the event back: a canceled event selects canceled with AlreadyCanceled, and an
// active one selects providerRejected. A lost answer or a 5xx returns Retry, because repeating the request
// reaches the same read-back.
func (operation CancelScheduledEventOperation) Invoke(call sdkgo.Call, input CancelScheduledEventInput) sdkgo.MutationAttempt[ScheduledEventCancellation] {
	operationID := CancelScheduledEventDefinition.Operation.OperationID
	client := operation.client
	output := ScheduledEventCancellation{ScheduledEventURI: input.ScheduledEventURI}
	eventID, err := parseScheduledEventURI(input.ScheduledEventURI)
	if err == nil && len([]rune(input.Reason)) > maximumCancellationReasonRunes {
		err = fmt.Errorf("cancellation reason cannot exceed %d characters", maximumCancellationReasonRunes)
	}
	if err != nil {
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialMutationAttempt(operationID, CancelScheduledEventBranchDefect, output, err)
	}
	body := map[string]string{}
	if input.Reason != "" {
		body["reason"] = input.Reason
	}
	response, err := client.sendCalendlyRequest(call, &credentials, http.MethodPost, "/scheduled_events/"+eventID+"/cancellation", nil, body, nil)
	if err != nil {
		return cancellationAttemptForExchangeError(client, operationID, output, err)
	}
	switch {
	case response.statusCode == http.StatusCreated || response.statusCode == http.StatusOK:
		var decoded struct {
			Resource calendlyCancellation `json:"resource"`
		}
		var cancellation Cancellation
		err := decodeCalendlyJSON(response.body, &decoded)
		if err == nil {
			cancellation, err = decoded.Resource.convert()
		}
		if err != nil {
			return sdkgo.NewMutationBranch(CancelScheduledEventBranchInvalidResponse, output,
				calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt(output.ScheduledEventURI))
		}
		output.Cancellation = cancellation
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchCanceled, output, nil, client.calendlyReceipt(output.ScheduledEventURI))
	case response.statusCode == http.StatusForbidden:
		return operation.readBackRefusedCancellation(call, &credentials, eventID, output, response)
	case response.statusCode == http.StatusNotFound:
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchNotFound, output,
			calendlyFailurePointer(operationID, sdkgo.FailureNotFound, "the Calendly scheduled event was not found"), client.calendlyReceipt(output.ScheduledEventURI))
	}
	outcome := client.classifyCalendlyFailure(operationID, response)
	if outcome.isRetry {
		return sdkgo.NewMutationRetry[ScheduledEventCancellation](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewMutationBranch(CancelScheduledEventBranchProviderRejected, output, &outcome.failure, client.calendlyReceipt(output.ScheduledEventURI))
}

// readBackRefusedCancellation reads the event after a 403, so an already-canceled event counts as canceled
// whatever message Calendly sent.
func (operation CancelScheduledEventOperation) readBackRefusedCancellation(
	call sdkgo.Call, credentials *Credentials, eventID string, output ScheduledEventCancellation, refusal calendlyResponse,
) sdkgo.MutationAttempt[ScheduledEventCancellation] {
	operationID := CancelScheduledEventDefinition.Operation.OperationID
	client := operation.client
	response, err := client.sendCalendlyRequest(call, credentials, http.MethodGet, "/scheduled_events/"+eventID, nil, nil, nil)
	if err != nil {
		return cancellationAttemptForExchangeError(client, operationID, output, err)
	}
	if response.statusCode == http.StatusNotFound {
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchNotFound, output,
			calendlyFailurePointer(operationID, sdkgo.FailureNotFound, "the Calendly scheduled event was not found"), client.calendlyReceipt(output.ScheduledEventURI))
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		outcome := client.classifyCalendlyFailure(operationID, response)
		if outcome.isRetry {
			return sdkgo.NewMutationRetry[ScheduledEventCancellation](outcome.failure, outcome.retryAfter)
		}
		refused := client.classifyCalendlyFailure(operationID, refusal)
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchProviderRejected, output, &refused.failure, client.calendlyReceipt(output.ScheduledEventURI))
	}
	event, err := decodeScheduledEventResource(response.body, output.ScheduledEventURI)
	if err != nil {
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchInvalidResponse, output,
			calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt(output.ScheduledEventURI))
	}
	if event.Status == ScheduledEventStatusCanceled {
		output.AlreadyCanceled = true
		if event.Cancellation != nil {
			output.Cancellation = *event.Cancellation
		}
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchCanceled, output, nil, client.calendlyReceipt(output.ScheduledEventURI))
	}
	failure := calendlyFailure(operationID, sdkgo.FailureAuthorization,
		"Calendly refused to cancel the active event; it may have already started, or the token may not cancel it")
	if isCalendlyInsufficientScope(refusal.body) {
		failure.Message = "the Calendly token lacks the scheduled_events:write scope needed to cancel events"
	} else if !event.StartTime.After(client.now()) {
		failure.Kind, failure.Message = sdkgo.FailureConflict, "Calendly cannot cancel an event that has already started"
	}
	return sdkgo.NewMutationBranch(CancelScheduledEventBranchProviderRejected, output, &failure, client.calendlyReceipt(output.ScheduledEventURI))
}

// cancellationAttemptForExchangeError retries a lost exchange: a repeated cancellation is harmless.
func cancellationAttemptForExchangeError(
	client *Client, operationID string, output ScheduledEventCancellation, err error,
) sdkgo.MutationAttempt[ScheduledEventCancellation] {
	if errors.Is(err, errCalendlyRequestInvalid) {
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{})
	}
	if errors.Is(err, errCalendlyResponseTooLarge) {
		return sdkgo.NewMutationBranch(CancelScheduledEventBranchInvalidResponse, output,
			calendlyFailurePointer(operationID, sdkgo.FailureResponseTooLarge, errCalendlyResponseTooLarge.Error()), client.calendlyReceipt(output.ScheduledEventURI))
	}
	return sdkgo.NewMutationRetry[ScheduledEventCancellation](calendlyFailure(operationID, sdkgo.FailureTransport,
		"Calendly's answer to the cancellation was lost; the retry reads the event back"), 0)
}

func (input ListScheduledEventsInput) validate() error {
	if input.UserURI != "" {
		if _, err := parseUserURI(input.UserURI); err != nil {
			return err
		}
	}
	if input.OrganizationURI != "" {
		if _, err := parseOrganizationURI(input.OrganizationURI); err != nil {
			return err
		}
	}
	if input.MinStartTime.IsZero() || input.MaxStartTime.IsZero() {
		return fmt.Errorf("listScheduledEvents requires both minStartTime and maxStartTime")
	}
	if !input.MaxStartTime.After(input.MinStartTime) {
		return fmt.Errorf("maxStartTime must be after minStartTime")
	}
	switch input.Status {
	case "", ScheduledEventStatusActive, ScheduledEventStatusCanceled:
	default:
		return fmt.Errorf("status must be active, canceled, or blank")
	}
	switch input.Sort {
	case "", ScheduledEventSortStartTimeAscending, ScheduledEventSortStartTimeDescending:
	default:
		return fmt.Errorf("sort must be start_time:asc, start_time:desc, or blank")
	}
	if input.InviteeEmail != "" {
		address, err := mail.ParseAddress(input.InviteeEmail)
		if err != nil || address.Address != input.InviteeEmail {
			return fmt.Errorf("inviteeEmail must be one plain email address")
		}
	}
	return validatePageRequest(input.PageSize, input.PageToken)
}

func (input ListScheduledEventsInput) query(userURI string, organizationURI string) url.Values {
	query := url.Values{
		"min_start_time": {formatCalendlyTimestamp(input.MinStartTime)},
		"max_start_time": {formatCalendlyTimestamp(input.MaxStartTime)},
		"sort":           {ScheduledEventSortStartTimeAscending},
	}
	if userURI != "" {
		query.Set("user", userURI)
	}
	if organizationURI != "" {
		query.Set("organization", organizationURI)
	}
	if input.Sort != "" {
		query.Set("sort", input.Sort)
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	if input.InviteeEmail != "" {
		query.Set("invitee_email", input.InviteeEmail)
	}
	if input.PageSize > 0 {
		query.Set("count", strconv.Itoa(input.PageSize))
	}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	return query
}

// decodeScheduledEventResource decodes {"resource": event} and requires the URI that was requested.
func decodeScheduledEventResource(body []byte, expectedURI string) (ScheduledEvent, error) {
	var decoded struct {
		Resource calendlyScheduledEvent `json:"resource"`
	}
	if err := decodeCalendlyJSON(body, &decoded); err != nil {
		return ScheduledEvent{}, err
	}
	event, err := decoded.Resource.convert()
	if err != nil || event.URI != expectedURI {
		return ScheduledEvent{}, errCalendlyResponseMalformed
	}
	return event, nil
}

func convertScheduledEvents(collection []calendlyScheduledEvent) ([]ScheduledEvent, error) {
	events := make([]ScheduledEvent, 0, len(collection))
	for _, item := range collection {
		event, err := item.convert()
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (event calendlyScheduledEvent) convert() (ScheduledEvent, error) {
	if _, err := parseScheduledEventURI(event.URI); err != nil {
		return ScheduledEvent{}, errCalendlyResponseMalformed
	}
	if event.Status != ScheduledEventStatusActive && event.Status != ScheduledEventStatusCanceled {
		return ScheduledEvent{}, errCalendlyResponseMalformed
	}
	converted := ScheduledEvent{URI: event.URI, Name: stringValue(event.Name), Status: event.Status, EventTypeURI: stringValue(event.EventType)}
	var err error
	if converted.StartTime, err = parseCalendlyTimestamp(event.StartTime); err != nil {
		return ScheduledEvent{}, err
	}
	if converted.EndTime, err = parseCalendlyTimestamp(event.EndTime); err != nil {
		return ScheduledEvent{}, err
	}
	if converted.CreatedAt, err = parseOptionalCalendlyTimestamp(event.CreatedAt); err != nil {
		return ScheduledEvent{}, err
	}
	if converted.UpdatedAt, err = parseOptionalCalendlyTimestamp(event.UpdatedAt); err != nil {
		return ScheduledEvent{}, err
	}
	if event.Location != nil {
		converted.Location = event.Location.convert()
	}
	if event.InviteesCounter != nil {
		converted.InviteesCounter = ScheduledEventInviteesCounter{
			Total: int(event.InviteesCounter.Total), Active: int(event.InviteesCounter.Active), Limit: int(event.InviteesCounter.Limit),
		}
	}
	for _, membership := range event.EventMemberships {
		converted.Hosts = append(converted.Hosts, ScheduledEventHost{UserURI: membership.User, Email: membership.UserEmail, Name: membership.UserName})
	}
	if event.Cancellation != nil {
		cancellation, err := event.Cancellation.convert()
		if err != nil {
			return ScheduledEvent{}, err
		}
		converted.Cancellation = &cancellation
	}
	return converted, nil
}

// convert keeps the location only when it is a string; some location kinds carry no text.
func (location calendlyLocation) convert() ScheduledEventLocation {
	converted := ScheduledEventLocation{Type: location.Type, JoinURL: stringValue(location.JoinURL), Status: stringValue(location.Status)}
	var text string
	if json.Unmarshal(location.Location, &text) == nil {
		converted.Location = strings.TrimSpace(text)
	}
	return converted
}

func (cancellation calendlyCancellation) convert() (Cancellation, error) {
	createdAt, err := parseOptionalCalendlyTimestamp(cancellation.CreatedAt)
	if err != nil {
		return Cancellation{}, err
	}
	return Cancellation{
		CanceledBy: cancellation.CanceledBy, Reason: stringValue(cancellation.Reason),
		CancelerType: cancellation.CancelerType, CreatedAt: createdAt,
	}, nil
}
