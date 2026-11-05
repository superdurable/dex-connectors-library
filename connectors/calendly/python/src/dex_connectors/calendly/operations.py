# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Calendly provider conversion and Dex operation invocations."""

import ipaddress
import json
import re
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any, cast
from urllib.parse import urlsplit

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    CANCEL_SCHEDULED_EVENT_DEFINITION,
    CREATE_SCHEDULING_LINK_DEFINITION,
    CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION,
    GET_SCHEDULED_EVENT_DEFINITION,
    LIST_EVENT_INVITEES_DEFINITION,
    LIST_SCHEDULED_EVENTS_DEFINITION,
)
from .client import API_BASE, Client, Credentials, Exchange, failure
from .models import (
    Cancellation,
    CancelScheduledEventInput,
    CreateSchedulingLinkInput,
    CreateWebhookSubscriptionInput,
    EventInviteePage,
    GetScheduledEventInput,
    Invitee,
    InviteeAnswer,
    ListEventInviteesInput,
    ListScheduledEventsInput,
    ScheduledEvent,
    ScheduledEventCancellation,
    ScheduledEventHost,
    ScheduledEventInviteesCounter,
    ScheduledEventLocation,
    ScheduledEventPage,
    SchedulingLink,
    WebhookSubscription,
)

_RESOURCE_ID = re.compile(r"[A-Za-z0-9_-]{1,64}\Z")
_PAGE_TOKEN = re.compile(r"[A-Za-z0-9_\-.~=+/]{1,512}\Z")
_TIMESTAMP = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})\Z")
_ZERO_TIME = datetime.min.replace(tzinfo=UTC)


def _resource_id(value: str, collection: str, noun: str) -> str:
    prefix = f"{API_BASE}/{collection}/"
    identifier = value[len(prefix) :] if isinstance(value, str) and value.startswith(prefix) else ""
    if not _RESOURCE_ID.fullmatch(identifier):
        raise ValueError(f"{noun} URI must look like {prefix}<id>")
    return identifier


def _page(size: int, token: str) -> None:
    if type(size) is not int or size < 0 or size > 100:
        raise ValueError("pageSize must be from 1 through 100, or 0 for Calendly's default of 20")
    if token and not _PAGE_TOKEN.fullmatch(token):
        raise ValueError("pageToken must be the nextPageToken value of an earlier page")


def _email(value: str) -> bool:
    if not isinstance(value, str) or value.count("@") != 1:
        return False
    local, domain = value.split("@")
    forbidden = set('()<>[]:;@\\,"')

    def is_atom(part: str) -> bool:
        return bool(part) and all(
            character not in forbidden
            and not character.isspace()
            and ord(character) > 32
            and ord(character) != 127
            for character in part
        )

    if not all(is_atom(part) for part in local.split(".")):
        return False
    if domain.startswith("[") and domain.endswith("]"):
        try:
            ipaddress.ip_address(domain[1:-1])
            return True
        except ValueError:
            return False
    return all(is_atom(part) for part in domain.split("."))


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict):
        raise ValueError("invalid Calendly object")
    return value


def _string(record: dict[str, object], name: str) -> str:
    value = record.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid Calendly string")
    return value


def _number(record: dict[str, object], name: str) -> int:
    value = record.get(name)
    if value is None:
        return 0
    if type(value) not in (int, float):
        raise ValueError("invalid Calendly number")
    return int(cast(float | int, value))


def _integer(record: dict[str, object], name: str) -> int:
    value = record.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError("invalid Calendly integer")
    return value


def _boolean(record: dict[str, object], name: str) -> bool:
    value = record.get(name)
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("invalid Calendly boolean")
    return value


def _array(record: dict[str, object], name: str) -> list[object]:
    value = record.get(name)
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("invalid Calendly array")
    return value


def _timestamp(value: str, optional: bool = False) -> datetime:
    if optional and not value:
        return _ZERO_TIME
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise ValueError("invalid Calendly timestamp")
    year, month, day, hour, minute, second = map(int, match.groups()[:6])
    fraction = match.group(7) or ""
    zone = match.group(8)
    if minute > 59 or second > 59 or (zone != "Z" and int(zone[-2:]) > 59):
        raise ValueError("invalid Calendly timestamp")
    try:
        parsed = datetime(year, month, day, hour, minute, second, int((fraction + "000000")[:6]), tzinfo=UTC)
        if zone != "Z":
            offset = int(zone[1:3]) * 60 + int(zone[-2:])
            parsed -= timedelta(minutes=offset if zone[0] == "+" else -offset)
        return parsed
    except (ValueError, OverflowError) as error:
        raise ValueError("invalid Calendly timestamp") from error


