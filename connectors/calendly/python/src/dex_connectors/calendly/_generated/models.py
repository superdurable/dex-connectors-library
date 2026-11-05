# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListScheduledEventsInput selects one page of scheduled events.
@dataclass(frozen=True, slots=True)
class ListScheduledEventsInput:
    # UserURI limits the list to one user's events, such as https://api.calendly.com/users/<id>. With
    # OrganizationURI it selects one member of an organization the token administers.
    user_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'userUri', 'dex_json_omit_default': True})
    # OrganizationURI lists every event of an organization, which needs an owner or admin token. When both
    # URIs are blank the connector lists the connected user's own events.
    organization_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationUri', 'dex_json_omit_default': True})
    # MinStartTime is the required inclusive lower bound of the start-time window.
    min_start_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'minStartTime', 'dex_json_format': 'rfc3339'})
    # MaxStartTime is the required upper bound of the start-time window; it must be after MinStartTime.
    max_start_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'maxStartTime', 'dex_json_format': 'rfc3339'})
    # Status is active or canceled; blank lists both.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # InviteeEmail limits the list to events with this invitee; blank lists every invitee's events.
    invitee_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'inviteeEmail', 'dex_json_omit_default': True})
    # Sort is start_time:asc or start_time:desc; blank uses start_time:asc so pages are stable.
    sort: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PageSize is 1 through 100 events per page; zero uses Calendly's default of 20.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page; blank reads the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListScheduledEventsInput, '__module__', 'dex_connectors.calendly.models')


# ScheduledEventLocation is the location kind and its public details. Conference data such as passwords
# is never copied.
@dataclass(frozen=True, slots=True)
class ScheduledEventLocation:
    # Type is Calendly's location kind, such as physical, zoom, google_conference, or custom.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Location is the address, phone number, or description for a location kind that has one.
    location: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # JoinURL is the conference link once Calendly has created it.
    join_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'joinUrl', 'dex_json_omit_default': True})
    # Status is the conference provisioning status, such as pushed, for conference locations.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ScheduledEventLocation, '__module__', 'dex_connectors.calendly.models')


# ScheduledEventInviteesCounter counts an event's invitees.
@dataclass(frozen=True, slots=True)
class ScheduledEventInviteesCounter:
    # Total counts every invitee, including canceled ones.
    total: int = 0
    # Active counts the invitees who have not canceled.
    active: int = 0
    # Limit is the maximum number of active invitees the event accepts.
    limit: int = 0


setattr(ScheduledEventInviteesCounter, '__module__', 'dex_connectors.calendly.models')


# ScheduledEventHost is one Calendly user who hosts an event.
@dataclass(frozen=True, slots=True)
class ScheduledEventHost:
    # UserURI is the host's Calendly user URI.
    user_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'userUri'})
    # Email is the host's email address.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Name is the host's display name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ScheduledEventHost, '__module__', 'dex_connectors.calendly.models')


# Cancellation describes who canceled an event or an invitee, and why.
@dataclass(frozen=True, slots=True)
class Cancellation:
    # CanceledBy is the name of the person who canceled.
    canceled_by: str = _dataclass_field(default='', metadata={'dex_json_name': 'canceledBy', 'dex_json_omit_default': True})
    # Reason is the cancellation reason, when one was given.
    reason: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CancelerType is host or invitee.
    canceler_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'cancelerType', 'dex_json_omit_default': True})
    # CreatedAt is when the cancellation happened.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Cancellation, '__module__', 'dex_connectors.calendly.models')


# ScheduledEvent is the connector-safe subset of a Calendly scheduled event. Every time is UTC.
# Conference passwords, internal meeting notes, and guest emails are not copied.
@dataclass(frozen=True, slots=True)
class ScheduledEvent:
    # URI is the event's stable Calendly URI, such as https://api.calendly.com/scheduled_events/<id>.
    uri: str = ''
    # Name is the event name, usually the event type's name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is active or canceled.
    status: str = ''
    # StartTime is when the event starts.
    start_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'startTime', 'dex_json_format': 'rfc3339'})
    # EndTime is when the event ends.
    end_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'endTime', 'dex_json_format': 'rfc3339'})
    # EventTypeURI is the URI of the event type the invitee booked.
    event_type_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventTypeUri', 'dex_json_omit_default': True})
    # Location describes where the event takes place.
    location: ScheduledEventLocation = _dataclass_field(default_factory=ScheduledEventLocation)
    # InviteesCounter counts the event's invitees.
    invitees_counter: ScheduledEventInviteesCounter = _dataclass_field(default_factory=ScheduledEventInviteesCounter, metadata={'dex_json_name': 'inviteesCounter'})
    # Hosts lists the Calendly users who host the event.
    hosts: tuple[ScheduledEventHost, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Cancellation describes the cancellation of a canceled event.
    cancellation: Cancellation | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CreatedAt is when the event was booked.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the event last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(ScheduledEvent, '__module__', 'dex_connectors.calendly.models')


# ScheduledEventPage is one page of scheduled events and the scope that was listed.
@dataclass(frozen=True, slots=True)
class ScheduledEventPage:
    # Events are the page's scheduled events in the requested order.
    events: tuple[ScheduledEvent, ...] = ()
    # NextPageToken reads the next page; blank means this is the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # UserURI is the user whose events were listed, including the connected user when the input named none.
    user_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'userUri', 'dex_json_omit_default': True})
    # OrganizationURI is the organization whose events were listed, when one was named.
    organization_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationUri', 'dex_json_omit_default': True})
    # MinStartTime echoes the window's lower bound, so a reader can tell which window was checked.
    min_start_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'minStartTime', 'dex_json_format': 'rfc3339'})
    # MaxStartTime echoes the window's upper bound.
    max_start_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'maxStartTime', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


