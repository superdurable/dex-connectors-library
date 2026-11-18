# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Graph transport, event decoding, and provider-backed operations."""

import ipaddress
import json
import re
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote, urlsplit
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    ReauthorizationRequiredError,
    Receipt,
    RejectedCredentialRefreshingProvider,
)
from dex_connectors.sdk.projectconfig.broker import ConnectionNotSelectedError, CredentialBrokerRejectedError
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError
from dex_connectors.sdk.providerhttp import (
    is_header_safe_credential,
    parse_retry_after,
    read_error_tokens,
    validate_base_url,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.config import Config, Credentials
from ._generated.definitions import (
    CREATE_EVENT_DEFINITION,
    GET_EVENT_DEFINITION,
    LIST_EVENTS_DEFINITION,
    QUERY_FREE_BUSY_DEFINITION,
    UPDATE_EVENT_DEFINITION,
)
from .models import (
    CreateEventInput,
    CreateEventOutput,
    Event,
    EventAttendee,
    EventAttendeeInput,
    EventDateTime,
    EventOnlineMeeting,
    GetEventInput,
    ListEventsInput,
    ListEventsOutput,
    QueryFreeBusyInput,
    QueryFreeBusyOutput,
    ScheduleAvailability,
    ScheduleItem,
    UpdateEventInput,
    UpdateEventOutput,
)

GRAPH_BASE = "https://graph.microsoft.com/v1.0"
PROVIDER = "outlook-calendar"
MARKER = "String {e8548d82-fea6-423a-bad8-7b6da468e968} Name DexIdempotencyKey"
EVENT_SELECT = (
    "id,subject,body,bodyPreview,start,end,originalStartTimeZone,originalEndTimeZone,isAllDay,"
    "isCancelled,isOrganizer,showAs,sensitivity,type,seriesMasterId,location,attendees,organizer,isOnlineMeeting,"
    "onlineMeetingProvider,onlineMeeting,webLink,iCalUId,changeKey,transactionId,responseStatus"
)
LIST_SELECT = EVENT_SELECT.replace("body,", "")
RFC3339 = re.compile(r"^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)([.,]\d+)?(Z|[+-]\d\d:\d\d)$")
GUID = re.compile(r"^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$")
DOMAIN = re.compile(r"^(?:(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)\.)+[A-Za-z]{2,63}$")
TRANSACTION = re.compile(r"^[A-Za-z0-9._-]{1,64}$")
REQUEST_ID = re.compile(r"^[A-Za-z0-9-]{1,64}$")
ERROR_CODE = re.compile(r"^[A-Za-z][A-Za-z0-9]{0,63}$")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, PROVIDER, operation, message)


def _is_bare_address(value: str) -> bool:
    local, separator, domain = value.partition("@")
    if not separator or not _is_go_dot_atom(local):
        return False
    if domain.startswith("[") and domain.endswith("]"):
        try:
            ipaddress.ip_address(domain[1:-1])
            return True
        except ValueError:
            return False
    return _is_go_dot_atom(domain)


def _is_go_dot_atom(value: str) -> bool:
    if not value or value.startswith(".") or value.endswith(".") or ".." in value:
        return False
    return all(
        character == "." or (
            (33 <= ord(character) <= 126 or ord(character) >= 128 and not 0xD800 <= ord(character) <= 0xDFFF)
            and character not in '()<>[]:;@\\,"'
        )
        for character in value
    )


def _instant(name: str, value: str) -> datetime:
    match = RFC3339.fullmatch(value)
    if match is None:
        raise ValueError(
            f"{name} {value!r} must be RFC 3339 with an explicit offset, such as "
            "2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z"
        )
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    if not 1 <= year <= 9999 or hour > 23 or minute > 59 or second > 59:
        raise ValueError(
            f"{name} {value!r} must be RFC 3339 with an explicit offset, such as "
            "2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z"
        )
    fraction = match.group(7)
    if fraction and any(character != "0" for character in fraction[7:]):
        raise ValueError(f"{name} has precision beyond Python's six fractional digits")
    microseconds = int((fraction[1:7] if fraction else "").ljust(6, "0"))
    try:
        candidate = datetime(year, month, day, hour, minute, second, microseconds)
        offset = match.group(8)
        if offset == "Z":
            return candidate.replace(tzinfo=UTC)
        offset_hour, offset_minute = int(offset[1:3]), int(offset[4:6])
        if offset_hour > 23 or offset_minute > 59:
            raise ValueError
        delta = timedelta(hours=offset_hour, minutes=offset_minute)
        if offset[0] == "-":
            delta = -delta
        return (candidate - delta).replace(tzinfo=UTC)
    except (OverflowError, ValueError) as error:
        raise ValueError(
            f"{name} {value!r} must be RFC 3339 with an explicit offset, such as "
            "2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z"
        ) from error


def _zone(name: str, value: str) -> ZoneInfo:
    if not value or value != value.strip() or value == "Local":
        raise ValueError(f"{name} {value!r} must be an IANA time zone name such as America/Los_Angeles")
    try:
        return ZoneInfo(value)
    except (ZoneInfoNotFoundError, ValueError) as error:
        raise ValueError(f"{name} {value!r} is not a known IANA time zone name") from error


def _display(value: str) -> tuple[ZoneInfo, str]:
    zone_name = value or "UTC"
    return _zone("timeZone", zone_name), zone_name


def _window(start: str, end: str) -> tuple[datetime, datetime]:
    if not start or not end:
        raise ValueError("timeMin and timeMax are both required")
    first, last = _instant("timeMin", start), _instant("timeMax", end)
    if last <= first:
        raise ValueError(f"timeMax {end} must be after timeMin {start}")
    return first, last


def _boundary(name: str, value: EventDateTime) -> datetime:
    instant = _instant(name + ".dateTime", value.date_time)
    if not value.time_zone:
        raise ValueError(f"{name}.timeZone is required; use an IANA name such as America/Los_Angeles")
    _zone(name + ".timeZone", value.time_zone)
    return instant


def _event_times(start: EventDateTime, end: EventDateTime, is_all_day: bool) -> None:
    first, last = _boundary("start", start), _boundary("end", end)
    if last <= first:
        raise ValueError("end must be after start")
    if is_all_day:
        if start.time_zone != end.time_zone:
            raise ValueError("an all-day event needs start and end in the same timeZone")
        location = _zone("start.timeZone", start.time_zone)
        for label, instant in (("start", first), ("end", last)):
            local = instant.astimezone(location)
            if local.hour or local.minute or local.second or local.microsecond:
                raise ValueError(
                    f"an all-day {label} must be midnight in {start.time_zone}, not {local.isoformat()}"
                )
            if local.replace(fold=1 - local.fold).utcoffset() != local.utcoffset():
                raise ValueError(f"an all-day {label} falls on an ambiguous midnight in {start.time_zone}")


