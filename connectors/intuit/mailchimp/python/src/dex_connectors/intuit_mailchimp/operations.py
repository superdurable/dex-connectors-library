# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mailchimp operation invocations and exact provider outcome routing."""

import hashlib
import ipaddress
import json
import math
import re
import unicodedata
from collections.abc import Iterable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
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
    Decision,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    GET_MEMBER_DEFINITION,
    LIST_MEMBERS_DEFINITION,
    SEND_CAMPAIGN_DEFINITION,
    UPDATE_MEMBER_TAGS_DEFINITION,
    UPSERT_MEMBER_DEFINITION,
)
from .client import Client, Exchange, failure
from .models import (
    Campaign,
    GetMemberInput,
    ListMembersInput,
    ListMembersOutput,
    Member,
    SendCampaignInput,
    SendCampaignOutput,
    UpdateMemberTagsInput,
    UpdateMemberTagsOutput,
    UpsertMemberInput,
    UpsertMemberOutput,
)


@dataclass(frozen=True, slots=True)
class _LegacySendDispatchMarker:
    """Checkpoint that releases before 0.2.1 recorded before a send."""

    dispatched_call_id: CallID = field(metadata={"dex_json_name": "mailchimpDispatchedCallId"})


_RESOURCE_ID = re.compile(r"[A-Za-z0-9]{1,64}\Z")
_SUBSCRIBER_HASH = re.compile(r"[0-9a-f]{32}\Z")
_MERGE_TAG = re.compile(r"[A-Z0-9_]{1,50}\Z")
_ADDRESS_KEY = re.compile(r"[a-z][a-z0-9_]{0,31}\Z")
_ADDRESS_ATOM = r"[A-Za-z0-9!#$%&'*+/=?^_`{|}~\-\u0080-\U0010FFFF]+"
_BARE_ADDRESS = re.compile(
    rf"{_ADDRESS_ATOM}(?:\.{_ADDRESS_ATOM})*@(?:{_ADDRESS_ATOM}(?:\.{_ADDRESS_ATOM})*|\[[0-9A-Fa-f:.]+\])\Z"
)
_MEMBER_LIST_FIELDS = ",".join(
    (
        "members.id",
        "members.email_address",
        "members.contact_id",
        "members.full_name",
        "members.web_id",
        "members.email_type",
        "members.status",
        "members.merge_fields",
        "members.vip",
        "members.language",
        "members.timestamp_signup",
        "members.timestamp_opt",
        "members.last_changed",
        "members.tags_count",
        "members.tags",
        "members.list_id",
        "list_id",
        "total_items",
    )
)
_CAMPAIGN_FIELDS = ",".join(
    (
        "id",
        "web_id",
        "type",
        "status",
        "emails_sent",
        "create_time",
        "send_time",
        "settings.title",
        "settings.subject_line",
        "recipients.list_id",
        "recipients.list_name",
        "recipients.recipient_count",
    )
)
# Statuses that prove a write was refused; 409, 3xx, and other statuses are unknown.
_DEFINITE_REFUSAL_STATUSES = frozenset((400, 401, 403, 404, 411, 413, 414, 415, 422))
_WRITABLE_STATUSES = frozenset(("subscribed", "pending", "unsubscribed", "transactional"))
_READABLE_STATUSES = _WRITABLE_STATUSES | {"cleaned", "archived"}


def _resource_id(name: str, value: str) -> None:
    if not _RESOURCE_ID.fullmatch(value):
        raise ValueError(f"{name} must be a Mailchimp ID of 1 to 64 letters and digits")


def _member_address(list_id: str, email_address: str) -> None:
    _resource_id("listId", list_id)
    if not email_address or len(email_address.encode()) > 254 or not _BARE_ADDRESS.fullmatch(email_address):
        raise ValueError("emailAddress must be one bare email address such as jane@example.com")
    domain = email_address.rsplit("@", 1)[1]
    if domain.startswith("["):
        try:
            ipaddress.ip_address(domain[1:-1])
        except ValueError as error:
            raise ValueError("emailAddress has an invalid IP address domain") from error