ScheduledEventPage.__annotations__['events'] = tuple[ScheduledEvent, ...] | None
ScheduledEventPage.__dataclass_fields__['events'].type = tuple[ScheduledEvent, ...] | None
setattr(ScheduledEventPage, '__module__', 'dex_connectors.calendly.models')


# GetScheduledEventInput identifies one scheduled event.
@dataclass(frozen=True, slots=True)
class GetScheduledEventInput:
    # ScheduledEventURI is the event's Calendly URI, such as the scheduledEvent.uri of an InviteeEvent.
    scheduled_event_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledEventUri'})


setattr(GetScheduledEventInput, '__module__', 'dex_connectors.calendly.models')


# ListEventInviteesInput selects one page of a scheduled event's invitees.
@dataclass(frozen=True, slots=True)
class ListEventInviteesInput:
    # ScheduledEventURI is the event's Calendly URI.
    scheduled_event_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledEventUri'})
    # Status is active or canceled; blank lists both.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email limits the page to one invitee email address; blank lists every invitee.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PageSize is 1 through 100 invitees per page; zero uses Calendly's default of 20.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page; blank reads the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListEventInviteesInput, '__module__', 'dex_connectors.calendly.models')


# InviteeAnswer is one answer on an event type's booking form.
@dataclass(frozen=True, slots=True)
class InviteeAnswer:
    # Question is the question text.
    question: str = ''
    # Answer is the invitee's answer.
    answer: str = ''
    # Position orders the question on the form.
    position: int = 0


setattr(InviteeAnswer, '__module__', 'dex_connectors.calendly.models')


# Invitee is the connector-safe subset of a Calendly invitee: the person who booked a scheduled event.
# Every time is UTC. Payment, tracking, and SMS reminder numbers are not copied.
@dataclass(frozen=True, slots=True)
class Invitee:
    # URI is the invitee's stable Calendly URI, such as
    # https://api.calendly.com/scheduled_events/<event id>/invitees/<invitee id>.
    uri: str = ''
    # ScheduledEventURI is the URI of the scheduled event the invitee booked.
    scheduled_event_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledEventUri'})
    # Email is the invitee's email address.
    email: str = ''
    # Name is the invitee's full name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FirstName is set when the event type asks for separate first and last names.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is set when the event type asks for separate first and last names.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # Status is active or canceled.
    status: str = ''
    # Timezone is the IANA time zone the invitee chose, such as America/New_York.
    timezone: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # QuestionsAndAnswers are the invitee's answers to the booking form, in form order.
    questions_and_answers: tuple[InviteeAnswer, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'questionsAndAnswers', 'dex_json_omit_default': True})
    # Rescheduled is true on a canceled invitee that rebooked; NewInviteeURI names the new booking.
    rescheduled: bool = False
    # OldInviteeURI names the canceled booking this invitee replaced, when it is a reschedule.
    old_invitee_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'oldInviteeUri', 'dex_json_omit_default': True})
    # NewInviteeURI names the booking that replaced this canceled invitee.
    new_invitee_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'newInviteeUri', 'dex_json_omit_default': True})
    # CancelURL is the invitee's link for canceling the booking.
    cancel_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'cancelUrl', 'dex_json_omit_default': True})
    # RescheduleURL is the invitee's link for rescheduling the booking.
    reschedule_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'rescheduleUrl', 'dex_json_omit_default': True})
    # Cancellation describes the cancellation of a canceled invitee.
    cancellation: Cancellation | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CreatedAt is when the invitee booked.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the invitee last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Invitee, '__module__', 'dex_connectors.calendly.models')