def _rfc3339(value: datetime) -> str:
    value = value.astimezone(UTC)
    return value.isoformat(timespec="microseconds").replace("+00:00", "Z")


def _body(exchange: Exchange) -> dict[str, object]:
    return _object(json.loads(exchange.body))


def _resource(exchange: Exchange) -> dict[str, object]:
    return _object(_body(exchange).get("resource"))


def _pagination(response: dict[str, object]) -> str:
    token = _string(
        _object(response["pagination"]) if response.get("pagination") is not None else {}, "next_page_token"
    )
    if token and not _PAGE_TOKEN.fullmatch(token):
        raise ValueError("invalid Calendly pagination")
    return token


def _cancellation(value: object) -> Cancellation:
    item = _object(value)
    return Cancellation(
        canceled_by=_string(item, "canceled_by"),
        reason=_string(item, "reason"),
        canceler_type=_string(item, "canceler_type"),
        created_at=_timestamp(_string(item, "created_at"), True),
    )


def _event(value: object) -> ScheduledEvent:
    item = _object(value)
    uri = _string(item, "uri")
    _resource_id(uri, "scheduled_events", "scheduled event")
    status = _string(item, "status")
    if status not in ("active", "canceled"):
        raise ValueError("invalid Calendly event status")
    location = _object(item["location"]) if item.get("location") is not None else {}
    counter = _object(item["invitees_counter"]) if item.get("invitees_counter") is not None else {}
    raw_location = location.get("location")
    hosts = tuple(
        ScheduledEventHost(
            user_uri=_string(host, "user"),
            email=_string(host, "user_email"),
            name=_string(host, "user_name"),
        )
        for host in (_object(value) for value in _array(item, "event_memberships"))
    )
    return ScheduledEvent(
        uri=uri,
        name=_string(item, "name"),
        status=status,
        start_time=_timestamp(_string(item, "start_time")),
        end_time=_timestamp(_string(item, "end_time")),
        event_type_uri=_string(item, "event_type"),
        location=ScheduledEventLocation(
            type=_string(location, "type"),
            location=raw_location.strip() if isinstance(raw_location, str) else "",
            join_url=_string(location, "join_url"),
            status=_string(location, "status"),
        ),
        invitees_counter=ScheduledEventInviteesCounter(
            total=_number(counter, "total"),
            active=_number(counter, "active"),
            limit=_number(counter, "limit"),
        ),
        hosts=hosts,
        cancellation=_cancellation(item["cancellation"]) if item.get("cancellation") is not None else None,
        created_at=_timestamp(_string(item, "created_at"), True),
        updated_at=_timestamp(_string(item, "updated_at"), True),
    )


def _invitee(value: object, expected_event: str) -> Invitee:
    item = _object(value)
    uri = _string(item, "uri")
    match = re.fullmatch(
        r"https://api\.calendly\.com/scheduled_events/([A-Za-z0-9_-]{1,64})/invitees/([A-Za-z0-9_-]{1,64})",
        uri,
    )
    event_uri = _string(item, "event")
    status = _string(item, "status")
    if (
        match is None
        or event_uri != f"{API_BASE}/scheduled_events/{match.group(1)}"
        or event_uri != expected_event
        or not _string(item, "email")
        or status not in ("active", "canceled")
    ):
        raise ValueError("invalid Calendly invitee")
    answers = tuple(
        InviteeAnswer(
            question=_string(answer, "question"),
            answer=_string(answer, "answer"),
            position=_integer(answer, "position"),
        )
        for answer in (_object(value) for value in _array(item, "questions_and_answers"))
    )
    return Invitee(
        uri=uri,
        scheduled_event_uri=event_uri,
        email=_string(item, "email"),
        status=status,
        name=_string(item, "name"),
        first_name=_string(item, "first_name"),
        last_name=_string(item, "last_name"),
        timezone=_string(item, "timezone"),
        questions_and_answers=answers,
        rescheduled=_boolean(item, "rescheduled"),
        old_invitee_uri=_string(item, "old_invitee"),
        new_invitee_uri=_string(item, "new_invitee"),
        cancel_url=_string(item, "cancel_url"),
        reschedule_url=_string(item, "reschedule_url"),
        cancellation=_cancellation(item["cancellation"]) if item.get("cancellation") is not None else None,
        created_at=_timestamp(_string(item, "created_at"), True),
        updated_at=_timestamp(_string(item, "updated_at"), True),
    )