def _subscriber_hash(email_address: str) -> str:
    lowered = "".join("i" if character == "İ" else character.lower() for character in email_address)
    return hashlib.md5(lowered.encode()).hexdigest()  # noqa: S324 -- Mailchimp's address key.


def _parse_time(value: object) -> datetime | None:
    if value in ("", None):
        return None
    if not isinstance(value, str):
        raise ValueError("a timestamp is not ISO 8601")
    try:
        parsed = datetime.fromisoformat(re.sub(r"T(\d):", r"T0\1:", value).replace("Z", "+00:00"))
        if parsed.utcoffset() is None:
            raise ValueError
        return parsed.astimezone(UTC)
    except ValueError as error:
        raise ValueError("a timestamp is not ISO 8601") from error


def _decode_object(body: bytes) -> dict[str, Any]:
    value = json.loads(body)
    if not isinstance(value, dict):
        raise ValueError("the response is not an object")
    return value


def _text_field(wire: dict[str, Any], name: str) -> str:
    value = wire.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"{name} is not a string")
    return value


def _integer_field(wire: dict[str, Any], name: str) -> int:
    value = wire.get(name)
    if value is None:
        return 0
    if isinstance(value, bool) or not isinstance(value, int):
        raise ValueError(f"{name} is not an integer")
    return value


def _object_field(wire: dict[str, Any], name: str) -> dict[str, Any]:
    value = wire.get(name)
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError(f"{name} is not an object")
    return value


def _decode_member(wire: dict[str, Any], expected_hash: str = "") -> Member:
    if not isinstance(wire, dict):
        raise ValueError("the contact is not an object")
    subscriber_hash = _text_field(wire, "id")
    if not _SUBSCRIBER_HASH.fullmatch(subscriber_hash):
        raise ValueError("the contact has no subscriber hash")
    if expected_hash and subscriber_hash != expected_hash:
        raise ValueError("the contact's subscriber hash does not match the requested address")
    email_address = _text_field(wire, "email_address")
    status = _text_field(wire, "status")
    if not email_address or not status:
        raise ValueError("the contact has no email address or status")
    tags = wire.get("tags")
    if tags is None:
        tags = []
    if not isinstance(tags, list) or any(not isinstance(tag, dict) for tag in tags):
        raise ValueError("tags is not a list of objects")
    vip = wire.get("vip")
    if vip is not None and not isinstance(vip, bool):
        raise ValueError("vip is not a boolean")
    return Member(
        subscriber_hash=subscriber_hash,
        email_address=email_address,
        contact_id=_text_field(wire, "contact_id"),
        full_name=_text_field(wire, "full_name"),
        status=status,
        email_type=_text_field(wire, "email_type"),
        merge_fields=_object_field(wire, "merge_fields"),
        tags=tuple(_text_field(tag, "name") for tag in tags),
        tag_count=_integer_field(wire, "tags_count"),
        is_vip=vip or False,
        language=_text_field(wire, "language"),
        list_id=_text_field(wire, "list_id"),
        web_id=_integer_field(wire, "web_id"),
        signed_up_at=_parse_time(_text_field(wire, "timestamp_signup")),
        opted_in_at=_parse_time(_text_field(wire, "timestamp_opt")),
        last_changed_at=_parse_time(_text_field(wire, "last_changed")),
    )