def _graph_boundary(value: EventDateTime) -> dict[str, str]:
    instant = _instant("dateTime", value.date_time)
    location = _zone("timeZone", value.time_zone)
    wall = instant.astimezone(location)
    # An ambiguous wall time is sent as UTC so Graph cannot choose its other offset.
    alternate = wall.replace(fold=1 - wall.fold)
    if alternate.utcoffset() != wall.utcoffset() and alternate.replace(tzinfo=None) == wall.replace(
        tzinfo=None
    ):
        return _graph_utc(instant)
    return {"dateTime": wall.strftime("%Y-%m-%dT%H:%M:%S"), "timeZone": value.time_zone}


def _graph_utc(instant: datetime) -> dict[str, str]:
    return {"dateTime": instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S"), "timeZone": "UTC"}


def _returned_boundary(
    name: str, value: Any, display: tuple[ZoneInfo, str]
) -> tuple[EventDateTime, datetime]:
    if not isinstance(value, dict) or not value.get("dateTime"):
        raise ValueError(f"{name} is missing")
    zone_name = value.get("timeZone") or "UTC"
    try:
        location = _zone(name + ".timeZone", zone_name)
    except ValueError as error:
        raise ValueError(f"{name}: returned time zone is neither UTC nor an IANA name") from error
    wall_text = value["dateTime"]
    if not isinstance(wall_text, str) or not re.fullmatch(
        r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:[.,]\d+)?", wall_text
    ):
        raise ValueError(f"{name}.dateTime is not a Graph wall time")
    try:
        fraction = wall_text[19:]
        if fraction and any(character != "0" for character in fraction[7:]):
            raise ValueError(f"{name}.dateTime has precision beyond Python's six fractional digits")
        instant = datetime.fromisoformat(wall_text.replace(",", ".")).replace(tzinfo=location).astimezone(UTC)
    except ValueError as error:
        raise ValueError(f"{name}.dateTime is not a Graph wall time") from error
    local = instant.astimezone(display[0]).isoformat(timespec="seconds").replace("+00:00", "Z")
    return EventDateTime(local, display[1]), instant


def _event(resource: Any, calendar_id: str, display: tuple[ZoneInfo, str]) -> Event:
    if not isinstance(resource, dict) or not isinstance(resource.get("id"), str) or not resource["id"]:
        raise ValueError("event response has no ID")
    event_id = resource["id"]
    try:
        start, first = _returned_boundary("start", resource.get("start"), display)
        end, last = _returned_boundary("end", resource.get("end"), display)
    except ValueError as error:
        raise ValueError(f"event {event_id}: {error}") from error
    if last < first:
        raise ValueError(f"event {event_id} ends before it starts")
    attendees = tuple(
        EventAttendee(
            email=item["emailAddress"].get("address", ""),
            name=item["emailAddress"].get("name", ""),
            type=item.get("type", ""),
            response_status=(item.get("status") or {}).get("response", ""),
        )
        for item in resource.get("attendees", []) or []
        if isinstance(item, dict) and isinstance(item.get("emailAddress"), dict)
    )
    online = None
    if resource.get("isOnlineMeeting") or resource.get("onlineMeeting") is not None:
        online = EventOnlineMeeting(
            resource.get("onlineMeetingProvider", ""),
            (resource.get("onlineMeeting") or {}).get("joinUrl", ""),
        )
    return Event(
        id=event_id,
        calendar_id=calendar_id,
        subject=resource.get("subject", "") or "",
        body=(resource.get("body") or {}).get("content", ""),
        body_preview=resource.get("bodyPreview", "") or "",
        location=(resource.get("location") or {}).get("displayName", ""),
        start=start,
        end=end,
        original_start_time_zone=resource.get("originalStartTimeZone", "") or "",
        original_end_time_zone=resource.get("originalEndTimeZone", "") or "",
        is_all_day=resource.get("isAllDay", False),
        is_cancelled=resource.get("isCancelled", False),
        is_organizer=resource.get("isOrganizer", False),
        show_as=resource.get("showAs") or "unknown",
        sensitivity=resource.get("sensitivity", "") or "",
        type=resource.get("type", "") or "",
        series_master_id=resource.get("seriesMasterId", "") or "",
        response_status=(resource.get("responseStatus") or {}).get("response", ""),
        attendees=attendees,
        organizer_email=((resource.get("organizer") or {}).get("emailAddress") or {}).get("address", ""),
        online_meeting=online,
        web_link=resource.get("webLink", "") or "",
        i_cal_u_id=resource.get("iCalUId", "") or "",
        change_key=resource.get("changeKey", "") or "",
        etag=resource.get("@odata.etag", "") or "",
        transaction_id=resource.get("transactionId", "") or "",
    )


def _query(values: dict[str, str]) -> str:
    return "&".join(
        f"{quote(key, safe='-._~')}={quote(values[key], safe='-._~')}"
        for key in sorted(values, key=lambda item: item.encode())
    )


def _path_escape(value: str) -> str:
    return quote(value, safe="-._~$&+=:@")


def _classification(status: int, body: bytes, headers: httpx.Headers) -> tuple[bool, FailureKind, str, float]:
    tokens = read_error_tokens(body, ("/error/code",))
    code = tokens[0] if tokens and ERROR_CODE.fullmatch(tokens[0]) else ""
    suffix = f" ({code})" if code else ""
    delay = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
    if status == 429:
        return True, FailureKind.RATE_LIMIT, "provider throttled the request" + suffix, delay
    if status in (408, 500, 502, 503, 504):
        return True, FailureKind.AVAILABILITY, "provider is temporarily unavailable" + suffix, delay
    kind = {
        400: FailureKind.VALIDATION,
        422: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        410: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        412: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)
    if code in ("quotaLimitReached", "ErrorQuotaExceeded"):
        kind = FailureKind.QUOTA_EXHAUSTED
    return False, kind, f"provider rejected the request with HTTP {status}" + suffix, 0


@dataclass(frozen=True, slots=True)
class GraphResponse:
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""


# Only these credential failures need a person; every other one retries.
_TERMINAL_CREDENTIAL_ERRORS = (
    ReauthorizationRequiredError,
    ObjectNotFoundError,
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
)


class RequestFailure(Exception):
    def __init__(self, kind: FailureKind, message: str, response: GraphResponse | None = None) -> None:
        self.kind, self.response = kind, response or GraphResponse()
        super().__init__(message)