# EventInviteePage is one page of a scheduled event's invitees, oldest booking first.
@dataclass(frozen=True, slots=True)
class EventInviteePage:
    # ScheduledEventURI is the event whose invitees were listed.
    scheduled_event_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledEventUri'})
    # Invitees are the page's invitees.
    invitees: tuple[Invitee, ...] = ()
    # NextPageToken reads the next page; blank means this is the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'invitees', _replace_nil_slices(self.invitees, 1))


EventInviteePage.__annotations__['invitees'] = tuple[Invitee, ...] | None
EventInviteePage.__dataclass_fields__['invitees'].type = tuple[Invitee, ...] | None
setattr(EventInviteePage, '__module__', 'dex_connectors.calendly.models')


# CancelScheduledEventInput identifies the scheduled event to cancel.
@dataclass(frozen=True, slots=True)
class CancelScheduledEventInput:
    # ScheduledEventURI is the event's Calendly URI.
    scheduled_event_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledEventUri'})
    # Reason is shown to the invitees in Calendly's cancellation notice; blank sends none. At most 10,000
    # characters.
    reason: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(CancelScheduledEventInput, '__module__', 'dex_connectors.calendly.models')


# ScheduledEventCancellation reports a canceled scheduled event.
@dataclass(frozen=True, slots=True)
class ScheduledEventCancellation:
    # ScheduledEventURI is the canceled event's URI.
    scheduled_event_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledEventUri'})
    # AlreadyCanceled is true when the event was canceled before this call, including by a duplicate
    # dispatch of the same Step.
    already_canceled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'alreadyCanceled'})
    # Cancellation describes the cancellation, when Calendly returned it.
    cancellation: Cancellation = _dataclass_field(default_factory=Cancellation)


setattr(ScheduledEventCancellation, '__module__', 'dex_connectors.calendly.models')


# CreateSchedulingLinkInput names the event type a single-use scheduling link books.
@dataclass(frozen=True, slots=True)
class CreateSchedulingLinkInput:
    # EventTypeURI is the event type's Calendly URI, such as the eventTypePicker unit's eventTypeUri.
    event_type_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventTypeUri'})


setattr(CreateSchedulingLinkInput, '__module__', 'dex_connectors.calendly.models')


# SchedulingLink is one single-use Calendly booking link. It accepts exactly one booking.
@dataclass(frozen=True, slots=True)
class SchedulingLink:
    # BookingURL is the link to send to the invitee.
    booking_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'bookingUrl'})
    # EventTypeURI is the event type the link books.
    event_type_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventTypeUri'})


setattr(SchedulingLink, '__module__', 'dex_connectors.calendly.models')


# CreateWebhookSubscriptionInput registers one callback URL for invitee webhooks.
@dataclass(frozen=True, slots=True)
class CreateWebhookSubscriptionInput:
    # CallbackURL is the public HTTPS URL where the application mounts the inviteeEventReceived endpoint,
    # such as https://example.com/webhooks/calendly.
    callback_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'callbackUrl'})
    # Scope is user or organization; blank uses user. An organization subscription needs an owner or admin.
    scope: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Events lists invitee.created and invitee.canceled; blank subscribes to both.
    events: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # UserURI is the user of a user subscription; blank uses the connected user.
    user_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'userUri', 'dex_json_omit_default': True})
    # OrganizationURI owns the subscription; blank uses the connected user's current organization.
    organization_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationUri', 'dex_json_omit_default': True})


setattr(CreateWebhookSubscriptionInput, '__module__', 'dex_connectors.calendly.models')


# WebhookSubscription is one Calendly webhook subscription.
@dataclass(frozen=True, slots=True)
class WebhookSubscription:
    # URI is the subscription's Calendly URI, used to delete it.
    uri: str = ''
    # CallbackURL is the URL Calendly posts events to.
    callback_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'callbackUrl'})
    # State is active or disabled; Calendly disables a subscription after 24 hours of failed deliveries.
    state: str = ''
    # Events are the webhook events the subscription delivers.
    events: tuple[str, ...] = ()
    # Scope is user, organization, or group.
    scope: str = ''
    # OrganizationURI is the organization that owns the subscription.
    organization_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationUri'})
    # UserURI is the user of a user subscription.
    user_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'userUri', 'dex_json_omit_default': True})
    # CreatedAt is when the subscription was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WasCreated is true when this attempt created the subscription, and false when it reused one. A
    # duplicate dispatch of the same Step can report false for the subscription its first attempt created.
    was_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasCreated'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


WebhookSubscription.__annotations__['events'] = tuple[str, ...] | None
WebhookSubscription.__dataclass_fields__['events'].type = tuple[str, ...] | None
setattr(WebhookSubscription, '__module__', 'dex_connectors.calendly.models')