def _decode_campaign(body: bytes, campaign_id: str) -> Campaign:
    wire = _decode_object(body)
    status = _text_field(wire, "status")
    campaign_type = _text_field(wire, "type")
    if _text_field(wire, "id") != campaign_id or not status or not campaign_type:
        raise ValueError("the campaign has another ID, or no status or type")
    settings = _object_field(wire, "settings")
    recipients = _object_field(wire, "recipients")
    return Campaign(
        id=campaign_id,
        web_id=_integer_field(wire, "web_id"),
        type=campaign_type,
        status=status,
        title=_text_field(settings, "title"),
        subject_line=_text_field(settings, "subject_line"),
        list_id=_text_field(recipients, "list_id"),
        list_name=_text_field(recipients, "list_name"),
        recipient_count=_integer_field(recipients, "recipient_count"),
        emails_sent=_integer_field(wire, "emails_sent"),
        created_at=_parse_time(_text_field(wire, "create_time")),
        sent_at=_parse_time(_text_field(wire, "send_time")),
    )


def _query_exchange(
    exchange: Exchange,
    receipt: Receipt,
    empty: Any,
    *,
    has_not_found: bool = True,
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rateLimited", "notSent", "unavailable"):
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        "notFound"
        if exchange.outcome == "notFound" and has_not_found
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "defect"
        if exchange.outcome == "defect"
        else "providerRejected"
    )
    return QueryAttempt.branch_result(branch, empty, receipt, exchange.failure)


def _is_outcome_unknown_after_dispatch(exchange: Exchange) -> bool:
    """Report an outcome that does not prove Mailchimp refused or skipped a write."""
    if exchange.outcome in ("rateLimited", "notSent", "unavailable", "invalid"):
        return True
    return exchange.outcome == "rejected" and exchange.status not in _DEFINITE_REFUSAL_STATUSES


def _mutation_exchange(
    exchange: Exchange,
    receipt: Receipt,
    empty: Any,
    *,
    has_not_found: bool = True,
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    # A status that is not a definite refusal is retried; an unusable 2xx keeps invalidResponse.
    if exchange.outcome != "invalid" and _is_outcome_unknown_after_dispatch(exchange):
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        "notFound"
        if exchange.outcome == "notFound" and has_not_found
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "defect"
        if exchange.outcome == "defect"
        else "providerRejected"
    )
    return MutationAttempt.branch_result(branch, empty, receipt, exchange.failure)


class GetMemberOperation:
    """Read one audience contact in any Mailchimp status."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the stable getMember operation contract."""
        return GET_MEMBER_DEFINITION

    async def invoke(self, call: Call, input: GetMemberInput) -> QueryAttempt[Member]:
        """Read the MD5-addressed contact and route its outcome."""
        empty = Member(status="")
        try:
            _member_address(input.list_id, input.email_address)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, "getMember", str(error))
            )
        connection = await self._client.resolve_credentials(call, "getMember")
        if not isinstance(connection, tuple):
            return QueryAttempt.branch_result("defect", empty, Receipt(), connection)
        subscriber_hash = _subscriber_hash(input.email_address)
        exchange = await self._client.exchange(
            call, "getMember", connection, "GET", f"/lists/{input.list_id}/members/{subscriber_hash}"
        )
        receipt = self._client.receipt(call, exchange, subscriber_hash)
        terminal = _query_exchange(exchange, receipt, empty)
        if terminal is not None:
            return terminal
        try:
            member = _decode_member(_decode_object(exchange.body), subscriber_hash)
            return QueryAttempt.branch_result("found", member, receipt)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(FailureKind.PROTOCOL, "getMember", "Mailchimp returned an invalid contact"),
            )