class Client:
    """Authenticated Graph client with host-managed generated OAuth grants."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        local_provider_url: str = "",
        now: Any = None,
    ) -> None:
        if credentials is None:
            raise ValueError("Outlook Calendar credential provider is required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Outlook Calendar response limit must be positive")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or httpx.AsyncClient(timeout=20, follow_redirects=False)
        self.owns_http_client = http_client is None
        validated_local_url = ""
        if local_provider_url:
            validated_local_url = validate_base_url(local_provider_url)
            parsed_local = urlsplit(validated_local_url)
            try:
                is_loopback = parsed_local.hostname == "localhost" or ipaddress.ip_address(
                    parsed_local.hostname or ""
                ).is_loopback
            except ValueError:
                is_loopback = parsed_local.hostname == "localhost"
            if (
                not is_loopback
                or parsed_local.scheme not in ("http", "https")
                or not parsed_local.netloc
                or parsed_local.username is not None
                or parsed_local.path not in ("", "/")
                or parsed_local.query
                or parsed_local.fragment
            ):
                raise ValueError(
                    "Outlook Calendar local provider URL must be a loopback base URL without a path"
                )
        self.base_url = validated_local_url + "/v1.0" if local_provider_url else GRAPH_BASE
        self.now = now or (lambda: datetime.now(UTC))
        tenant = config.tenant_id.strip()
        if tenant and not (GUID.fullmatch(tenant) or (len(tenant) <= 253 and DOMAIN.fullmatch(tenant))):
            raise ValueError(
                "tenantId must be a directory (tenant) ID GUID or a verified domain "
                "such as contoso.onmicrosoft.com"
            )
        mailbox = config.mailbox.strip()
        if mailbox and not (GUID.fullmatch(mailbox) or _is_bare_address(mailbox)):
            raise ValueError(
                "mailbox must be a bare address such as scheduling@contoso.com or a user object ID GUID"
            )
        self.mailbox = mailbox

    async def aclose(self) -> None:
        """Close an HTTP client created by this connector."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def list_events(self) -> "ListEventsOperation":
        """Bind the listEvents query."""
        return ListEventsOperation(self)

    def get_event(self) -> "GetEventOperation":
        """Bind the getEvent query."""
        return GetEventOperation(self)

    def create_event(self) -> "CreateEventOperation":
        """Bind the createEvent mutation."""
        return CreateEventOperation(self)

    def update_event(self) -> "UpdateEventOperation":
        """Bind the updateEvent mutation."""
        return UpdateEventOperation(self)

    def query_free_busy(self) -> "QueryFreeBusyOperation":
        """Bind the queryFreeBusy query."""
        return QueryFreeBusyOperation(self)

    async def resolve_credentials(self, call: Call, operation: str) -> tuple[Credentials, str]:
        """Resolve a current credential and the selected mailbox path.

        A credential failure that is not terminal raises an AVAILABILITY RequestFailure, which the
        caller retries.
        """
        try:
            material = await self.credentials.resolve(call)
        except _TERMINAL_CREDENTIAL_ERRORS as error:
            raise RequestFailure(
                FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            ) from error
        except Exception as error:
            # The generated connection module imports this one, so import its predicate lazily.
            from ._generated.connection import is_credential_material_error

            if is_credential_material_error(error):
                raise RequestFailure(
                    FailureKind.AUTHENTICATION, "connection credentials are unavailable"
                ) from error
            raise RequestFailure(
                FailureKind.AVAILABILITY,
                "connection credentials are temporarily unavailable; nothing was sent",
            ) from error
        try:
            token = material.access_token.reveal() if material.access_token is not None else ""
            if not is_header_safe_credential(token) or material.auth_method_id not in (
                "microsoft-oauth",
                "app-only",
            ):
                raise ValueError
        except Exception as error:
            raise RequestFailure(
                FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            ) from error
        if material.auth_method_id == "microsoft-oauth":
            return material, "/me"
        if not self.mailbox:
            raise RequestFailure(
                FailureKind.VALIDATION, "an app-only connection needs its mailbox configured"
            )
        return material, "/users/" + _path_escape(self.mailbox)

    async def send(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: Any = None,
        if_match: str = "",
        next_link: str = "",
    ) -> GraphResponse:
        """Send one bounded Graph request, retrying one rejected credential once."""
        target = self.base_url + path + (("?" + _query(query)) if query else "")
        if next_link:
            target = (
                next_link.replace(GRAPH_BASE, self.base_url, 1) if self.base_url != GRAPH_BASE else next_link
            )
        try:
            encoded = (
                json.dumps(payload, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()
                if payload is not None
                else None
            )
        except (TypeError, ValueError) as error:
            raise RequestFailure(FailureKind.LOCAL_DEFECT, "provider request could not be encoded") from error
        for attempt in range(2):
            token = credentials.access_token.reveal() if credentials.access_token is not None else ""
            headers = [
                ("Authorization", "Bearer " + token),
                ("Accept", "application/json"),
                ("Prefer", 'outlook.timezone="UTC"'),
                ("Prefer", 'outlook.body-content-type="text"'),
                ("client-request-id", str(call.id)),
            ]
            if encoded is not None:
                headers.append(("Content-Type", "application/json"))
            if if_match:
                headers.append(("If-Match", if_match))
            try:
                async with self.http_client.stream(
                    method, target, headers=headers, content=encoded, timeout=20, follow_redirects=False
                ) as response:
                    metadata = GraphResponse(response.status_code, response.headers.copy())
                    body = b""
                    async for chunk in response.aiter_bytes():
                        body += chunk
                        if len(body) > self.config.max_response_bytes:
                            raise RequestFailure(
                                FailureKind.RESPONSE_TOO_LARGE,
                                "provider response exceeds configured limit",
                                metadata,
                            )
                    result = GraphResponse(response.status_code, response.headers.copy(), body)
            except RequestFailure:
                raise
            except (httpx.HTTPError, OSError) as error:
                raise RequestFailure(FailureKind.TRANSPORT, "provider request failed") from error
            if result.status != 401 or attempt:
                return result
            if not isinstance(self.credentials, RejectedCredentialRefreshingProvider):
                return result
            try:
                credentials = await self.credentials.resolve_after_rejection(call, None)  # type: ignore[arg-type]
                token = credentials.access_token.reveal() if credentials.access_token is not None else ""
                if not is_header_safe_credential(token):
                    return result
            except Exception:
                return result
        raise RequestFailure(FailureKind.LOCAL_DEFECT, "authenticated request retry was exhausted")

    def receipt(self, call: Call, response: GraphResponse, object_id: str = "") -> Receipt:
        """Build a credential-safe provider receipt."""
        request_id = response.headers.get("request-id", "")
        if not REQUEST_ID.fullmatch(request_id):
            request_id = ""
        return Receipt(call.id, PROVIDER, self.now(), call.idempotency_key, object_id, request_id)


def _calendar_path(root: str, calendar_id: str) -> str:
    return root + ("/calendars/" + _path_escape(calendar_id) if calendar_id else "/calendar")


def _page_link(value: str) -> None:
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or parsed.netloc != "graph.microsoft.com"
        or parsed.fragment
        or not parsed.path.startswith("/v1.0/")
        or not parsed.path.lower().endswith("/calendarview")
    ):
        raise ValueError(
            "pageToken must be the nextPageToken of a previous page, an https://graph.microsoft.com URL"
        )


def _response_json(response: GraphResponse) -> Any:
    return json.loads(response.body, parse_constant=lambda value: (_ for _ in ()).throw(ValueError(value)))


