// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Invitee is the connector-safe subset of a Calendly invitee: the person who booked a scheduled event.
// Every time is UTC. Payment, tracking, and SMS reminder numbers are not copied.
type Invitee struct {
	// URI is the invitee's stable Calendly URI, such as
	// https://api.calendly.com/scheduled_events/<event id>/invitees/<invitee id>.
	URI string `json:"uri"`
	// ScheduledEventURI is the URI of the scheduled event the invitee booked.
	ScheduledEventURI string `json:"scheduledEventUri"`
	// Email is the invitee's email address.
	Email string `json:"email"`
	// Name is the invitee's full name.
	Name string `json:"name,omitempty"`
	// FirstName is set when the event type asks for separate first and last names.
	FirstName string `json:"firstName,omitempty"`
	// LastName is set when the event type asks for separate first and last names.
	LastName string `json:"lastName,omitempty"`
	// Status is active or canceled.
	Status string `json:"status"`
	// Timezone is the IANA time zone the invitee chose, such as America/New_York.
	Timezone string `json:"timezone,omitempty"`
	// QuestionsAndAnswers are the invitee's answers to the booking form, in form order.
	QuestionsAndAnswers []InviteeAnswer `json:"questionsAndAnswers,omitempty"`
	// Rescheduled is true on a canceled invitee that rebooked; NewInviteeURI names the new booking.
	Rescheduled bool `json:"rescheduled"`
	// OldInviteeURI names the canceled booking this invitee replaced, when it is a reschedule.
	OldInviteeURI string `json:"oldInviteeUri,omitempty"`
	// NewInviteeURI names the booking that replaced this canceled invitee.
	NewInviteeURI string `json:"newInviteeUri,omitempty"`
	// CancelURL is the invitee's link for canceling the booking.
	CancelURL string `json:"cancelUrl,omitempty"`
	// RescheduleURL is the invitee's link for rescheduling the booking.
	RescheduleURL string `json:"rescheduleUrl,omitempty"`
	// Cancellation describes the cancellation of a canceled invitee.
	Cancellation *Cancellation `json:"cancellation,omitempty"`
	// CreatedAt is when the invitee booked.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// UpdatedAt is when the invitee last changed.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// InviteeAnswer is one answer on an event type's booking form.
type InviteeAnswer struct {
	// Question is the question text.
	Question string `json:"question"`
	// Answer is the invitee's answer.
	Answer string `json:"answer"`
	// Position orders the question on the form.
	Position int `json:"position"`
}

// ListEventInviteesInput selects one page of a scheduled event's invitees.
type ListEventInviteesInput struct {
	// ScheduledEventURI is the event's Calendly URI.
	ScheduledEventURI string `json:"scheduledEventUri"`
	// Status is active or canceled; blank lists both.
	Status string `json:"status,omitempty"`
	// Email limits the page to one invitee email address; blank lists every invitee.
	Email string `json:"email,omitempty"`
	// PageSize is 1 through 100 invitees per page; zero uses Calendly's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page; blank reads the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// EventInviteePage is one page of a scheduled event's invitees, oldest booking first.
type EventInviteePage struct {
	// ScheduledEventURI is the event whose invitees were listed.
	ScheduledEventURI string `json:"scheduledEventUri"`
	// Invitees are the page's invitees.
	Invitees []Invitee `json:"invitees"`
	// NextPageToken reads the next page; blank means this is the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListEventInviteesOperation implements the listEventInvitees Query. Build it with Client.ListEventInvitees.
type ListEventInviteesOperation struct{ client *Client }

type calendlyInvitee struct {
	URI                 string                  `json:"uri"`
	Event               string                  `json:"event"`
	Email               string                  `json:"email"`
	Name                *string                 `json:"name"`
	FirstName           *string                 `json:"first_name"`
	LastName            *string                 `json:"last_name"`
	Status              string                  `json:"status"`
	Timezone            *string                 `json:"timezone"`
	QuestionsAndAnswers []calendlyInviteeAnswer `json:"questions_and_answers"`
	Rescheduled         bool                    `json:"rescheduled"`
	OldInvitee          *string                 `json:"old_invitee"`
	NewInvitee          *string                 `json:"new_invitee"`
	CancelURL           *string                 `json:"cancel_url"`
	RescheduleURL       *string                 `json:"reschedule_url"`
	Cancellation        *calendlyCancellation   `json:"cancellation"`
	CreatedAt           *string                 `json:"created_at"`
	UpdatedAt           *string                 `json:"updated_at"`
}

type calendlyInviteeAnswer struct {
	Question string  `json:"question"`
	Answer   *string `json:"answer"`
	Position int     `json:"position"`
}

// Definition returns the immutable listEventInvitees operation definition.
func (ListEventInviteesOperation) Definition() sdkgo.QueryDefinition {
	return ListEventInviteesDefinition
}

// Invoke reads one page of GET /scheduled_events/{id}/invitees, sorted by booking time. A 404 selects notFound.
func (operation ListEventInviteesOperation) Invoke(call sdkgo.Call, input ListEventInviteesInput) sdkgo.QueryAttempt[EventInviteePage] {
	operationID := ListEventInviteesDefinition.Operation.OperationID
	client := operation.client
	eventID, err := input.validate()
	if err != nil {
		return sdkgo.NewQueryBranch(ListEventInviteesBranchDefect, EventInviteePage{},
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[EventInviteePage](operationID, ListEventInviteesBranchDefect, err)
	}
	response, err := client.sendCalendlyRequest(call, &credentials, http.MethodGet, "/scheduled_events/"+eventID+"/invitees", input.query(), nil, nil)
	branches := queryFailureBranches{
		notFound: ListEventInviteesBranchNotFound, providerRejected: ListEventInviteesBranchProviderRejected,
		invalidResponse: ListEventInviteesBranchInvalidResponse,
	}
	if attempt, isTerminal := classifyQueryExchange[EventInviteePage](client, operationID, response, err, branches, input.ScheduledEventURI); isTerminal {
		return attempt
	}
	var decoded struct {
		Collection []calendlyInvitee  `json:"collection"`
		Pagination calendlyPagination `json:"pagination"`
	}
	page := EventInviteePage{ScheduledEventURI: input.ScheduledEventURI, Invitees: []Invitee{}}
	err = decodeCalendlyJSON(response.body, &decoded)
	if err == nil {
		page.NextPageToken, err = decoded.Pagination.nextPageToken()
	}
	for _, item := range decoded.Collection {
		if err != nil {
			break
		}
		var invitee Invitee
		invitee, err = item.convert()
		if err == nil && invitee.ScheduledEventURI != input.ScheduledEventURI {
			err = errCalendlyResponseMalformed
		}
		page.Invitees = append(page.Invitees, invitee)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(ListEventInviteesBranchInvalidResponse, EventInviteePage{},
			calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt(input.ScheduledEventURI))
	}
	return sdkgo.NewQueryBranch(ListEventInviteesBranchListed, page, nil, client.calendlyReceipt(input.ScheduledEventURI))
}

func (input ListEventInviteesInput) validate() (string, error) {
	eventID, err := parseScheduledEventURI(input.ScheduledEventURI)
	if err != nil {
		return "", err
	}
	switch input.Status {
	case "", ScheduledEventStatusActive, ScheduledEventStatusCanceled:
	default:
		return "", fmt.Errorf("status must be active, canceled, or blank")
	}
	if input.Email != "" {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email {
			return "", fmt.Errorf("email must be one plain email address")
		}
	}
	return eventID, validatePageRequest(input.PageSize, input.PageToken)
}

func (input ListEventInviteesInput) query() url.Values {
	query := url.Values{"sort": {"created_at:asc"}}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	if input.Email != "" {
		query.Set("email", input.Email)
	}
	if input.PageSize > 0 {
		query.Set("count", strconv.Itoa(input.PageSize))
	}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	return query
}

func (invitee calendlyInvitee) convert() (Invitee, error) {
	eventID, _, err := parseInviteeURI(invitee.URI)
	if err != nil || invitee.Event != calendlyAPIBaseURL+"/scheduled_events/"+eventID || invitee.Email == "" {
		return Invitee{}, errCalendlyResponseMalformed
	}
	if invitee.Status != ScheduledEventStatusActive && invitee.Status != ScheduledEventStatusCanceled {
		return Invitee{}, errCalendlyResponseMalformed
	}
	converted := Invitee{
		URI: invitee.URI, ScheduledEventURI: invitee.Event, Email: invitee.Email, Name: stringValue(invitee.Name),
		FirstName: stringValue(invitee.FirstName), LastName: stringValue(invitee.LastName), Status: invitee.Status,
		Timezone: stringValue(invitee.Timezone), Rescheduled: invitee.Rescheduled,
		OldInviteeURI: stringValue(invitee.OldInvitee), NewInviteeURI: stringValue(invitee.NewInvitee),
		CancelURL: stringValue(invitee.CancelURL), RescheduleURL: stringValue(invitee.RescheduleURL),
	}
	for _, answer := range invitee.QuestionsAndAnswers {
		converted.QuestionsAndAnswers = append(converted.QuestionsAndAnswers, InviteeAnswer{
			Question: answer.Question, Answer: stringValue(answer.Answer), Position: answer.Position,
		})
	}
	if converted.CreatedAt, err = parseOptionalCalendlyTimestamp(invitee.CreatedAt); err != nil {
		return Invitee{}, err
	}
	if converted.UpdatedAt, err = parseOptionalCalendlyTimestamp(invitee.UpdatedAt); err != nil {
		return Invitee{}, err
	}
	if invitee.Cancellation != nil {
		cancellation, err := invitee.Cancellation.convert()
		if err != nil {
			return Invitee{}, err
		}
		converted.Cancellation = &cancellation
	}
	return converted, nil
}
