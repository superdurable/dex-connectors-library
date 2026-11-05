# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Trello transport, card decoding, and secret-safe classifications."""

import math
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from .models import Card, Label, MemberReference

if TYPE_CHECKING:
    from .operations import (
        AddCommentOperation,
        CreateCardOperation,
        GetCardOperation,
        ListCardsOperation,
        UpdateCardOperation,
    )

_ID = re.compile(r"^[0-9a-fA-F]{24}$")
_SHORT_LINK = re.compile(r"^[0-9A-Za-z]{8}$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9-]{1,64}$")
SUMMARY_FIELDS = (
    "id,shortLink,name,idBoard,idList,closed,due,dueComplete,start,"
    "idLabels,labels,idMembers,pos,url,shortUrl,dateLastActivity"
)
DETAIL_FIELDS = SUMMARY_FIELDS + ",desc"


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """One bounded response, including status when body reading failed."""

    status: int
    headers: httpx.Headers
    body: bytes = b""
    oversized: bool = False
    interrupted: bool = False
    reflects_secret: bool = False


class Client:
    """Send Trello requests with current credentials and a caller-owned optional HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Trello configuration and credentials are required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        if self.max_response_bytes < 1:
            raise ValueError("Trello maxResponseBytes must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None

    async def aclose(self) -> None:
        """Close the connector-owned transport after Worker shutdown."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def list_cards(self) -> "ListCardsOperation":
        """Bind the card page Query to this client."""
        from .operations import ListCardsOperation

        return ListCardsOperation(self)

    def get_card(self) -> "GetCardOperation":
        """Bind the card detail Query to this client."""
        from .operations import GetCardOperation

        return GetCardOperation(self)

    def create_card(self) -> "CreateCardOperation":
        """Bind the single-dispatch card creation Mutation."""
        from .operations import CreateCardOperation

        return CreateCardOperation(self)

    def update_card(self) -> "UpdateCardOperation":
        """Bind the repeatable card update Mutation."""
        from .operations import UpdateCardOperation

        return UpdateCardOperation(self)

    def add_comment(self) -> "AddCommentOperation":
        """Bind the single-dispatch comment Mutation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Read and validate both secret header values for this call."""
        credentials = await self.credentials.resolve(call)
        if not isinstance(credentials, Credentials) or not all(
            is_header_safe_credential(value) and not any(character in value for character in '"\\,')
            for value in (credentials.api_key.reveal(), credentials.token.reveal())
        ):
            raise ValueError("Trello connection credentials are unavailable")
        return credentials

    async def request(
        self,
        credentials: Credentials,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> ProviderResponse:
        """Send one authenticated request without following redirects or persisting secrets."""
        api_key, token = credentials.api_key.reveal(), credentials.token.reveal()
        headers = {
            "Authorization": f'OAuth oauth_consumer_key="{api_key}", oauth_token="{token}"',
            "Accept": "application/json",
        }
        if payload is not None:
            headers["Content-Type"] = "application/json"
        request = self.http_client.build_request(
            method, self.endpoint + path, params=query, json=payload, headers=headers
        )
        response, _ = await send_traced(self.http_client, request)
        try:
            body = await read_bounded_body(
                response,
                self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES,
            )
        except BodyTooLargeError:
            return ProviderResponse(response.status_code, response.headers, oversized=True)
        except ProviderTransportError:
            return ProviderResponse(response.status_code, response.headers, interrupted=True)
        if api_key.encode() in body or token.encode() in body:
            return ProviderResponse(response.status_code, response.headers, reflects_secret=True)
        return ProviderResponse(response.status_code, response.headers, body)

    def receipt(self, call: Call, response: ProviderResponse | None = None, object_id: str = "") -> Receipt:
        """Record only a bounded request ID and provider object ID."""
        request_id = response.headers.get("Atl-Request-Id", "") if response else ""
        return Receipt(
            call.id,
            "trello",
            datetime.now(UTC),
            call.idempotency_key,
            object_id,
            request_id if _REQUEST_ID.fullmatch(request_id) else "",
        )


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create a persistable failure without response bodies or secrets."""
    return Failure(kind, "trello", operation, message)


def trello_id(value: str, field: str) -> str:
    """Validate a Trello object ID before it enters a URL or request body."""
    value = value.strip()
    if not _ID.fullmatch(value):
        raise ValueError(
            f"{field} must be a Trello ID, 24 hexadecimal characters such as 5b6893f01cb3228998cf629e"
        )
    return value


def trello_ids(values: tuple[str, ...] | list[str], field: str) -> tuple[str, ...]:
    """Validate no more than 50 distinct provider object IDs."""
    if len(values) > 50:
        raise ValueError(f"{field} accepts at most 50 IDs")
    result = tuple(trello_id(value, field) for value in values)
    if len({value.lower() for value in result}) != len(result):
        raise ValueError(f"{field} lists an ID twice")
    return result


def path(*segments: str) -> str:
    """Escape validated provider path segments."""
    return "/" + "/".join(quote(segment, safe="") for segment in segments)


def trello_time(value: datetime) -> str:
    """Write Trello's millisecond UTC timestamp."""
    return value.astimezone(UTC).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def parse_time(value: object) -> datetime | None:
    """Decode an optional ISO timestamp and reject malformed provider values."""
    if value is None or value == "":
        return None
    if not isinstance(value, str) or not re.fullmatch(
        r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})", value
    ):
        raise ValueError("timestamp is not an ISO 8601 date-time")
    try:
        result = datetime.fromisoformat(value.replace("Z", "+00:00"))
        if result.tzinfo is None:
            raise ValueError
        return result.astimezone(UTC)
    except ValueError:
        raise ValueError("timestamp is not an ISO 8601 date-time") from None