def _subscription(value: object) -> WebhookSubscription:
    item = _object(value)
    uri = _string(item, "uri")
    _resource_id(uri, "webhook_subscriptions", "webhook subscription")
    callback = _string(item, "callback_url")
    state = _string(item, "state")
    scope = _string(item, "scope")
    if not callback or not state or not scope:
        raise ValueError("invalid Calendly subscription")
    events = _array(item, "events")
    if any(not isinstance(event, str) for event in events):
        raise ValueError("invalid Calendly subscription events")
    return WebhookSubscription(
        uri=uri,
        callback_url=callback,
        state=state,
        events=tuple(cast(list[str], events)),
        scope=scope,
        organization_uri=_string(item, "organization"),
        user_uri=_string(item, "user"),
        created_at=_timestamp(_string(item, "created_at"), True),
    )


def _query_exchange(
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    zero: object,
    object_uri: str = "",
    has_not_found: bool = False,
) -> QueryAttempt[Any] | None:
    if exchange.error == "defect":
        return QueryAttempt.branch_result(
            "defect",
            zero,
            Receipt(),
            failure(operation, FailureKind.LOCAL_DEFECT, "Calendly request could not be built"),
        )
    if exchange.error in ("tooLarge", "malformed"):
        if exchange.error == "malformed":
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                client.receipt(call, object_uri),
                failure(operation, FailureKind.PROTOCOL, "Calendly returned a malformed response"),
            )
        return QueryAttempt.branch_result(
            "invalidResponse",
            zero,
            client.receipt(call, object_uri),
            failure(
                operation,
                FailureKind.RESPONSE_TOO_LARGE,
                "Calendly response exceeds the configured maxResponseBytes",
            ),
        )
    if exchange.error:
        return QueryAttempt.retry(failure(operation, FailureKind.TRANSPORT, "Calendly could not be reached"))
    if 200 <= exchange.status < 300:
        return None
    problem, delay, is_retry = client.classify(operation, exchange)
    if is_retry:
        return QueryAttempt.retry(problem, delay)
    return QueryAttempt.branch_result(
        "notFound" if has_not_found and exchange.status == 404 else "providerRejected",
        zero,
        client.receipt(call, object_uri),
        problem,
    )


async def _current_user(client: Client, call: Call, credentials: Credentials) -> tuple[str, str, Exchange]:
    exchange = await client.exchange(call, credentials, "GET", "/users/me")
    if exchange.error or exchange.status != 200:
        return "", "", exchange
    try:
        resource = _resource(exchange)
        user = _string(resource, "uri")
        organization = _string(resource, "current_organization")
        _resource_id(user, "users", "user")
        _resource_id(organization, "organizations", "organization")
        return user, organization, exchange
    except (ValueError, TypeError, KeyError):
        return "", "", Exchange(error="malformed")


def _credential_query(
    operation: str, kind: FailureKind | None, message: str, zero: object
) -> QueryAttempt[Any] | None:
    if kind is None:
        return None
    if kind == FailureKind.AVAILABILITY:
        return QueryAttempt.retry(failure(operation, kind, message))
    return QueryAttempt.branch_result("defect", zero, Receipt(), failure(operation, kind, message))


def _credential_mutation(
    operation: str, kind: FailureKind | None, message: str, zero: object
) -> MutationAttempt[Any] | None:
    if kind is None:
        return None
    if kind == FailureKind.AVAILABILITY:
        return MutationAttempt.retry(failure(operation, kind, message))
    return MutationAttempt.branch_result("defect", zero, Receipt(), failure(operation, kind, message))