def _request_error_query(
    operation: str, failure: RequestFailure, empty: Any, receipt: Receipt
) -> QueryAttempt[Any]:
    if failure.kind == FailureKind.RESPONSE_TOO_LARGE:
        return QueryAttempt.branch_result(
            "invalidResponse", empty, receipt, _failure(operation, failure.kind, str(failure))
        )
    if failure.kind == FailureKind.LOCAL_DEFECT:
        return QueryAttempt.branch_result(
            "defect", empty, Receipt(), _failure(operation, failure.kind, str(failure))
        )
    return QueryAttempt.retry(_failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"))


def _read_classification(
    operation: str, response: GraphResponse, event: Event | None, error: str = ""
) -> tuple[str, Failure | None, float]:
    if response.status in (404, 410):
        return "notFound", _failure(operation, FailureKind.NOT_FOUND, "event was not found"), 0
    if not 200 <= response.status < 300:
        retry, kind, message, delay = _classification(response.status, response.body, response.headers)
        return ("retry" if retry else "providerRejected"), _failure(operation, kind, message), delay
    if error:
        return (
            "invalidResponse",
            _failure(operation, FailureKind.PROTOCOL, "provider returned an invalid event: " + error),
            0,
        )
    if event is None:
        return (
            "invalidResponse",
            _failure(operation, FailureKind.PROTOCOL, "provider returned an invalid event"),
            0,
        )
    return "found", None, 0


async def _read_event(
    client: Client,
    call: Call,
    credentials: Credentials,
    operation: str,
    root: str,
    event_id: str,
    display: tuple[ZoneInfo, str],
) -> tuple[str, Event, GraphResponse, Failure | None, float]:
    try:
        response = await client.send(
            call,
            credentials,
            "GET",
            root + "/events/" + _path_escape(event_id),
            {"$select": EVENT_SELECT},
        )
    except RequestFailure as request_error:
        response = request_error.response
        if request_error.kind == FailureKind.RESPONSE_TOO_LARGE:
            return (
                "invalidResponse",
                Event(),
                response,
                _failure(operation, request_error.kind, str(request_error)),
                0,
            )
        if request_error.kind == FailureKind.LOCAL_DEFECT:
            return "defect", Event(), response, _failure(operation, request_error.kind, str(request_error)), 0
        return (
            "retry",
            Event(),
            response,
            _failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"),
            0,
        )
    if not 200 <= response.status < 300:
        kind, failure, delay = _read_classification(operation, response, None)
        return kind, Event(), response, failure, delay
    try:
        event = _event(_response_json(response), "", display)
        return "found", event, response, None, 0
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        kind, failure, delay = _read_classification(operation, response, None, str(error))
        return kind, Event(), response, failure, delay


async def _lookup(
    client: Client,
    call: Call,
    credentials: Credentials,
    operation: str,
    root: str,
    key: str,
    calendar_id: str,
    display: tuple[ZoneInfo, str],
) -> tuple[str, Event, tuple[str, ...], GraphResponse, Failure | None, float]:
    filter_value = f"singleValueExtendedProperties/Any(ep: ep/id eq '{MARKER}' and ep/value eq '{key}')"
    try:
        response = await client.send(
            call,
            credentials,
            "GET",
            root + "/events",
            {"$filter": filter_value, "$select": EVENT_SELECT, "$top": "10"},
        )
    except RequestFailure as request_error:
        response = request_error.response
        if request_error.kind == FailureKind.RESPONSE_TOO_LARGE:
            return (
                "invalidResponse",
                Event(),
                (),
                response,
                _failure(operation, request_error.kind, str(request_error)),
                0,
            )
        if request_error.kind == FailureKind.LOCAL_DEFECT:
            return (
                "defect",
                Event(),
                (),
                response,
                _failure(operation, request_error.kind, str(request_error)),
                0,
            )
        return (
            "retry",
            Event(),
            (),
            response,
            _failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"),
            0,
        )
    if not 200 <= response.status < 300:
        kind, failure, delay = _read_classification(operation, response, None)
        return kind, Event(), (), response, failure, delay
    try:
        value = _response_json(response)["value"]
        if not isinstance(value, list):
            raise ValueError
        if not value:
            return (
                "missing",
                Event(),
                (),
                response,
                _failure(operation, FailureKind.NOT_FOUND, "no event carries the idempotency key"),
                0,
            )
        converted = tuple(_event(item, calendar_id, display) for item in value)
        return "found", converted[0], tuple(item.id for item in converted[1:]), response, None, 0
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        return (
            "invalidResponse",
            Event(),
            (),
            response,
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "provider returned an invalid event lookup"
                if not str(error)
                else "provider returned an invalid event: " + str(error),
            ),
            0,
        )