class ListMembersOperation:
    """Read one bounded page of audience contacts."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the stable listMembers operation contract."""
        return LIST_MEMBERS_DEFINITION

    async def invoke(self, call: Call, input: ListMembersInput) -> QueryAttempt[ListMembersOutput]:
        """Request a sorted page and decode each contact."""
        empty = ListMembersOutput()
        try:
            _resource_id("listId", input.list_id)
            if input.status and input.status not in _READABLE_STATUSES:
                raise ValueError("status is not a Mailchimp contact status")
            if input.page_size < 0 or input.page_size > 1000 or input.offset < 0 or input.offset > 2147483647:
                raise ValueError("pageSize or offset is outside the Mailchimp range")
            page_size = input.page_size or 100
            query = {
                "count": str(page_size),
                "offset": str(input.offset),
                "sort_field": "last_changed",
                "sort_dir": "ASC",
                "fields": _MEMBER_LIST_FIELDS,
            }
            if input.status:
                query["status"] = input.status
            for source, target in (
                (input.changed_since, "since_last_changed"),
                (input.opted_in_since, "since_timestamp_opt"),
            ):
                if source is not None:
                    if source == datetime.min.replace(tzinfo=UTC):
                        raise ValueError("a timestamp must not be zero")
                    query[target] = source.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S+00:00")
        except (ValueError, AttributeError) as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, "listMembers", str(error))
            )
        connection = await self._client.resolve_credentials(call, "listMembers")
        if not isinstance(connection, tuple):
            return QueryAttempt.branch_result("defect", empty, Receipt(), connection)
        exchange = await self._client.exchange(
            call, "listMembers", connection, "GET", f"/lists/{input.list_id}/members", query=query
        )
        receipt = self._client.receipt(call, exchange, input.list_id)
        terminal = _query_exchange(exchange, receipt, empty)
        if terminal is not None:
            return terminal
        try:
            page = _decode_object(exchange.body)
            members = page.get("members")
            if members is None:
                members = []
            total_items = page.get("total_items")
            if (
                not isinstance(members, list)
                or isinstance(total_items, bool)
                or not isinstance(total_items, int)
                or total_items < 0
                or len(members) > page_size
            ):
                raise ValueError("the page has no total_items or more contacts than requested")
            output = ListMembersOutput(
                tuple(_decode_member(member) for member in members),
                total_items,
                input.offset + len(members) if members and input.offset + len(members) < total_items else 0,
            )
            return QueryAttempt.branch_result("listed", output, receipt)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    FailureKind.PROTOCOL, "listMembers", "Mailchimp returned an invalid page of contacts"
                ),
            )


def _validate_merge_fields(fields: dict[str, Any]) -> None:
    if len(fields) > 30:
        raise ValueError("mergeFields holds at most 30 fields")
    for tag, value in fields.items():
        if not _MERGE_TAG.fullmatch(tag):
            raise ValueError("merge tag must be uppercase letters, digits, or underscores")
        if isinstance(value, bool) or not isinstance(value, (str, int, float, dict)):
            raise ValueError("merge field must be a string, number, or address object")
        if isinstance(value, float) and not math.isfinite(value):
            raise ValueError("merge field must be finite")
        if isinstance(value, dict):
            if any(
                not isinstance(text, str) or not _ADDRESS_KEY.fullmatch(key) for key, text in value.items()
            ):
                raise ValueError("merge field address must have string values")
            texts: Iterable[str] = value.values()
        else:
            texts = (value,) if isinstance(value, str) else ()
        for text in texts:
            if len(text.encode()) > 1024 or any(
                unicodedata.category(char) == "Cc" and char not in "\n\t" for char in text
            ):
                raise ValueError("merge field text is invalid")