class ListScheduledEventsOperation:
    """List one bounded page of Calendly scheduled events."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation contract."""
        return LIST_SCHEDULED_EVENTS_DEFINITION

    async def invoke(self, call: Call, input: ListScheduledEventsInput) -> QueryAttempt[ScheduledEventPage]:
        """Resolve the scope and decode one page of events."""
        zero = ScheduledEventPage()
        try:
            if input.user_uri:
                _resource_id(input.user_uri, "users", "user")
            if input.organization_uri:
                _resource_id(input.organization_uri, "organizations", "organization")
            if input.min_start_time == _ZERO_TIME or input.max_start_time == _ZERO_TIME:
                raise ValueError("listScheduledEvents requires both minStartTime and maxStartTime")
            if input.max_start_time <= input.min_start_time:
                raise ValueError("maxStartTime must be after minStartTime")
            if input.status not in ("", "active", "canceled"):
                raise ValueError("status must be active, canceled, or blank")
            if input.sort not in ("", "start_time:asc", "start_time:desc"):
                raise ValueError("sort must be start_time:asc, start_time:desc, or blank")
            if input.invitee_email and not _email(input.invitee_email):
                raise ValueError("inviteeEmail must be one plain email address")
            _page(input.page_size, input.page_token)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", zero, Receipt(), failure("listScheduledEvents", FailureKind.VALIDATION, str(error))
            )
        credentials, kind, message = await self.client.resolve_credentials(call)
        attempt = _credential_query("listScheduledEvents", kind, message, zero)
        if attempt is not None:
            return attempt
        assert credentials is not None
        user_uri = input.user_uri
        organization_uri = input.organization_uri
        if not user_uri and not organization_uri:
            user_uri, _, current = await _current_user(self.client, call, credentials)
            attempt = _query_exchange(self.client, call, "listScheduledEvents", current, zero)
            if attempt is not None:
                return attempt
        query = {
            "min_start_time": _rfc3339(input.min_start_time),
            "max_start_time": _rfc3339(input.max_start_time),
            "sort": input.sort or "start_time:asc",
        }
        if user_uri:
            query["user"] = user_uri
        if organization_uri:
            query["organization"] = organization_uri
        if input.status:
            query["status"] = input.status
        if input.invitee_email:
            query["invitee_email"] = input.invitee_email
        if input.page_size:
            query["count"] = str(input.page_size)
        if input.page_token:
            query["page_token"] = input.page_token
        exchange = await self.client.exchange(call, credentials, "GET", "/scheduled_events", query)
        attempt = _query_exchange(self.client, call, "listScheduledEvents", exchange, zero)
        if attempt is not None:
            return attempt
        try:
            body = _body(exchange)
            events = tuple(_event(item) for item in _array(body, "collection"))
            output = ScheduledEventPage(
                events=events,
                next_page_token=_pagination(body),
                user_uri=user_uri,
                organization_uri=organization_uri,
                min_start_time=input.min_start_time.astimezone(UTC),
                max_start_time=input.max_start_time.astimezone(UTC),
            )
            return QueryAttempt.branch_result("listed", output, self.client.receipt(call))
        except (ValueError, TypeError, KeyError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call),
                failure(
                    "listScheduledEvents", FailureKind.PROTOCOL, "Calendly returned a malformed response"
                ),
            )


class GetScheduledEventOperation:
    """Read one Calendly scheduled event."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation contract."""
        return GET_SCHEDULED_EVENT_DEFINITION

    async def invoke(self, call: Call, input: GetScheduledEventInput) -> QueryAttempt[ScheduledEvent]:
        """Read the event and check its URI matches the request."""
        zero = ScheduledEvent()
        try:
            event_id = _resource_id(input.scheduled_event_uri, "scheduled_events", "scheduled event")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", zero, Receipt(), failure("getScheduledEvent", FailureKind.VALIDATION, str(error))
            )
        credentials, kind, message = await self.client.resolve_credentials(call)
        attempt = _credential_query("getScheduledEvent", kind, message, zero)
        if attempt is not None:
            return attempt
        assert credentials is not None
        exchange = await self.client.exchange(call, credentials, "GET", f"/scheduled_events/{event_id}")
        attempt = _query_exchange(
            self.client, call, "getScheduledEvent", exchange, zero, input.scheduled_event_uri, True
        )
        if attempt is not None:
            return attempt
        try:
            event = _event(_resource(exchange))
            if event.uri != input.scheduled_event_uri:
                raise ValueError("wrong event URI")
            return QueryAttempt.branch_result("found", event, self.client.receipt(call, event.uri))
        except (ValueError, TypeError, KeyError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call, input.scheduled_event_uri),
                failure("getScheduledEvent", FailureKind.PROTOCOL, "Calendly returned a malformed response"),
            )