class ListEventsOperation:
    """List one bounded calendar view page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest operation definition."""
        return LIST_EVENTS_DEFINITION

    async def invoke(self, call: Call, input: ListEventsInput) -> QueryAttempt[ListEventsOutput]:
        """Read and decode one Graph calendarView page."""
        operation = "listEvents"
        empty = ListEventsOutput()
        try:
            if input.calendar_id != input.calendar_id.strip():
                raise ValueError(
                    "calendarId cannot have surrounding whitespace; leave it blank "
                    "for the mailbox's default calendar"
                )
            first, last = _window(input.time_min, input.time_max)
            if last - first > timedelta(days=366):
                raise ValueError("the timeMin to timeMax window cannot exceed 366 days")
            display = _display(input.time_zone)
            if input.page_token:
                _page_link(input.page_token)
            if type(input.page_size) is not int or input.page_size < 0 or input.page_size > 250:
                raise ValueError("pageSize must be between 1 and 250, or zero for the default")
            credentials, root = await self.client.resolve_credentials(call, operation)
        except RequestFailure as error:
            if error.kind is FailureKind.AVAILABILITY:
                return QueryAttempt.retry(_failure(operation, error.kind, str(error)))
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, error.kind, str(error))
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, FailureKind.VALIDATION, str(error))
            )
        query = (
            None
            if input.page_token
            else {
                "startDateTime": input.time_min,
                "endDateTime": input.time_max,
                "$top": str(input.page_size or 50),
                "$select": LIST_SELECT,
            }
        )
        try:
            response = await self.client.send(
                call,
                credentials,
                "GET",
                _calendar_path(root, input.calendar_id) + "/calendarView",
                query,
                next_link=input.page_token,
            )
        except RequestFailure as error:
            return _request_error_query(operation, error, empty, self.client.receipt(call, error.response))
        receipt = self.client.receipt(call, response)
        if response.status in (404, 410):
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                receipt,
                _failure(operation, FailureKind.NOT_FOUND, "calendar was not found"),
            )
        if not 200 <= response.status < 300:
            retry, kind, message, delay = _classification(response.status, response.body, response.headers)
            failure = _failure(operation, kind, message)
            return (
                QueryAttempt.retry(failure, delay)
                if retry
                else QueryAttempt.branch_result("providerRejected", empty, receipt, failure)
            )
        try:
            page = _response_json(response)
            if not isinstance(page, dict) or not isinstance(page.get("value"), list):
                raise ValueError("event page is not a JSON object with a value array")
            next_link = page.get("@odata.nextLink", "") or ""
            if next_link:
                _page_link(next_link)
            converted = tuple(_event(item, input.calendar_id, display) for item in page["value"])
            converted = tuple(sorted(converted, key=lambda item: _instant("dateTime", item.start.date_time)))
            output = ListEventsOutput(
                input.calendar_id, input.time_min, input.time_max, display[1], converted, next_link
            )
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                _failure(
                    operation, FailureKind.PROTOCOL, "provider returned an invalid event page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("listed", output, receipt)


class GetEventOperation:
    """Read one event by Graph ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest operation definition."""
        return GET_EVENT_DEFINITION

    async def invoke(self, call: Call, input: GetEventInput) -> QueryAttempt[Event]:
        """Read one event and route its definite outcome."""
        operation = "getEvent"
        try:
            if not input.event_id.strip() or input.event_id != input.event_id.strip():
                raise ValueError("eventId is required")
            display = _display(input.time_zone)
            credentials, root = await self.client.resolve_credentials(call, operation)
        except RequestFailure as error:
            if error.kind is FailureKind.AVAILABILITY:
                return QueryAttempt.retry(_failure(operation, error.kind, str(error)))
            return QueryAttempt.branch_result(
                "defect", Event(), Receipt(), _failure(operation, error.kind, str(error))
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", Event(), Receipt(), _failure(operation, FailureKind.VALIDATION, str(error))
            )
        outcome, event, response, failure, delay = await _read_event(
            self.client, call, credentials, operation, root, input.event_id, display
        )
        if outcome == "retry":
            return QueryAttempt.retry(
                failure or _failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"), delay
            )
        return QueryAttempt.branch_result(
            outcome,
            event if outcome == "found" else Event(),
            self.client.receipt(call, response, event.id) if outcome != "defect" else Receipt(),
            failure,
        )


def _validate_schedules(input: QueryFreeBusyInput) -> tuple[datetime, datetime, int, tuple[ZoneInfo, str]]:
    if not 1 <= len(input.schedules) <= 20:
        raise ValueError("schedules must list 1 to 20 addresses")
    seen: set[str] = set()
    for schedule in input.schedules:
        if not _is_bare_address(schedule):
            raise ValueError(f"schedule {schedule!r} must be one bare address such as person@example.com")
        if schedule.lower() in seen:
            raise ValueError(f"schedule {schedule!r} is duplicated")
        seen.add(schedule.lower())
    first, last = _window(input.time_min, input.time_max)
    if last - first >= timedelta(days=62):
        raise ValueError("the timeMin to timeMax window must be shorter than 62 days")
    interval = input.availability_view_interval or 30
    if type(interval) is not int or not 5 <= interval <= 1440:
        raise ValueError(
            "availabilityViewInterval must be between 5 and 1440 minutes, or zero for the default"
        )
    return first, last, interval, _display(input.time_zone)


def _availability(schedule_id: str, resource: Any, display: tuple[ZoneInfo, str]) -> ScheduleAvailability:
    items: list[ScheduleItem] = []
    busy, known, reason = False, True, ""
    error = resource.get("error")
    for item in resource.get("scheduleItems", []) or []:
        try:
            start, first = _returned_boundary("scheduleItem.start", item.get("start"), display)
            end, last = _returned_boundary("scheduleItem.end", item.get("end"), display)
            if last < first:
                raise ValueError
        except (ValueError, TypeError, AttributeError):
            if error is not None:
                continue
            raise ValueError(f"schedule {schedule_id!r} has an item without two ordered times") from None
        status = item.get("status") or "unknown"
        items.append(ScheduleItem(start.date_time, end.date_time, status))
        if status in ("tentative", "busy", "oof"):
            busy = True
        elif status not in ("free", "workingElsewhere") and known:
            known, reason = False, "unknownStatus"
    view = resource.get("availabilityView", "") or ""
    if any(character in "123" for character in view):
        busy = True
    if not view or any(character not in "01234" for character in view):
        if known:
            known, reason = False, "unreadableAvailabilityView"
    code = ""
    if error is not None:
        known, reason = False, "error"
        candidate = error.get("responseCode", "") if isinstance(error, dict) else ""
        if isinstance(candidate, str) and ERROR_CODE.fullmatch(candidate):
            code = candidate
    return ScheduleAvailability(schedule_id, busy, known, reason, code, tuple(items), view)


class QueryFreeBusyOperation:
    """Read bounded free and busy schedules."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest operation definition."""
        return QUERY_FREE_BUSY_DEFINITION

    async def invoke(self, call: Call, input: QueryFreeBusyInput) -> QueryAttempt[QueryFreeBusyOutput]:
        """Call Graph getSchedule and preserve unknown availability."""
        operation, empty = "queryFreeBusy", QueryFreeBusyOutput()
        try:
            first, last, interval, display = _validate_schedules(input)
            credentials, root = await self.client.resolve_credentials(call, operation)
        except RequestFailure as error:
            if error.kind is FailureKind.AVAILABILITY:
                return QueryAttempt.retry(_failure(operation, error.kind, str(error)))
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, error.kind, str(error))
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, FailureKind.VALIDATION, str(error))
            )
        payload = {
            "schedules": input.schedules,
            "startTime": _graph_utc(first),
            "endTime": _graph_utc(last),
            "availabilityViewInterval": interval,
        }
        try:
            response = await self.client.send(
                call, credentials, "POST", root + "/calendar/getSchedule", payload=payload
            )
        except RequestFailure as error:
            return _request_error_query(operation, error, empty, self.client.receipt(call, error.response))
        receipt = self.client.receipt(call, response)
        if not 200 <= response.status < 300:
            retry, kind, message, delay = _classification(response.status, response.body, response.headers)
            failure = _failure(operation, kind, message)
            return (
                QueryAttempt.retry(failure, delay)
                if retry
                else QueryAttempt.branch_result("providerRejected", empty, receipt, failure)
            )
        try:
            result = _response_json(response)
            if not isinstance(result, dict) or not isinstance(result.get("value"), list):
                raise ValueError("schedule response is not a JSON object with a value array")
            returned = {item.get("scheduleId", "").lower(): item for item in result["value"]}
            schedules = tuple(
                _availability(item, returned[item.lower()], display)
                if item.lower() in returned
                else ScheduleAvailability(item, False, False, "missing", "", (), "")
                for item in input.schedules
            )
            output = QueryFreeBusyOutput(input.time_min, input.time_max, display[1], interval, schedules)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                _failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid schedule response: " + str(error),
                ),
            )
        if any(not item.is_availability_known for item in output.schedules):
            return QueryAttempt.branch_result(
                "incomplete",
                output,
                receipt,
                _failure(
                    operation,
                    FailureKind.PROVIDER_REJECTION,
                    "provider could not report availability for every schedule",
                ),
            )
        return QueryAttempt.branch_result("queried", output, receipt)