class UpsertMemberOperation:
    """Write absolute contact values without resubscribing an existing contact by default."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the stable upsertMember contract."""
        return UPSERT_MEMBER_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UpsertMemberInput) -> IdempotencyKey:
        """Correlate repeatable PUT attempts with the Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertMemberInput) -> MutationAttempt[UpsertMemberOutput]:
        """Write one contact and decode the returned contact."""
        empty = UpsertMemberOutput(Member(status=""))
        try:
            _member_address(input.list_id, input.email_address)
            if input.status_if_new not in _WRITABLE_STATUSES:
                raise ValueError("statusIfNew must be a writable Mailchimp status")
            if input.status and input.status not in _WRITABLE_STATUSES:
                raise ValueError("status must be a writable Mailchimp status")
            if input.status in ("subscribed", "pending") and not input.is_resubscribe_allowed:
                raise ValueError("resubscription requires isResubscribeAllowed")
            _validate_merge_fields(input.merge_fields)
            if len(input.marketing_permissions) > 20:
                raise ValueError("marketingPermissions holds at most 20 permissions")
            seen: set[str] = set()
            permissions: list[dict[str, Any]] = []
            for permission in input.marketing_permissions:
                identity = (
                    permission["marketingPermissionId"]
                    if isinstance(permission, dict)
                    else permission.marketing_permission_id
                )
                enabled = permission["isEnabled"] if isinstance(permission, dict) else permission.is_enabled
                _resource_id("marketingPermissionId", identity)
                if identity in seen:
                    raise ValueError("marketing permission repeats")
                seen.add(identity)
                permissions.append({"marketing_permission_id": identity, "enabled": enabled})
            payload: dict[str, Any] = {
                "email_address": input.email_address,
                "status_if_new": input.status_if_new,
            }
            if input.status:
                payload["status"] = input.status
            if input.merge_fields:
                payload["merge_fields"] = input.merge_fields
            if permissions:
                payload["marketing_permissions"] = permissions
        except (ValueError, KeyError, TypeError) as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, "upsertMember", str(error))
            )
        connection = await self._client.resolve_credentials(call, "upsertMember")
        if not isinstance(connection, tuple):
            return MutationAttempt.branch_result("defect", empty, Receipt(), connection)
        subscriber_hash = _subscriber_hash(input.email_address)
        exchange = await self._client.exchange(
            call,
            "upsertMember",
            connection,
            "PUT",
            f"/lists/{input.list_id}/members/{subscriber_hash}",
            payload=payload,
            query={"skip_merge_validation": "true"} if input.should_skip_merge_validation else None,
        )
        receipt = self._client.receipt(call, exchange, subscriber_hash)
        terminal = _mutation_exchange(exchange, receipt, empty, has_not_found=False)
        if terminal is not None:
            return terminal
        try:
            member = _decode_member(_decode_object(exchange.body), subscriber_hash)
            return MutationAttempt.branch_result("upserted", UpsertMemberOutput(member), receipt)
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    FailureKind.PROTOCOL,
                    "upsertMember",
                    "Mailchimp accepted the contact but returned an invalid contact",
                ),
            )


class UpdateMemberTagsOperation:
    """Declare contact tags active or inactive, making retries repeatable."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the stable updateMemberTags contract."""
        return UPDATE_MEMBER_TAGS_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UpdateMemberTagsInput) -> IdempotencyKey:
        """Correlate repeatable tag declarations with the Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: UpdateMemberTagsInput
    ) -> MutationAttempt[UpdateMemberTagsOutput]:
        """Post tag declarations and route the provider response."""
        empty = UpdateMemberTagsOutput()
        try:
            _member_address(input.list_id, input.email_address)
            names = (*input.add_tags, *input.remove_tags)
            if not names or len(names) > 50:
                raise ValueError("addTags and removeTags hold 1 to 50 tags together")
            seen: set[str] = set()
            for tag in names:
                if (
                    not tag
                    or len(tag.encode()) > 100
                    or tag.strip() != tag
                    or any(unicodedata.category(char) == "Cc" for char in tag)
                ):
                    raise ValueError("tag must be 1 to 100 bytes without surrounding spaces or controls")
                if tag.lower() in seen:
                    raise ValueError("tag appears more than once")
                seen.add(tag.lower())
            tags = [{"name": tag, "status": "active"} for tag in input.add_tags] + [
                {"name": tag, "status": "inactive"} for tag in input.remove_tags
            ]
            payload: dict[str, Any] = {"tags": tags}
            if input.should_suppress_automations:
                payload["is_syncing"] = True
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, "updateMemberTags", str(error))
            )
        connection = await self._client.resolve_credentials(call, "updateMemberTags")
        if not isinstance(connection, tuple):
            return MutationAttempt.branch_result("defect", empty, Receipt(), connection)
        subscriber_hash = _subscriber_hash(input.email_address)
        exchange = await self._client.exchange(
            call,
            "updateMemberTags",
            connection,
            "POST",
            f"/lists/{input.list_id}/members/{subscriber_hash}/tags",
            payload=payload,
        )
        receipt = self._client.receipt(call, exchange, subscriber_hash)
        terminal = _mutation_exchange(exchange, receipt, empty)
        if terminal is not None:
            return terminal
        return MutationAttempt.branch_result(
            "updated", UpdateMemberTagsOutput(subscriber_hash, input.add_tags, input.remove_tags), receipt
        )


class SendCampaignOperation:
    """Read a campaign and send a draft under a single-dispatch fence capped by maxSends."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the stable sendCampaign contract."""
        return SEND_CAMPAIGN_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: SendCampaignInput) -> IdempotencyKey:
        """Correlate the send with its logical Call ID; Mailchimp has no deduplication key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: SendCampaignInput) -> MutationAttempt[SendCampaignOutput]:
        """Read the campaign, then send a draft; resend an unknown outcome only below the cap."""
        empty = SendCampaignOutput(Campaign(status=""))
        try:
            _resource_id("campaignId", input.campaign_id)
            if input.expected_list_id:
                _resource_id("expectedListId", input.expected_list_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, "sendCampaign", str(error))
            )
        if _has_legacy_dispatch(call):
            return await _read_back_without_send(
                self._client, call, input, "an earlier attempt of this Step may have sent the campaign"
            )
        try:
            fence = Fence(
                call,
                SEND_CAMPAIGN_DEFINITION.step_defaults.execute_method_timeout,
                max_sends=call.max_sends,
            )
            decision = await fence.decide()
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    FailureKind.LOCAL_DEFECT,
                    "sendCampaign",
                    "the Mailchimp dispatch fence could not be constructed",
                ),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    "sendCampaign",
                    "the dispatch checkpoint could not be read; nothing was sent in this attempt",
                )
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return await _read_back_without_send(self._client, call, input, "the send cap is reached")
        attempt = _SendCampaignAttempt(self._client, call, input, fence, decision)
        return await attempt.read_then_send()


class _SendCampaignAttempt:
    """One fenced sendCampaign attempt after the Fence decided it may send."""

    def __init__(
        self, client: Client, call: Call, input: SendCampaignInput, fence: Fence, decision: Decision
    ) -> None:
        self._client = client
        self._call = call
        self._input = input
        self._fence = fence
        self._decision = decision
        # False when this attempt's send is the last one the cap allows.
        self._can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        # A refusal or a changed campaign cannot prove nothing was sent.
        self._has_possible_earlier_send = decision.prior_send_upper_bound > 0

    async def read_then_send(self) -> MutationAttempt[SendCampaignOutput]:
        """Read the campaign, which is also the read-back of an earlier send, and send a draft."""
        empty = SendCampaignOutput(Campaign(status=""))
        connection = await self._client.resolve_credentials(self._call, "sendCampaign")
        if not isinstance(connection, tuple):
            if self._has_possible_earlier_send:
                return MutationAttempt.uncertain(
                    empty,
                    failure(
                        FailureKind.AUTHENTICATION,
                        "sendCampaign",
                        "an earlier attempt may have sent the campaign, and the connection credentials "
                        "are unavailable to read it; reconnect the connection",
                    ),
                    Receipt(),
                )
            return MutationAttempt.branch_result("defect", empty, Receipt(), connection)
        exchange = await self._client.exchange(
            self._call,
            "sendCampaign",
            connection,
            "GET",
            f"/campaigns/{self._input.campaign_id}",
            query={"fields": _CAMPAIGN_FIELDS},
        )
        receipt = self._client.receipt(self._call, exchange, self._input.campaign_id)
        if exchange.outcome in ("rateLimited", "notSent", "unavailable"):
            assert exchange.failure is not None
            return await self._retry_before_send(exchange.failure, exchange.retry_after)
        if exchange.outcome != "succeeded":
            assert exchange.failure is not None
            return self._refuse_or_select_uncertain(
                _campaign_read_branch(exchange.outcome), empty, exchange.failure, receipt
            )
        try:
            campaign = _decode_campaign(exchange.body, self._input.campaign_id)
        except (ValueError, TypeError, KeyError):
            return self._refuse_or_select_uncertain(
                "invalidResponse",
                empty,
                failure(
                    FailureKind.PROTOCOL,
                    "sendCampaign",
                    "Mailchimp returned an invalid campaign, so it was not sent",
                ),
                receipt,
            )
        output = SendCampaignOutput(campaign)
        if campaign.status in ("sending", "sent"):
            return MutationAttempt.branch_result("alreadySent", output, receipt)
        if (
            campaign.status != "save"
            or campaign.type not in ("regular", "plaintext", "absplit", "variate")
            or (self._input.expected_list_id and campaign.list_id != self._input.expected_list_id)
        ):
            # A campaign canceled or changed after a possible earlier send may have partly sent.
            return self._refuse_or_select_uncertain(
                "notSendable",
                output,
                failure(
                    FailureKind.CONFLICT,
                    "sendCampaign",
                    "the campaign is not sendable; nothing was sent in this attempt",
                ),
                receipt,
            )
        return await self._send(connection, output)

    async def _send(
        self, connection: tuple[str, str], output: SendCampaignOutput
    ) -> MutationAttempt[SendCampaignOutput]:
        try:
            sent = await self._fence.dispatch(
                lambda _deadline: self._client.exchange(
                    self._call,
                    "sendCampaign",
                    connection,
                    "POST",
                    f"/campaigns/{self._input.campaign_id}/actions/send",
                )
            )
        except CheckpointError:
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    "sendCampaign",
                    "Dex did not record the dispatch checkpoint; nothing was sent in this attempt",
                )
            )
        except (InsufficientAttemptTimeError, DispatchNotAllowedError):
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    "sendCampaign",
                    "too little of the Step deadline remained to send; nothing was sent in this attempt",
                )
            )
        except TimeoutError:
            sent = Exchange(
                "unavailable",
                failure(
                    FailureKind.TRANSPORT,
                    "sendCampaign",
                    "Mailchimp request failed before a response arrived",
                ),
            )
        receipt = self._client.receipt(self._call, sent, self._input.campaign_id)
        if sent.outcome == "succeeded":
            return MutationAttempt.branch_result("sent", output, receipt)
        assert sent.failure is not None
        if sent.outcome in ("rateLimited", "notSent") or (
            sent.outcome == "unavailable" and sent.status == 503
        ):
            try:
                await self._fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        FailureKind.AVAILABILITY,
                        "sendCampaign",
                        "the Mailchimp retry checkpoint could not be recorded",
                    )
                )
            return MutationAttempt.retry(sent.failure, sent.retry_after)
        if _is_outcome_unknown_after_dispatch(sent):
            if self._can_resend_unknown_outcome:
                # The claim stays the last marker, so the next attempt counts this send and reads first.
                return MutationAttempt.retry(sent.failure, sent.retry_after)
            return MutationAttempt.uncertain(output, sent.failure, receipt)
        if self._has_possible_earlier_send:
            # A refused resend usually means an earlier send landed, so the campaign is read instead.
            return await _read_back_without_send(
                self._client,
                self._call,
                self._input,
                "Mailchimp refused a resend after a possible earlier send",
            )
        try:
            await self._fence.record_can_resend()
        except Exception:
            pass  # The refusal proves nothing was sent; a lost checkpoint only makes a replay read first.
        branch = {"notFound": "notFound", "defect": "defect"}.get(sent.outcome, "providerRejected")
        return MutationAttempt.branch_result(branch, output, receipt, sent.failure)

    async def _retry_before_send(
        self, retry_failure: Failure, retry_after: float
    ) -> MutationAttempt[SendCampaignOutput]:
        """Record notSent before a pre-send Retry so the next attempt does not count a send."""
        if self._decision.kind == DecisionKind.SEND_ALLOWED:
            # After READ_BACK_THEN_RESEND_ALLOWED, notSent would hide the earlier possible send.
            try:
                await self._fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        FailureKind.AVAILABILITY,
                        "sendCampaign",
                        "the no-send checkpoint could not be recorded; nothing was sent in this attempt",
                    )
                )
        return MutationAttempt.retry(retry_failure, retry_after)

    def _refuse_or_select_uncertain(
        self, branch: str, output: SendCampaignOutput, refusal: Failure, receipt: Receipt
    ) -> MutationAttempt[SendCampaignOutput]:
        """Select branch only when no earlier attempt may have sent the campaign."""
        if self._has_possible_earlier_send:
            return MutationAttempt.uncertain(
                output,
                failure(
                    refusal.kind,
                    "sendCampaign",
                    "an earlier attempt may have sent the campaign and Mailchimp does not show it "
                    "sending or sent: " + refusal.message,
                ),
                receipt,
            )
        return MutationAttempt.branch_result(branch, output, receipt, refusal)


def _has_legacy_dispatch(call: Call) -> bool:
    """Report a pre-fence checkpoint of this call; such a Step execution never sends again."""
    context = call.context
    if context is None:
        return False
    try:
        if not context.has_last_heartbeat_value():
            return False
        marker = context.get_last_heartbeat_value(_LegacySendDispatchMarker)
    except Exception:
        return False  # Not a pre-fence checkpoint; the Fence judges it.
    return isinstance(marker, _LegacySendDispatchMarker) and marker.dispatched_call_id == call.id


async def _read_back_without_send(
    client: Client, call: Call, input: SendCampaignInput, reason: str
) -> MutationAttempt[SendCampaignOutput]:
    """Never send; report alreadySent only when Mailchimp shows the campaign sending or sent."""
    empty = SendCampaignOutput(Campaign(status=""))
    connection = await client.resolve_credentials(call, "sendCampaign")
    if not isinstance(connection, tuple):
        return MutationAttempt.uncertain(
            empty,
            failure(
                FailureKind.AUTHENTICATION,
                "sendCampaign",
                reason + ", and the connection credentials are unavailable to read the campaign; "
                "reconnect the connection",
            ),
            Receipt(),
        )
    exchange = await client.exchange(
        call,
        "sendCampaign",
        connection,
        "GET",
        f"/campaigns/{input.campaign_id}",
        query={"fields": _CAMPAIGN_FIELDS},
    )
    receipt = client.receipt(call, exchange, input.campaign_id)
    if exchange.outcome in ("rateLimited", "notSent", "unavailable"):
        # Reading is harmless to repeat, and a retry of this path never sends.
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    if exchange.outcome == "succeeded":
        try:
            campaign = _decode_campaign(exchange.body, input.campaign_id)
        except (ValueError, TypeError, KeyError):
            pass  # An undecodable campaign cannot confirm the earlier send.
        else:
            output = SendCampaignOutput(campaign)
            if campaign.status in ("sending", "sent"):
                return MutationAttempt.branch_result("alreadySent", output, receipt)
            return MutationAttempt.uncertain(
                output,
                failure(
                    FailureKind.TRANSPORT,
                    "sendCampaign",
                    reason + " and Mailchimp does not show it sending or sent, so it is not sent again",
                ),
                receipt,
            )
    return MutationAttempt.uncertain(
        empty,
        failure(
            FailureKind.TRANSPORT,
            "sendCampaign",
            reason + " and its status could not be read, so it is not sent again",
        ),
        receipt,
    )


def _campaign_read_branch(outcome: str) -> str:
    """Map a definite read failure before a send; nothing was sent by this attempt."""
    return {"notFound": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
        outcome, "providerRejected"
    )