class ListEventInviteesOperation:
    """List invitees of one scheduled event."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation contract."""
        return LIST_EVENT_INVITEES_DEFINITION

    async def invoke(self, call: Call, input: ListEventInviteesInput) -> QueryAttempt[EventInviteePage]:
        """Read and validate one invitee page."""
        zero = EventInviteePage()
        try:
            event_id = _resource_id(input.scheduled_event_uri, "scheduled_events", "scheduled event")
            if input.status not in ("", "active", "canceled"):
                raise ValueError("status must be active, canceled, or blank")
            if input.email and not _email(input.email):
                raise ValueError("email must be one plain email address")
            _page(input.page_size, input.page_token)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", zero, Receipt(), failure("listEventInvitees", FailureKind.VALIDATION, str(error))
            )
        credentials, kind, message = await self.client.resolve_credentials(call)
        attempt = _credential_query("listEventInvitees", kind, message, zero)
        if attempt is not None:
            return attempt
        assert credentials is not None
        query = {"sort": "created_at:asc"}
        if input.status:
            query["status"] = input.status
        if input.email:
            query["email"] = input.email
        if input.page_size:
            query["count"] = str(input.page_size)
        if input.page_token:
            query["page_token"] = input.page_token
        exchange = await self.client.exchange(
            call, credentials, "GET", f"/scheduled_events/{event_id}/invitees", query
        )
        attempt = _query_exchange(
            self.client, call, "listEventInvitees", exchange, zero, input.scheduled_event_uri, True
        )
        if attempt is not None:
            return attempt
        try:
            body = _body(exchange)
            invitees = tuple(_invitee(item, input.scheduled_event_uri) for item in _array(body, "collection"))
            output = EventInviteePage(input.scheduled_event_uri, invitees, _pagination(body))
            return QueryAttempt.branch_result(
                "listed", output, self.client.receipt(call, input.scheduled_event_uri)
            )
        except (ValueError, TypeError, KeyError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call, input.scheduled_event_uri),
                failure("listEventInvitees", FailureKind.PROTOCOL, "Calendly returned a malformed response"),
            )