def _attendees(attendees: tuple[EventAttendeeInput, ...]) -> list[dict[str, Any]]:
    return [
        {"emailAddress": {"address": item.email}, "type": "optional" if item.is_optional else "required"}
        for item in attendees
    ]


def _validate_attendees(attendees: tuple[EventAttendeeInput, ...]) -> None:
    seen: set[str] = set()
    for item in attendees:
        if not _is_bare_address(item.email):
            raise ValueError(
                f"attendee email {item.email!r} must be one bare address such as person@example.com"
            )
        if item.email.lower() in seen:
            raise ValueError(f"attendee email {item.email!r} is duplicated")
        seen.add(item.email.lower())


def _coerce_boundary(value: Any) -> EventDateTime:
    if isinstance(value, EventDateTime):
        return value
    if isinstance(value, dict):
        return EventDateTime(value.get("dateTime", ""), value.get("timeZone", ""))
    raise ValueError("event boundary is invalid")


def _coerce_attendees(value: Any) -> tuple[EventAttendeeInput, ...]:
    if value is None:
        return ()
    if not isinstance(value, (tuple, list)):
        raise ValueError("attendees must be a list")
    return tuple(
        item
        if isinstance(item, EventAttendeeInput)
        else EventAttendeeInput(item["email"], item.get("isOptional", False))
        for item in value
    )


def _create_payload(input: CreateEventInput, key: str) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "subject": input.subject,
        "start": _graph_boundary(input.start),
        "end": _graph_boundary(input.end),
        "transactionId": key,
        "singleValueExtendedProperties": [{"id": MARKER, "value": key}],
    }
    if input.is_all_day:
        payload["isAllDay"] = True
    if input.body:
        payload["body"] = {"contentType": "text", "content": input.body}
    if input.location:
        payload["location"] = {"displayName": input.location}
    if input.attendees:
        payload["attendees"] = _attendees(input.attendees)
    if input.request_online_meeting:
        payload["isOnlineMeeting"] = True
        payload["onlineMeetingProvider"] = "teamsForBusiness"
    return payload


class CreateEventOperation:
    """Idempotently create one Graph event."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest operation definition."""
        return CREATE_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateEventInput) -> IdempotencyKey:
        """Use a caller transaction ID or the stable Dex Call ID."""
        return IdempotencyKey(input.transaction_id or call_id)

    async def invoke(self, call: Call, input: CreateEventInput) -> MutationAttempt[CreateEventOutput]:
        """Read by marker before POST and after ambiguous rejection."""
        operation, empty = "createEvent", CreateEventOutput()
        key = str(call.idempotency_key)
        try:
            input = replace(
                input,
                start=_coerce_boundary(input.start),
                end=_coerce_boundary(input.end),
                attendees=_coerce_attendees(input.attendees),
            )
            if input.calendar_id != input.calendar_id.strip():
                raise ValueError(
                    "calendarId cannot have surrounding whitespace; leave it blank "
                    "for the mailbox's default calendar"
                )
            if not TRANSACTION.fullmatch(key):
                raise ValueError(
                    "transactionId must be 1 to 64 letters, digits, dots, hyphens, or underscores"
                )
            if not input.subject.strip():
                raise ValueError("subject is required")
            _event_times(input.start, input.end, input.is_all_day)
            _validate_attendees(input.attendees)
            display = _display(input.start.time_zone)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            fence = Fence(call, timedelta(seconds=30), max_sends=call.max_sends)
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                _failure(operation, FailureKind.LOCAL_DEFECT, "createEvent dispatch fence is unavailable"),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            # An unreadable checkpoint is an unknown outcome; the retry budget bounds these attempts.
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "an earlier attempt may have created the event, and its dispatch checkpoint is "
                    "unreadable",
                )
            )
        provider_id = decision.marker.provider_id if decision.marker is not None else ""
        attempt = _CreateEventAttempt(
            self.client,
            call,
            fence,
            input,
            key,
            display,
            has_possible_earlier_send=(
                decision.kind != DecisionKind.SEND_ALLOWED
                or decision.prior_send_upper_bound > 0
                or bool(provider_id)
            ),
            can_resend_unknown_outcome=decision.prior_send_upper_bound + 1 < call.max_sends,
            is_send_forbidden=decision.kind == DecisionKind.READ_BACK_REQUIRED or bool(provider_id),
        )
        return await attempt.run()


# A 2xx whose event ID could not be read is recorded so that no later attempt POSTs again.
_ACCEPTED_EVENT_PROVIDER_ID = "accepted"
_DEFINITE_REFUSAL_STATUSES = frozenset((400, 401, 403, 404, 411, 413, 414, 415, 422))


def _is_definite_graph_refusal(response: GraphResponse) -> bool:
    """Report an allowlisted refusal status whose body carries no created item ID."""
    if response.status not in _DEFINITE_REFUSAL_STATUSES:
        return False
    try:
        document = json.loads(response.body)
    except (ValueError, UnicodeDecodeError):
        return True
    return not (isinstance(document, dict) and document.get("id") not in (None, ""))


