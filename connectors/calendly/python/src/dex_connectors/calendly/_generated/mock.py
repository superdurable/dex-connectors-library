# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CancelScheduledEventInput, Cancellation, CreateSchedulingLinkInput, CreateWebhookSubscriptionInput, EventInviteePage, GetScheduledEventInput, Invitee, InviteeAnswer, ListEventInviteesInput, ListScheduledEventsInput, ScheduledEvent, ScheduledEventCancellation, ScheduledEventHost, ScheduledEventInviteesCounter, ScheduledEventLocation, ScheduledEventPage, SchedulingLink, WebhookSubscription
from .definitions import CONNECTOR_ID, CANCELED, CANCEL_SCHEDULED_EVENT_DEFINITION, CONFLICTING_SUBSCRIPTION, CREATED, CREATE_SCHEDULING_LINK_DEFINITION, CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION, DEFECT, FOUND, GET_SCHEDULED_EVENT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_EVENT_INVITEES_DEFINITION, LIST_SCHEDULED_EVENTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, SUBSCRIBED, UNCERTAIN

def list_scheduled_events_listed(value: ScheduledEventPage, failure: Failure | None = None) -> Case[ScheduledEventPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_scheduled_events_listed, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_provider_rejected(value: ScheduledEventPage, failure: Failure | None = None) -> Case[ScheduledEventPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_scheduled_events_provider_rejected, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_invalid_response(value: ScheduledEventPage, failure: Failure | None = None) -> Case[ScheduledEventPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_scheduled_events_invalid_response, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_defect(value: ScheduledEventPage, failure: Failure | None = None) -> Case[ScheduledEventPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_scheduled_events_defect, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_retry(failure: Failure, after_seconds: float = 0) -> Case[ScheduledEventPage]:
    return Case.retry(failure, after_seconds)

setattr(list_scheduled_events_retry, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_three_scheduled_event_pages() -> Case[ScheduledEventPage]:
    return Case.paged(LISTED, (ScheduledEventPage(events=(ScheduledEvent(uri='https://api.calendly.com/scheduled_events/EVT00001', name='Discovery Call', status='active', start_time=datetime.fromisoformat("2026-11-02T17:00:00Z".replace('Z', '+00:00')), end_time=datetime.fromisoformat("2026-11-02T17:30:00Z".replace('Z', '+00:00')), event_type_uri='https://api.calendly.com/event_types/TYPE0001', location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter(total=1, active=1, limit=1)),), next_page_token='page-two', user_uri='https://api.calendly.com/users/USER0001', min_start_time=datetime.fromisoformat("2026-11-01T00:00:00Z".replace('Z', '+00:00')), max_start_time=datetime.fromisoformat("2026-12-01T00:00:00Z".replace('Z', '+00:00'))), ScheduledEventPage(events=(ScheduledEvent(uri='https://api.calendly.com/scheduled_events/EVT00002', name='Planning Call', status='active', start_time=datetime.fromisoformat("2026-11-03T17:00:00Z".replace('Z', '+00:00')), end_time=datetime.fromisoformat("2026-11-03T17:30:00Z".replace('Z', '+00:00')), event_type_uri='https://api.calendly.com/event_types/TYPE0001', location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter(total=1, active=1, limit=1)), ScheduledEvent(uri='https://api.calendly.com/scheduled_events/EVT00001', name='Discovery Call', status='active', start_time=datetime.fromisoformat("2026-11-02T17:00:00Z".replace('Z', '+00:00')), end_time=datetime.fromisoformat("2026-11-02T17:30:00Z".replace('Z', '+00:00')), event_type_uri='https://api.calendly.com/event_types/TYPE0001', location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter(total=1, active=1, limit=1))), next_page_token='page-three', user_uri='https://api.calendly.com/users/USER0001', min_start_time=datetime.fromisoformat("2026-11-01T00:00:00Z".replace('Z', '+00:00')), max_start_time=datetime.fromisoformat("2026-12-01T00:00:00Z".replace('Z', '+00:00'))), ScheduledEventPage(events=(), user_uri='https://api.calendly.com/users/USER0001', min_start_time=datetime.fromisoformat("2026-11-01T00:00:00Z".replace('Z', '+00:00')), max_start_time=datetime.fromisoformat("2026-12-01T00:00:00Z".replace('Z', '+00:00')))))

setattr(list_scheduled_events_three_scheduled_event_pages, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_provider_rejected_failure() -> Case[ScheduledEventPage]:
    return Case.branch_result(PROVIDER_REJECTED, ScheduledEventPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listScheduledEvents", "Calendly rejected the access token."))

setattr(list_scheduled_events_provider_rejected_failure, '__module__', "dex_connectors.calendly.calendlymock")

def list_scheduled_events_invalid_response_failure() -> Case[ScheduledEventPage]:
    return Case.branch_result(INVALID_RESPONSE, ScheduledEventPage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listScheduledEvents", "Calendly returned a malformed response."))

setattr(list_scheduled_events_invalid_response_failure, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_found(value: ScheduledEvent, failure: Failure | None = None) -> Case[ScheduledEvent]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_scheduled_event_found, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_not_found(value: ScheduledEvent, failure: Failure | None = None) -> Case[ScheduledEvent]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_scheduled_event_not_found, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_provider_rejected(value: ScheduledEvent, failure: Failure | None = None) -> Case[ScheduledEvent]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_scheduled_event_provider_rejected, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_invalid_response(value: ScheduledEvent, failure: Failure | None = None) -> Case[ScheduledEvent]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_scheduled_event_invalid_response, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_defect(value: ScheduledEvent, failure: Failure | None = None) -> Case[ScheduledEvent]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_scheduled_event_defect, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_retry(failure: Failure, after_seconds: float = 0) -> Case[ScheduledEvent]:
    return Case.retry(failure, after_seconds)

setattr(get_scheduled_event_retry, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_discovery_call() -> Case[ScheduledEvent]:
    return Case.branch_result(FOUND, ScheduledEvent(uri='https://api.calendly.com/scheduled_events/EVT00001', name='Discovery Call', status='active', start_time=datetime.fromisoformat("2026-11-02T17:00:00Z".replace('Z', '+00:00')), end_time=datetime.fromisoformat("2026-11-02T17:30:00Z".replace('Z', '+00:00')), event_type_uri='https://api.calendly.com/event_types/TYPE0001', location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter(total=1, active=1, limit=1)))

setattr(get_scheduled_event_discovery_call, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_not_found_failure() -> Case[ScheduledEvent]:
    return Case.branch_result(NOT_FOUND, ScheduledEvent(location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getScheduledEvent", "The scheduled event does not exist or is not visible to the token."))

setattr(get_scheduled_event_not_found_failure, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_provider_rejected_failure() -> Case[ScheduledEvent]:
    return Case.branch_result(PROVIDER_REJECTED, ScheduledEvent(location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getScheduledEvent", "Calendly rejected the access token."))

setattr(get_scheduled_event_provider_rejected_failure, '__module__', "dex_connectors.calendly.calendlymock")

def get_scheduled_event_invalid_response_failure() -> Case[ScheduledEvent]:
    return Case.branch_result(INVALID_RESPONSE, ScheduledEvent(location=ScheduledEventLocation(), invitees_counter=ScheduledEventInviteesCounter()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getScheduledEvent", "Calendly returned a malformed response."))

setattr(get_scheduled_event_invalid_response_failure, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_listed(value: EventInviteePage, failure: Failure | None = None) -> Case[EventInviteePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_event_invitees_listed, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_not_found(value: EventInviteePage, failure: Failure | None = None) -> Case[EventInviteePage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_event_invitees_not_found, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_provider_rejected(value: EventInviteePage, failure: Failure | None = None) -> Case[EventInviteePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_event_invitees_provider_rejected, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_invalid_response(value: EventInviteePage, failure: Failure | None = None) -> Case[EventInviteePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_event_invitees_invalid_response, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_defect(value: EventInviteePage, failure: Failure | None = None) -> Case[EventInviteePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_event_invitees_defect, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_retry(failure: Failure, after_seconds: float = 0) -> Case[EventInviteePage]:
    return Case.retry(failure, after_seconds)

setattr(list_event_invitees_retry, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_three_invitee_pages() -> Case[EventInviteePage]:
    return Case.paged(LISTED, (EventInviteePage(scheduled_event_uri='https://api.calendly.com/scheduled_events/EVT00001', invitees=(Invitee(uri='https://api.calendly.com/scheduled_events/EVT00001/invitees/INV00001', scheduled_event_uri='https://api.calendly.com/scheduled_events/EVT00001', email='ada@example.com', name='Ada Lovelace', status='active'),), next_page_token='page-two'), EventInviteePage(scheduled_event_uri='https://api.calendly.com/scheduled_events/EVT00001', invitees=(Invitee(uri='https://api.calendly.com/scheduled_events/EVT00001/invitees/INV00001', scheduled_event_uri='https://api.calendly.com/scheduled_events/EVT00001', email='ada@example.com', name='Ada Lovelace', status='active'),), next_page_token='page-three'), EventInviteePage(scheduled_event_uri='https://api.calendly.com/scheduled_events/EVT00001', invitees=())))

setattr(list_event_invitees_three_invitee_pages, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_not_found_failure() -> Case[EventInviteePage]:
    return Case.branch_result(NOT_FOUND, EventInviteePage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listEventInvitees", "The scheduled event does not exist or is not visible to the token."))

setattr(list_event_invitees_not_found_failure, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_provider_rejected_failure() -> Case[EventInviteePage]:
    return Case.branch_result(PROVIDER_REJECTED, EventInviteePage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listEventInvitees", "Calendly rejected the access token."))

setattr(list_event_invitees_provider_rejected_failure, '__module__', "dex_connectors.calendly.calendlymock")

def list_event_invitees_invalid_response_failure() -> Case[EventInviteePage]:
    return Case.branch_result(INVALID_RESPONSE, EventInviteePage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listEventInvitees", "Calendly returned a malformed response."))

setattr(list_event_invitees_invalid_response_failure, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_canceled(value: ScheduledEventCancellation, failure: Failure | None = None) -> Case[ScheduledEventCancellation]:
    return Case.branch_result(CANCELED, value, failure)

setattr(cancel_scheduled_event_canceled, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_not_found(value: ScheduledEventCancellation, failure: Failure | None = None) -> Case[ScheduledEventCancellation]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(cancel_scheduled_event_not_found, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_provider_rejected(value: ScheduledEventCancellation, failure: Failure | None = None) -> Case[ScheduledEventCancellation]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(cancel_scheduled_event_provider_rejected, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_invalid_response(value: ScheduledEventCancellation, failure: Failure | None = None) -> Case[ScheduledEventCancellation]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(cancel_scheduled_event_invalid_response, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_defect(value: ScheduledEventCancellation, failure: Failure | None = None) -> Case[ScheduledEventCancellation]:
    return Case.branch_result(DEFECT, value, failure)

setattr(cancel_scheduled_event_defect, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_retry(failure: Failure, after_seconds: float = 0) -> Case[ScheduledEventCancellation]:
    return Case.retry(failure, after_seconds)

setattr(cancel_scheduled_event_retry, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_canceled_discovery_call() -> Case[ScheduledEventCancellation]:
    return Case.branch_result(CANCELED, ScheduledEventCancellation(scheduled_event_uri='https://api.calendly.com/scheduled_events/EVT00001', already_canceled=False, cancellation=Cancellation(canceled_by='Hana Host', reason='Schedule changed', canceler_type='host', created_at=datetime.fromisoformat("2026-10-27T10:00:00Z".replace('Z', '+00:00')))))

setattr(cancel_scheduled_event_canceled_discovery_call, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_not_found_failure() -> Case[ScheduledEventCancellation]:
    return Case.branch_result(NOT_FOUND, ScheduledEventCancellation(cancellation=Cancellation()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "cancelScheduledEvent", "The scheduled event does not exist or is not visible to the token."))

setattr(cancel_scheduled_event_not_found_failure, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_provider_rejected_failure() -> Case[ScheduledEventCancellation]:
    return Case.branch_result(PROVIDER_REJECTED, ScheduledEventCancellation(cancellation=Cancellation()), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "cancelScheduledEvent", "Calendly cannot cancel an event that has already started."))

setattr(cancel_scheduled_event_provider_rejected_failure, '__module__', "dex_connectors.calendly.calendlymock")

def cancel_scheduled_event_invalid_response_failure() -> Case[ScheduledEventCancellation]:
    return Case.branch_result(INVALID_RESPONSE, ScheduledEventCancellation(cancellation=Cancellation()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "cancelScheduledEvent", "Calendly returned a malformed response."))

setattr(cancel_scheduled_event_invalid_response_failure, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_created(value: SchedulingLink, failure: Failure | None = None) -> Case[SchedulingLink]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_scheduling_link_created, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_provider_rejected(value: SchedulingLink, failure: Failure | None = None) -> Case[SchedulingLink]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_scheduling_link_provider_rejected, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_uncertain(value: SchedulingLink, failure: Failure) -> Case[SchedulingLink]:
    return Case.uncertain(value, failure)

setattr(create_scheduling_link_uncertain, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_defect(value: SchedulingLink, failure: Failure | None = None) -> Case[SchedulingLink]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_scheduling_link_defect, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_retry(failure: Failure, after_seconds: float = 0) -> Case[SchedulingLink]:
    return Case.retry(failure, after_seconds)

setattr(create_scheduling_link_retry, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_single_use_discovery_link() -> Case[SchedulingLink]:
    return Case.branch_result(CREATED, SchedulingLink(booking_url='https://calendly.com/d/abcd-1234/discovery-call', event_type_uri='https://api.calendly.com/event_types/TYPE0001'))

setattr(create_scheduling_link_single_use_discovery_link, '__module__', "dex_connectors.calendly.calendlymock")

def create_scheduling_link_provider_rejected_failure() -> Case[SchedulingLink]:
    return Case.branch_result(PROVIDER_REJECTED, SchedulingLink(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createSchedulingLink", "Calendly denied permission for the selected event type."))

setattr(create_scheduling_link_provider_rejected_failure, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_subscribed(value: WebhookSubscription, failure: Failure | None = None) -> Case[WebhookSubscription]:
    return Case.branch_result(SUBSCRIBED, value, failure)

setattr(create_webhook_subscription_subscribed, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_conflicting_subscription(value: WebhookSubscription, failure: Failure | None = None) -> Case[WebhookSubscription]:
    return Case.branch_result(CONFLICTING_SUBSCRIPTION, value, failure)

setattr(create_webhook_subscription_conflicting_subscription, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_provider_rejected(value: WebhookSubscription, failure: Failure | None = None) -> Case[WebhookSubscription]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_webhook_subscription_provider_rejected, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_invalid_response(value: WebhookSubscription, failure: Failure | None = None) -> Case[WebhookSubscription]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_webhook_subscription_invalid_response, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_defect(value: WebhookSubscription, failure: Failure | None = None) -> Case[WebhookSubscription]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_webhook_subscription_defect, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_retry(failure: Failure, after_seconds: float = 0) -> Case[WebhookSubscription]:
    return Case.retry(failure, after_seconds)

setattr(create_webhook_subscription_retry, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_active_invitee_webhook() -> Case[WebhookSubscription]:
    return Case.branch_result(SUBSCRIBED, WebhookSubscription(uri='https://api.calendly.com/webhook_subscriptions/WH00001', callback_url='https://example.com/webhooks/calendly', state='active', events=('invitee.created', 'invitee.canceled'), scope='user', organization_uri='https://api.calendly.com/organizations/ORG00001', user_uri='https://api.calendly.com/users/USER0001', created_at=datetime.fromisoformat("2026-10-27T09:00:00Z".replace('Z', '+00:00')), was_created=True))

setattr(create_webhook_subscription_active_invitee_webhook, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_conflicting_subscription_failure() -> Case[WebhookSubscription]:
    return Case.branch_result(CONFLICTING_SUBSCRIPTION, WebhookSubscription(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createWebhookSubscription", "A subscription with this callback URL already exists under another scope or user."))

setattr(create_webhook_subscription_conflicting_subscription_failure, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_provider_rejected_failure() -> Case[WebhookSubscription]:
    return Case.branch_result(PROVIDER_REJECTED, WebhookSubscription(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createWebhookSubscription", "Calendly denied permission for the webhook scope."))

setattr(create_webhook_subscription_provider_rejected_failure, '__module__', "dex_connectors.calendly.calendlymock")

def create_webhook_subscription_invalid_response_failure() -> Case[WebhookSubscription]:
    return Case.branch_result(INVALID_RESPONSE, WebhookSubscription(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "createWebhookSubscription", "Calendly returned a malformed response."))

setattr(create_webhook_subscription_invalid_response_failure, '__module__', "dex_connectors.calendly.calendlymock")


class Mock:

    def __init__(self, connection_name: str = "calendly") -> None:
        self.list_scheduled_events_script: Query[ListScheduledEventsInput, ScheduledEventPage] = Query(LIST_SCHEDULED_EVENTS_DEFINITION)
        self.list_scheduled_events_script.default(list_scheduled_events_three_scheduled_event_pages())
        self.get_scheduled_event_script: Query[GetScheduledEventInput, ScheduledEvent] = Query(GET_SCHEDULED_EVENT_DEFINITION)
        self.get_scheduled_event_script.default(get_scheduled_event_discovery_call())
        self.list_event_invitees_script: Query[ListEventInviteesInput, EventInviteePage] = Query(LIST_EVENT_INVITEES_DEFINITION)
        self.list_event_invitees_script.default(list_event_invitees_three_invitee_pages())
        self.cancel_scheduled_event_script: Mutation[CancelScheduledEventInput, ScheduledEventCancellation] = Mutation(CANCEL_SCHEDULED_EVENT_DEFINITION)
        self.cancel_scheduled_event_script.default(cancel_scheduled_event_canceled_discovery_call())
        self.create_scheduling_link_script: Mutation[CreateSchedulingLinkInput, SchedulingLink] = Mutation(CREATE_SCHEDULING_LINK_DEFINITION)
        self.create_scheduling_link_script.default(create_scheduling_link_single_use_discovery_link())
        self.create_webhook_subscription_script: Mutation[CreateWebhookSubscriptionInput, WebhookSubscription] = Mutation(CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION)
        self.create_webhook_subscription_script.default(create_webhook_subscription_active_invitee_webhook())
        self._connection = new_connection_with_operations(
            list_scheduled_events=self.list_scheduled_events_script,
            get_scheduled_event=self.get_scheduled_event_script,
            list_event_invitees=self.list_event_invitees_script,
            cancel_scheduled_event=self.cancel_scheduled_event_script,
            create_scheduling_link=self.create_scheduling_link_script,
            create_webhook_subscription=self.create_webhook_subscription_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.calendly.calendlymock")

__all__ = [
    "Mock",
    "list_scheduled_events_listed",
    "list_scheduled_events_provider_rejected",
    "list_scheduled_events_invalid_response",
    "list_scheduled_events_defect",
    "list_scheduled_events_retry",
    "list_scheduled_events_three_scheduled_event_pages",
    "list_scheduled_events_provider_rejected_failure",
    "list_scheduled_events_invalid_response_failure",
    "get_scheduled_event_found",
    "get_scheduled_event_not_found",
    "get_scheduled_event_provider_rejected",
    "get_scheduled_event_invalid_response",
    "get_scheduled_event_defect",
    "get_scheduled_event_retry",
    "get_scheduled_event_discovery_call",
    "get_scheduled_event_not_found_failure",
    "get_scheduled_event_provider_rejected_failure",
    "get_scheduled_event_invalid_response_failure",
    "list_event_invitees_listed",
    "list_event_invitees_not_found",
    "list_event_invitees_provider_rejected",
    "list_event_invitees_invalid_response",
    "list_event_invitees_defect",
    "list_event_invitees_retry",
    "list_event_invitees_three_invitee_pages",
    "list_event_invitees_not_found_failure",
    "list_event_invitees_provider_rejected_failure",
    "list_event_invitees_invalid_response_failure",
    "cancel_scheduled_event_canceled",
    "cancel_scheduled_event_not_found",
    "cancel_scheduled_event_provider_rejected",
    "cancel_scheduled_event_invalid_response",
    "cancel_scheduled_event_defect",
    "cancel_scheduled_event_retry",
    "cancel_scheduled_event_canceled_discovery_call",
    "cancel_scheduled_event_not_found_failure",
    "cancel_scheduled_event_provider_rejected_failure",
    "cancel_scheduled_event_invalid_response_failure",
    "create_scheduling_link_created",
    "create_scheduling_link_provider_rejected",
    "create_scheduling_link_uncertain",
    "create_scheduling_link_defect",
    "create_scheduling_link_retry",
    "create_scheduling_link_single_use_discovery_link",
    "create_scheduling_link_provider_rejected_failure",
    "create_webhook_subscription_subscribed",
    "create_webhook_subscription_conflicting_subscription",
    "create_webhook_subscription_provider_rejected",
    "create_webhook_subscription_invalid_response",
    "create_webhook_subscription_defect",
    "create_webhook_subscription_retry",
    "create_webhook_subscription_active_invitee_webhook",
    "create_webhook_subscription_conflicting_subscription_failure",
    "create_webhook_subscription_provider_rejected_failure",
    "create_webhook_subscription_invalid_response_failure",
]