class CancelScheduledEventOperation:
    """Cancel an event, reading back a 403 to recognize earlier cancellation."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation contract."""
        return CANCEL_SCHEDULED_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CancelScheduledEventInput) -> IdempotencyKey:
        """Use the stable Call ID for repeatable cancellation."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: CancelScheduledEventInput
    ) -> MutationAttempt[ScheduledEventCancellation]:
        """Send cancellation and classify its read-back when refused."""
        operation = "cancelScheduledEvent"
        output = ScheduledEventCancellation(scheduled_event_uri=input.scheduled_event_uri)
        try:
            event_id = _resource_id(input.scheduled_event_uri, "scheduled_events", "scheduled event")
            if len(input.reason) > 10000:
                raise ValueError("cancellation reason cannot exceed 10000 characters")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, kind, message = await self.client.resolve_credentials(call)
        attempted = _credential_mutation(operation, kind, message, output)
        if attempted is not None:
            return attempted
        assert credentials is not None
        exchange = await self.client.exchange(
            call,
            credentials,
            "POST",
            f"/scheduled_events/{event_id}/cancellation",
            payload={"reason": input.reason} if input.reason else {},
        )
        attempted = self._exchange_error(call, exchange, output)
        if attempted is not None:
            return attempted
        if exchange.status in (200, 201):
            try:
                cancellation = _cancellation(_resource(exchange))
                return MutationAttempt.branch_result(
                    "canceled",
                    replace(output, cancellation=cancellation),
                    self.client.receipt(call, input.scheduled_event_uri),
                )
            except (ValueError, TypeError, KeyError, OverflowError):
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    output,
                    self.client.receipt(call, input.scheduled_event_uri),
                    failure(operation, FailureKind.PROTOCOL, "Calendly returned a malformed response"),
                )
        if exchange.status == 403:
            return await self._read_back(call, credentials, event_id, output, exchange)
        if exchange.status == 404:
            return MutationAttempt.branch_result(
                "notFound",
                output,
                self.client.receipt(call, input.scheduled_event_uri),
                failure(operation, FailureKind.NOT_FOUND, "the Calendly scheduled event was not found"),
            )
        problem, delay, is_retry = self.client.classify(operation, exchange)
        return (
            MutationAttempt.retry(problem, delay)
            if is_retry
            else MutationAttempt.branch_result(
                "providerRejected", output, self.client.receipt(call, input.scheduled_event_uri), problem
            )
        )

    def _exchange_error(
        self, call: Call, exchange: Exchange, output: ScheduledEventCancellation
    ) -> MutationAttempt[Any] | None:
        if exchange.error == "defect":
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                failure(
                    "cancelScheduledEvent", FailureKind.LOCAL_DEFECT, "Calendly request could not be built"
                ),
            )
        if exchange.error == "tooLarge":
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                self.client.receipt(call, output.scheduled_event_uri),
                failure(
                    "cancelScheduledEvent",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Calendly response exceeds the configured maxResponseBytes",
                ),
            )
        if exchange.error:
            return MutationAttempt.retry(
                failure(
                    "cancelScheduledEvent",
                    FailureKind.TRANSPORT,
                    "Calendly's answer to the cancellation was lost; the retry reads the event back",
                )
            )
        return None

    async def _read_back(
        self,
        call: Call,
        credentials: Credentials,
        event_id: str,
        output: ScheduledEventCancellation,
        refusal: Exchange,
    ) -> MutationAttempt[ScheduledEventCancellation]:
        operation = "cancelScheduledEvent"
        response = await self.client.exchange(call, credentials, "GET", f"/scheduled_events/{event_id}")
        attempted = self._exchange_error(call, response, output)
        if attempted is not None:
            return attempted
        receipt = self.client.receipt(call, output.scheduled_event_uri)
        if response.status == 404:
            return MutationAttempt.branch_result(
                "notFound",
                output,
                receipt,
                failure(operation, FailureKind.NOT_FOUND, "the Calendly scheduled event was not found"),
            )
        if not 200 <= response.status < 300:
            problem, delay, is_retry = self.client.classify(operation, response)
            if is_retry:
                return MutationAttempt.retry(problem, delay)
            refused, _, _ = self.client.classify(operation, refusal)
            return MutationAttempt.branch_result("providerRejected", output, receipt, refused)
        try:
            event = _event(_resource(response))
            if event.uri != output.scheduled_event_uri:
                raise ValueError("wrong event URI")
        except (ValueError, TypeError, KeyError, OverflowError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Calendly returned a malformed response"),
            )
        if event.status == "canceled":
            return MutationAttempt.branch_result(
                "canceled",
                replace(output, already_canceled=True, cancellation=event.cancellation or Cancellation()),
                receipt,
            )
        message = (
            "Calendly refused to cancel the active event; it may have already started, "
            "or the token may not cancel it"
        )
        kind = FailureKind.AUTHORIZATION
        try:
            if _body(refusal).get("title") == "Insufficient scope":
                message = "the Calendly token lacks the scheduled_events:write scope needed to cancel events"
        except (ValueError, TypeError):
            pass  # The HTTP status still classifies an unreadable error body.
        if message.startswith("Calendly refused") and event.start_time <= self.client.now():
            kind = FailureKind.CONFLICT
            message = "Calendly cannot cancel an event that has already started"
        return MutationAttempt.branch_result(
            "providerRejected", output, receipt, failure(operation, kind, message)
        )


class CreateSchedulingLinkOperation:
    """Create a single-use link under Dex's durable dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation contract."""
        return CREATE_SCHEDULING_LINK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateSchedulingLinkInput) -> IdempotencyKey:
        """Correlate an unsafe write with its stable Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateSchedulingLinkInput) -> MutationAttempt[SchedulingLink]:
        """Send once and report unconfirmable acceptance as uncertain."""
        operation = "createSchedulingLink"
        output = SchedulingLink(event_type_uri=input.event_type_uri)
        try:
            fence = Fence(call, timedelta(seconds=30), write_window_fraction=5 / 6)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure(operation, FailureKind.LOCAL_DEFECT, str(error))
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "the scheduling link dispatch checkpoint cannot be trusted",
                ),
                self.client.receipt(call),
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            is_same_call = decision.marker is not None and decision.marker.call_id == call.id
            decision_kind = FailureKind.TRANSPORT if is_same_call else FailureKind.PROTOCOL
            message = (
                (
                    "a prior scheduling link request may have been accepted, "
                    "and Calendly offers no link read-back"
                )
                if is_same_call
                else "no confirmable prior scheduling link checkpoint exists"
            )
            return MutationAttempt.uncertain(
                output, failure(operation, decision_kind, message), self.client.receipt(call)
            )
        try:
            _resource_id(input.event_type_uri, "event_types", "event type")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, kind, message = await self.client.resolve_credentials(call)
        if kind is not None:
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Calendly credentials and a safe retry checkpoint are unavailable",
                    )
                )
            attempted = _credential_mutation(operation, kind, message, output)
            assert attempted is not None
            return attempted
        assert credentials is not None
        try:
            exchange = await fence.dispatch(
                lambda deadline: self.client.exchange(
                    call,
                    credentials,
                    "POST",
                    "/scheduling_links",
                    payload={"max_event_count": 1, "owner": input.event_type_uri, "owner_type": "EventType"},
                    allow_refresh=False,
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the scheduling link request was not sent before its safe deadline",
                )
            )
        except Exception:
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "Calendly's scheduling link answer was lost after the request may have been sent",
                ),
                self.client.receipt(call),
            )
        if exchange.error == "notSent":
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Calendly could not be reached and the retry checkpoint failed",
                    )
                )
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Calendly could not be reached before a connection was obtained",
                )
            )
        if exchange.error == "transport":
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "Calendly's scheduling link answer was lost after the request may have been sent",
                ),
                self.client.receipt(call),
            )
        if exchange.error == "tooLarge":
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Calendly's scheduling link response exceeds maxResponseBytes",
                ),
                self.client.receipt(call),
            )
        if exchange.error == "defect":
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                failure(operation, FailureKind.LOCAL_DEFECT, "Calendly request could not be built"),
            )
        if 200 <= exchange.status < 300:
            try:
                resource = _resource(exchange)
                booking_url = _string(resource, "booking_url")
                parsed = urlsplit(booking_url)
                if (
                    parsed.scheme != "https"
                    or not parsed.hostname
                    or parsed.username is not None
                    or parsed.password is not None
                    or (_string(resource, "owner") and _string(resource, "owner") != input.event_type_uri)
                ):
                    raise ValueError("invalid link")
                try:
                    await fence.record_provider_id(booking_url)
                except Exception:
                    pass  # The accepted provider result remains authoritative.
                link = SchedulingLink(booking_url, input.event_type_uri)
                return MutationAttempt.branch_result("created", link, self.client.receipt(call, booking_url))
            except (ValueError, TypeError, KeyError):
                return MutationAttempt.uncertain(
                    output,
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Calendly created a scheduling link but its answer is malformed",
                    ),
                    self.client.receipt(call),
                )
        problem, delay, _ = self.client.classify(operation, exchange)
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Calendly declined the request but the retry checkpoint failed",
                    )
                )
            return MutationAttempt.retry(problem, delay)
        if exchange.status == 409:
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.CONFLICT,
                    "Calendly reported a link conflict without a way to identify the existing link",
                ),
                self.client.receipt(call),
            )
        if exchange.status >= 500:
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Calendly's response does not prove whether a scheduling link was created",
                ),
                self.client.receipt(call),
            )
        return MutationAttempt.branch_result("providerRejected", output, self.client.receipt(call), problem)


class CreateWebhookSubscriptionOperation:
    """Reuse or create a Calendly webhook subscription by callback URL."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation contract."""
        return CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateWebhookSubscriptionInput) -> IdempotencyKey:
        """Correlate list-before-create retries with the Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: CreateWebhookSubscriptionInput
    ) -> MutationAttempt[WebhookSubscription]:
        """Scan subscriptions before POST and again after a conflict."""
        operation = "createWebhookSubscription"
        zero = WebhookSubscription()
        try:
            callback = urlsplit(input.callback_url)
            if (
                callback.scheme != "https"
                or not callback.hostname
                or callback.username is not None
                or callback.password is not None
                or callback.fragment
            ):
                raise ValueError(
                    "callbackUrl must be an absolute HTTPS URL without user information or a fragment"
                )
            scope = input.scope or "user"
            if scope not in ("user", "organization"):
                raise ValueError("scope must be user, organization, or blank")
            if scope == "organization" and input.user_uri:
                raise ValueError("userUri applies only to a user subscription")
            if input.user_uri:
                _resource_id(input.user_uri, "users", "user")
            if input.organization_uri:
                _resource_id(input.organization_uri, "organizations", "organization")
            events = input.events or ("invitee.created", "invitee.canceled")
            if len(set(events)) != len(events) or any(
                event not in ("invitee.created", "invitee.canceled") for event in events
            ):
                raise ValueError("events must be distinct values from invitee.created and invitee.canceled")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, kind, message = await self.client.resolve_credentials(call)
        attempted = _credential_mutation(operation, kind, message, zero)
        if attempted is not None:
            return attempted
        assert credentials is not None
        register_key = credentials.auth_method_id == "personal-access-token"
        if register_key and not credentials.webhook_signing_key.reveal():
            return MutationAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    (
                        "set webhook_signing_key on the Calendly connection before creating "
                        "a subscription, so deliveries can be verified"
                    ),
                ),
            )
        organization_uri, user_uri = input.organization_uri, input.user_uri
        if not organization_uri or (scope == "user" and not user_uri):
            current_user, current_organization, exchange = await _current_user(self.client, call, credentials)
            attempted = self._failed_exchange(call, exchange)
            if attempted is not None:
                return attempted
            organization_uri = organization_uri or current_organization
            if scope == "user":
                user_uri = user_uri or current_user
        request = (input.callback_url, scope, tuple(events), organization_uri, user_uri)
        attempted = await self._reuse(call, credentials, request)
        if attempted is not None:
            return attempted
        payload: dict[str, object] = {
            "url": input.callback_url,
            "events": events,
            "organization": organization_uri,
            "scope": scope,
        }
        if scope == "user":
            payload["user"] = user_uri
        if register_key:
            payload["signing_key"] = credentials.webhook_signing_key.reveal()
        exchange = await self.client.exchange(
            call, credentials, "POST", "/webhook_subscriptions", payload=payload
        )
        if exchange.status == 409 and not exchange.error:
            attempted = await self._reuse(call, credentials, request)
            if attempted is not None:
                return attempted
            return MutationAttempt.branch_result(
                "conflictingSubscription",
                zero,
                self.client.receipt(call),
                failure(
                    operation,
                    FailureKind.CONFLICT,
                    (
                        "Calendly already has a subscription for this callback URL in another scope "
                        "or for another user; delete it or use another URL"
                    ),
                ),
            )
        attempted = self._failed_exchange(call, exchange)
        if attempted is not None:
            return attempted
        try:
            subscription = _subscription(_resource(exchange))
            if subscription.callback_url != input.callback_url:
                raise ValueError("wrong callback")
            subscription = replace(subscription, was_created=True)
            return MutationAttempt.branch_result(
                "subscribed", subscription, self.client.receipt(call, subscription.uri)
            )
        except (ValueError, TypeError, KeyError, OverflowError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call),
                failure(operation, FailureKind.PROTOCOL, "Calendly returned a malformed response"),
            )

    async def _reuse(
        self, call: Call, credentials: Credentials, request: tuple[str, str, tuple[str, ...], str, str]
    ) -> MutationAttempt[WebhookSubscription] | None:
        callback, scope, events, organization_uri, user_uri = request
        query = {"organization": organization_uri, "scope": scope, "count": "100", "sort": "created_at:asc"}
        if scope == "user":
            query["user"] = user_uri
        for _ in range(10):
            exchange = await self.client.exchange(call, credentials, "GET", "/webhook_subscriptions", query)
            attempted = self._failed_exchange(call, exchange)
            if attempted is not None:
                return attempted
            try:
                body = _body(exchange)
                for value in _array(body, "collection"):
                    raw = _object(value)
                    if _string(raw, "callback_url") != callback:
                        continue
                    existing = _subscription(raw)
                    receipt = self.client.receipt(call, existing.uri)
                    if existing.state == "active" and all(event in existing.events for event in events):
                        return MutationAttempt.branch_result("subscribed", existing, receipt)
                    return MutationAttempt.branch_result(
                        "conflictingSubscription",
                        existing,
                        receipt,
                        failure(
                            "createWebhookSubscription",
                            FailureKind.CONFLICT,
                            (
                                "a Calendly subscription for this callback URL is disabled or lacks "
                                "a requested event; delete it and run again"
                            ),
                        ),
                    )
                next_token = _pagination(body)
                if not next_token:
                    return None
                query["page_token"] = next_token
            except (ValueError, TypeError, KeyError, OverflowError):
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    WebhookSubscription(),
                    self.client.receipt(call),
                    failure(
                        "createWebhookSubscription",
                        FailureKind.PROTOCOL,
                        "Calendly returned a malformed response",
                    ),
                )
        return MutationAttempt.branch_result(
            "invalidResponse",
            WebhookSubscription(),
            self.client.receipt(call),
            failure(
                "createWebhookSubscription",
                FailureKind.RESPONSE_TOO_LARGE,
                "the scope has more than 1000 webhook subscriptions",
            ),
        )

    def _failed_exchange(self, call: Call, exchange: Exchange) -> MutationAttempt[WebhookSubscription] | None:
        operation = "createWebhookSubscription"
        zero = WebhookSubscription()
        if exchange.error == "defect":
            return MutationAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                failure(operation, FailureKind.LOCAL_DEFECT, "Calendly request could not be built"),
            )
        if exchange.error in ("tooLarge", "malformed"):
            kind = FailureKind.RESPONSE_TOO_LARGE if exchange.error == "tooLarge" else FailureKind.PROTOCOL
            message = (
                "Calendly response exceeds the configured maxResponseBytes"
                if exchange.error == "tooLarge"
                else "Calendly returned a malformed response"
            )
            return MutationAttempt.branch_result(
                "invalidResponse", zero, self.client.receipt(call), failure(operation, kind, message)
            )
        if exchange.error:
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "Calendly's answer was lost; the retry lists subscriptions before creating one",
                )
            )
        if 200 <= exchange.status < 300:
            return None
        problem, delay, is_retry = self.client.classify(operation, exchange)
        return (
            MutationAttempt.retry(problem, delay)
            if is_retry
            else MutationAttempt.branch_result("providerRejected", zero, self.client.receipt(call), problem)
        )