class _CreateEventAttempt:
    """One createEvent attempt's Fence decision and resolved connection."""

    def __init__(
        self,
        client: Client,
        call: Call,
        fence: Fence,
        input: CreateEventInput,
        key: str,
        display: tuple[ZoneInfo, str],
        *,
        has_possible_earlier_send: bool,
        can_resend_unknown_outcome: bool,
        is_send_forbidden: bool,
    ) -> None:
        self.client, self.call, self.fence, self.input, self.key, self.display = (
            client, call, fence, input, key, display,
        )
        self.has_possible_earlier_send = has_possible_earlier_send
        self.can_resend_unknown_outcome = can_resend_unknown_outcome
        self.is_send_forbidden = is_send_forbidden
        self.credentials: Credentials | None = None
        self.root = ""

    async def run(self) -> MutationAttempt[CreateEventOutput]:
        """Resolve credentials, read the marker back, and POST only when the cap allows."""
        try:
            self.credentials, self.root = await self.client.resolve_credentials(self.call, "createEvent")
        except RequestFailure as error:
            return await self._credential_failure(error)
        decided = await self._look_up()
        if decided is not None:
            return decided
        if self.is_send_forbidden:
            return self._uncertain(
                _failure(
                    "createEvent",
                    FailureKind.TRANSPORT,
                    "an earlier attempt may have created the event, and the marker lookup cannot confirm it",
                ),
                GraphResponse(),
            )
        return await self._send()

    async def _credential_failure(self, error: RequestFailure) -> MutationAttempt[CreateEventOutput]:
        failure = _failure("createEvent", error.kind, str(error))
        is_retryable = error.kind is FailureKind.AVAILABILITY
        if self.has_possible_earlier_send:
            if is_retryable:
                return MutationAttempt.retry(failure)
            return self._uncertain(
                _failure(
                    "createEvent",
                    error.kind,
                    "an earlier attempt may have created the event, and the connection credentials are "
                    "unavailable to confirm it",
                ),
                GraphResponse(),
            )
        try:
            await self.fence.record_not_sent()
        except Exception:
            return MutationAttempt.retry(
                _failure(
                    "createEvent",
                    FailureKind.AVAILABILITY,
                    "credentials and the no-send checkpoint are unavailable",
                )
            )
        if is_retryable:
            return MutationAttempt.retry(failure)
        return MutationAttempt.branch_result("defect", CreateEventOutput(), Receipt(), failure)

    async def _record_provider_id(self, provider_id: str) -> bool:
        try:
            await self.fence.record_provider_id(provider_id)
        except Exception:
            return False
        return True

    async def _record_best_effort(self, record: Any) -> None:
        # A lost checkpoint leaves the claim, which agent hosts conservatively report as unknown.
        try:
            await record()
        except Exception:
            pass

    def _created(
        self, event: Event, duplicates: tuple[str, ...], response: GraphResponse, *, existing: bool
    ) -> MutationAttempt[CreateEventOutput]:
        receipt = self.client.receipt(self.call, response, event.id)
        output = CreateEventOutput(event, existing, duplicates)
        return MutationAttempt.branch_result("created", output, receipt)

    async def _lookup(self) -> tuple[str, Event, tuple[str, ...], GraphResponse, Failure | None, float]:
        assert self.credentials is not None
        return await _lookup(
            self.client,
            self.call,
            self.credentials,
            "createEvent",
            self.root,
            self.key,
            self.input.calendar_id,
            self.display,
        )

    async def _look_up(self) -> MutationAttempt[CreateEventOutput] | None:
        outcome, event, duplicates, response, failure, delay = await self._lookup()
        failure = failure or _failure("createEvent", FailureKind.AVAILABILITY, "provider is unavailable")
        if outcome == "missing":
            return None
        if outcome == "found":
            await self._record_provider_id(event.id)
            return self._created(event, duplicates, response, existing=True)
        if outcome in ("providerRejected", "notFound") and _is_definite_graph_refusal(response):
            if self.has_possible_earlier_send:
                return self._uncertain(failure, response)
            await self._record_best_effort(self.fence.record_not_sent)
            return MutationAttempt.branch_result(
                "providerRejected", CreateEventOutput(), self.client.receipt(self.call, response), failure
            )
        if outcome == "invalidResponse" and not self.has_possible_earlier_send:
            # Nothing was sent yet, so an unusable lookup answer can end the Step without a duplicate.
            await self._record_best_effort(self.fence.record_not_sent)
            return MutationAttempt.branch_result(
                "invalidResponse", CreateEventOutput(), self.client.receipt(self.call, response), failure
            )
        if outcome == "defect":
            if self.has_possible_earlier_send:
                return self._uncertain(failure, GraphResponse())
            await self._record_best_effort(self.fence.record_not_sent)
            return MutationAttempt.branch_result("defect", CreateEventOutput(), Receipt(), failure)
        # A transient, unlisted, or unreadable lookup answer leaves the outcome unknown.
        return MutationAttempt.retry(failure, delay)

    async def _send(self) -> MutationAttempt[CreateEventOutput]:
        operation = "createEvent"
        credentials = self.credentials
        assert credentials is not None

        async def post(_write_deadline: datetime) -> GraphResponse:
            return await self.client.send(
                self.call,
                credentials,
                "POST",
                _calendar_path(self.root, self.input.calendar_id) + "/events",
                payload=_create_payload(self.input, self.key),
            )

        try:
            response = await self.fence.dispatch(post)
        except (CheckpointError, InsufficientAttemptTimeError, DispatchNotAllowedError):
            return MutationAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "the event POST was not sent in this attempt")
            )
        except TimeoutError:
            # The write window closed with the answer pending; a 2xx may have arrived, so it is never resent.
            return await self._accepted_without_event(GraphResponse())
        except RequestFailure as error:
            if error.kind == FailureKind.RESPONSE_TOO_LARGE and 200 <= error.response.status < 300:
                return await self._accepted_without_event(error.response)
            if error.kind == FailureKind.LOCAL_DEFECT and not self.has_possible_earlier_send:
                return MutationAttempt.branch_result(
                    "defect", CreateEventOutput(), Receipt(), _failure(operation, error.kind, str(error))
                )
            return self._resend_or_uncertain(
                _failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "create outcome is unknown; the retry looks the idempotency key up first",
                ),
                0,
                error.response,
            )
        if response.status in (429, 503):
            _, kind, message, delay = _classification(response.status, response.body, response.headers)
            # Graph did not process the POST, so it does not count against the cap.
            await self._record_best_effort(self.fence.record_can_resend)
            return MutationAttempt.retry(_failure(operation, kind, message), delay)
        if 200 <= response.status < 300:
            try:
                event = _event(_response_json(response), self.input.calendar_id, self.display)
                if event.transaction_id and event.transaction_id != self.key:
                    raise ValueError("another transactionId")
            except (ValueError, TypeError, KeyError, AttributeError):
                return await self._accepted_without_event(response)
            await self._record_provider_id(event.id)
            return self._created(event, (), response, existing=False)
        _, kind, message, delay = _classification(response.status, response.body, response.headers)
        failure = _failure(operation, kind, message)
        if response.status in (400, 409):
            outcome, event, duplicates, read_response, _, _ = await self._lookup()
            if outcome == "found":
                await self._record_provider_id(event.id)
                return self._created(event, duplicates, read_response, existing=True)
            if outcome != "missing":
                return self._resend_or_uncertain(failure, delay, response)
        if not _is_definite_graph_refusal(response):
            # Only an allowlisted refusal proves nothing was created; any other answer is unknown.
            return self._resend_or_uncertain(failure, delay, response)
        if self.has_possible_earlier_send:
            return self._uncertain(failure, response)
        await self._record_best_effort(self.fence.record_can_resend)
        return MutationAttempt.branch_result(
            "providerRejected", CreateEventOutput(), self.client.receipt(self.call, response), failure
        )

    async def _accepted_without_event(self, response: GraphResponse) -> MutationAttempt[CreateEventOutput]:
        failure = _failure(
            "createEvent", FailureKind.PROTOCOL, "Graph accepted the event, but its response is unusable"
        )
        if not await self._record_provider_id(_ACCEPTED_EVENT_PROVIDER_ID):
            return self._uncertain(failure, response)
        outcome, event, duplicates, read_response, _, _ = await self._lookup()
        if outcome == "found":
            await self._record_provider_id(event.id)
            return self._created(event, duplicates, read_response, existing=False)
        # The marker index may lag the create; the next attempt only reads back.
        return MutationAttempt.retry(failure)

    def _resend_or_uncertain(
        self, failure: Failure, delay: float, response: GraphResponse
    ) -> MutationAttempt[CreateEventOutput]:
        if self.can_resend_unknown_outcome:
            return MutationAttempt.retry(failure, delay)
        return self._uncertain(failure, response)

    def _uncertain(self, failure: Failure, response: GraphResponse) -> MutationAttempt[CreateEventOutput]:
        receipt = self.client.receipt(self.call, response)
        return MutationAttempt.uncertain(CreateEventOutput(), failure, receipt)