def decode_card(value: object, detail: bool) -> Card:
    """Validate Trello's card identity and retain only bounded public fields."""
    if not isinstance(value, dict):
        raise ValueError("response must be a JSON object")
    for name in ("id", "idBoard", "idList"):
        if not isinstance(value.get(name, ""), str):
            raise ValueError(f"card {name} has an invalid type")
    card_id = trello_id(value.get("id", ""), "card ID")
    board_id = trello_id(value.get("idBoard", ""), "card board ID")
    list_id = trello_id(value.get("idList", ""), "card list ID")
    for name in ("name", "desc", "shortLink", "url", "shortUrl"):
        if name in value and not isinstance(value[name], str):
            raise ValueError(f"card {name} has an invalid type")
    for name in ("closed", "dueComplete"):
        if name in value and type(value[name]) is not bool:
            raise ValueError(f"card {name} has an invalid type")
    if (
        "pos" in value
        and value["pos"] is not None
        and (type(value["pos"]) not in (int, float) or not math.isfinite(value["pos"]))
    ):
        raise ValueError("card pos has an invalid type")
    for name in ("idLabels", "labels", "idMembers", "members"):
        if name in value and value[name] is not None and not isinstance(value[name], list):
            raise ValueError(f"card {name} has an invalid type")
    label_ids: list[str] = []
    labels: list[Label] = []
    has_more_labels = False
    for entry in (*(value.get("idLabels") or []), *(value.get("labels") or [])):
        if isinstance(entry, str):
            label_id, label = entry, None
        elif isinstance(entry, dict):
            label_id = entry.get("id", "")
            label = Label(label_id, entry.get("name", ""), entry.get("color") or "")
        else:
            continue
        if not isinstance(label_id, str) or not _ID.fullmatch(label_id):
            continue
        if (
            label is not None
            and len(labels) < 50
            and all(item.id.lower() != label_id.lower() for item in labels)
        ):
            labels.append(label)
        if all(item.lower() != label_id.lower() for item in label_ids):
            if len(label_ids) == 50:
                has_more_labels = True
            else:
                label_ids.append(label_id)
    member_ids = tuple(
        member
        for member in (value.get("idMembers") or [])
        if isinstance(member, str) and _ID.fullmatch(member)
    )[:50]
    members = (
        tuple(
            MemberReference(item["id"], item.get("fullName", ""), item.get("username", ""))
            for item in (value.get("members") or [])
            if isinstance(item, dict) and _ID.fullmatch(str(item.get("id", "")))
        )[:50]
        if detail
        else ()
    )
    description = value.get("desc", "") if detail else ""
    list_value = value.get("list")
    list_name = (
        list_value.get("name", "")
        if detail and isinstance(list_value, dict) and list_value.get("id") == list_id
        else ""
    )
    short_link = value.get("shortLink", "")
    position = float(value.get("pos") or 0)
    if math.isfinite(position) and position.is_integer() and abs(position) <= 2**53:
        position = int(position)
    return Card(
        id=card_id,
        board_id=board_id,
        list_id=list_id,
        name=value.get("name", ""),
        short_link=short_link if isinstance(short_link, str) and _SHORT_LINK.fullmatch(short_link) else "",
        description=description[:16384],
        is_description_truncated=len(description) > 16384,
        list_name=list_name,
        is_closed=bool(value.get("closed", False)),
        due=parse_time(value.get("due")),
        is_due_complete=bool(value.get("dueComplete", False)),
        start=parse_time(value.get("start")),
        label_ids=tuple(label_ids),
        labels=tuple(labels),
        has_more_labels=has_more_labels,
        member_ids=member_ids,
        members=members,
        position=position,
        url=value.get("url", ""),
        short_url=value.get("shortUrl", ""),
        last_activity_at=parse_time(value.get("dateLastActivity")),
    )