def _update_applied(current: Event, input: UpdateEventInput) -> bool:
    if input.subject is not None and current.subject != input.subject:
        return False
    if (
        input.body is not None
        and current.body.replace("\r\n", "\n").strip() != input.body.replace("\r\n", "\n").strip()
    ):
        return False
    if input.location is not None and current.location != input.location:
        return False
    if (
        input.start is not None
        and input.end is not None
        and (
            current.is_all_day != input.is_all_day
            or _instant("dateTime", current.start.date_time) != _instant("dateTime", input.start.date_time)
            or _instant("dateTime", current.end.date_time) != _instant("dateTime", input.end.date_time)
        )
    ):
        return False
    if input.attendees is not None:
        actual = {(item.email.lower(), item.type.lower()) for item in current.attendees}
        desired = {
            (item.email.lower(), "optional" if item.is_optional else "required") for item in input.attendees
        }
        return actual == desired and len(current.attendees) == len(input.attendees)
    return True


def _update_payload(input: UpdateEventInput) -> dict[str, Any]:
    payload: dict[str, Any] = {}
    if input.subject is not None:
        payload["subject"] = input.subject
    if input.body is not None:
        payload["body"] = {"contentType": "text", "content": input.body}
    if input.location is not None:
        payload["location"] = {"displayName": input.location}
    if input.start is not None and input.end is not None:
        payload["start"], payload["end"], payload["isAllDay"] = (
            _graph_boundary(input.start),
            _graph_boundary(input.end),
            input.is_all_day,
        )
    if input.attendees is not None:
        payload["attendees"] = _attendees(input.attendees)
    return payload


class UpdateEventOperation:
    """Compare and conditionally patch an event with its ETag."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest operation definition."""
        return UPDATE_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UpdateEventInput) -> IdempotencyKey:
        """Use the stable Dex Call ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateEventInput) -> MutationAttempt[UpdateEventOutput]:
        """Read, compare, and patch one existing event."""
        operation, empty = "updateEvent", UpdateEventOutput()
        try:
            input = replace(
                input,
                start=_coerce_boundary(input.start) if input.start is not None else None,
                end=_coerce_boundary(input.end) if input.end is not None else None,
                attendees=_coerce_attendees(input.attendees) if input.attendees is not None else None,
            )
            if not input.event_id.strip() or input.event_id != input.event_id.strip():
                raise ValueError("eventId is required")
            if all(
                item is None
                for item in (
                    input.subject,
                    input.body,
                    input.location,
                    input.start,
                    input.end,
                    input.attendees,
                )
            ):
                raise ValueError("set at least one of subject, body, location, start and end, or attendees")
            if input.subject is not None and not input.subject.strip():
                raise ValueError("subject cannot be blank")
            if (input.start is None) != (input.end is None):
                raise ValueError("set start and end together so the event keeps one unambiguous time range")
            if input.is_all_day and input.start is None:
                raise ValueError("isAllDay applies only together with start and end")
            if input.start is not None and input.end is not None:
                _event_times(input.start, input.end, input.is_all_day)
            if input.attendees is not None:
                _validate_attendees(input.attendees)
            display = _display(input.time_zone or (input.start.time_zone if input.start else ""))
            credentials, root = await self.client.resolve_credentials(call, operation)
        except RequestFailure as error:
            is_later_attempt = call.context is not None and call.context.attempt > 1
            # A later attempt may follow an applied PATCH, so defect would misreport it.
            if error.kind is FailureKind.AVAILABILITY or (
                is_later_attempt and error.kind is FailureKind.AUTHENTICATION
            ):
                return MutationAttempt.retry(_failure(operation, error.kind, str(error)))
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, error.kind, str(error))
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), _failure(operation, FailureKind.VALIDATION, str(error))
            )
        outcome, current, response, failure, delay = await _read_event(
            self.client, call, credentials, operation, root, input.event_id, display
        )
        read_receipt = self.client.receipt(call, response, input.event_id)
        is_later_attempt = call.context is not None and call.context.attempt > 1
        if outcome == "notFound":
            return MutationAttempt.branch_result("notFound", empty, read_receipt, failure)
        if outcome == "providerRejected" and _is_definite_graph_refusal(response):
            return MutationAttempt.branch_result("providerRejected", empty, read_receipt, failure)
        if outcome == "defect" and not is_later_attempt:
            return MutationAttempt.branch_result("defect", empty, Receipt(), failure)
        if outcome != "found":
            # A transient, unlisted, or unusable pre-write read is unknown, and the update is safe to repeat.
            return MutationAttempt.retry(
                failure or _failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"), delay
            )
        if current.is_cancelled:
            return MutationAttempt.branch_result(
                "notFound",
                empty,
                read_receipt,
                _failure(operation, FailureKind.NOT_FOUND, "event is cancelled"),
            )
        if _update_applied(current, input):
            return MutationAttempt.branch_result("updated", UpdateEventOutput(current, True), read_receipt)
        try:
            response = await self.client.send(
                call,
                credentials,
                "PATCH",
                root + "/events/" + _path_escape(input.event_id),
                payload=_update_payload(input),
                if_match=current.etag,
            )
        except RequestFailure as error:
            if error.kind == FailureKind.LOCAL_DEFECT:
                return MutationAttempt.branch_result(
                    "defect", empty, Receipt(), _failure(operation, error.kind, str(error))
                )
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "update outcome is unknown; the retry rereads the event first",
                )
            )
        receipt = self.client.receipt(call, response, input.event_id)
        if response.status in (409, 412):
            code = read_error_tokens(response.body, ("/error/code",))
            suffix = f" ({code[0]})" if code and ERROR_CODE.fullmatch(code[0]) else ""
            return MutationAttempt.retry(
                _failure(operation, FailureKind.CONFLICT, "event changed after it was read" + suffix)
            )
        if response.status in (404, 410):
            return MutationAttempt.branch_result(
                "notFound", empty, receipt, _failure(operation, FailureKind.NOT_FOUND, "event was not found")
            )
        if not 200 <= response.status < 300:
            retry, kind, message, delay = _classification(response.status, response.body, response.headers)
            return (
                MutationAttempt.retry(_failure(operation, kind, message), delay)
                if retry or not _is_definite_graph_refusal(response)
                else MutationAttempt.branch_result(
                    "providerRejected", empty, receipt, _failure(operation, kind, message)
                )
            )
        try:
            event = _event(_response_json(response), "", display)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            # The PATCH is absolute, so the retry rereads the event and finds the change if it applied.
            return MutationAttempt.retry(
                _failure(
                    operation, FailureKind.PROTOCOL, "provider returned an invalid event: " + str(error)
                )
            )
        return MutationAttempt.branch_result("updated", UpdateEventOutput(event), receipt)


__all__ = [
    "Client",
    "Config",
    "Credentials",
    "ListEventsOperation",
    "GetEventOperation",
    "CreateEventOperation",
    "UpdateEventOperation",
    "QueryFreeBusyOperation",
]