def classify_response(
    operation: str, subject: str, response: ProviderResponse, *, write: bool = False, repeatable: bool = False
) -> tuple[str, Failure | None, float]:
    """Map status and bounded-body evidence to Go-compatible outcomes."""
    status = response.status
    delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
    if status == 429:
        tokens = read_error_tokens(response.body, ("/error",))
        suffix = f" [{tokens[0]}]" if tokens else ""
        return (
            "retry",
            failure(operation, FailureKind.RATE_LIMIT, f"Trello rate limited the {subject}{suffix}"),
            delay or 10,
        )
    if write and not repeatable:
        if 200 <= status < 300:
            if response.interrupted:
                return (
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        f"Trello accepted the {subject} but its response was interrupted",
                    ),
                    0,
                )
            if response.oversized:
                return (
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        f"Trello accepted the {subject} but its response exceeds the configured size limit",
                    ),
                    0,
                )
            if response.reflects_secret:
                return (
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        f"Trello accepted the {subject} but its response contains credential material",
                    ),
                    0,
                )
            return "accepted", None, 0
        if status == 503:
            return (
                "retry",
                failure(operation, FailureKind.AVAILABILITY, f"Trello {subject} is temporarily unavailable"),
                delay,
            )
        if status == 404:
            return (
                "notFound",
                failure(
                    operation, FailureKind.NOT_FOUND, f"Trello rejected the {subject} with HTTP {status}"
                ),
                0,
            )
        if status == 409:
            return (
                "uncertain",
                failure(
                    operation,
                    FailureKind.CONFLICT,
                    f"Trello {subject} conflicts with provider state and cannot be retried without a key",
                ),
                0,
            )
        if status < 400 or status >= 500 or status == 408:
            return (
                "uncertain",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"Trello {subject} outcome is unknown after HTTP {status}",
                ),
                0,
            )
    else:
        if status == 404:
            return (
                "notFound",
                failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    f"Trello {subject} was not found"
                    if not write
                    else f"Trello rejected the {subject} with HTTP {status}",
                ),
                0,
            )
        if status == 408 or status >= 500:
            return (
                "retry",
                failure(operation, FailureKind.AVAILABILITY, f"Trello {subject} is temporarily unavailable"),
                delay,
            )
        if 200 <= status < 300:
            if response.interrupted:
                if repeatable:
                    return "accepted", None, 0
                return (
                    "retry",
                    failure(operation, FailureKind.TRANSPORT, f"Trello {subject} response was interrupted"),
                    0,
                )
            if response.oversized:
                return (
                    "invalidResponse",
                    failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        f"Trello {subject} response exceeds the configured size limit",
                    ),
                    0,
                )
            if response.reflects_secret:
                return (
                    "invalidResponse",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        f"Trello {subject} response contains credential material",
                    ),
                    0,
                )
            return "accepted", None, 0
        if status < 400 or status >= 500:
            return (
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Trello returned unexpected HTTP {status} for the {subject}",
                ),
                0,
            )
    kinds = {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }
    return (
        "providerRejected",
        failure(
            operation,
            kinds.get(status, FailureKind.PROVIDER_REJECTION),
            f"Trello rejected the {subject} with HTTP {status}",
        ),
        0,
    )
