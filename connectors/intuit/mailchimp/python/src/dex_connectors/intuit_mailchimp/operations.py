# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mailchimp operation invocations and exact provider outcome routing."""

import hashlib
import json
import math
import re
import unicodedata
from collections.abc import Iterable
from datetime import UTC, datetime
from email.utils import parseaddr
from typing import Any

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
from dex_connectors.sdk.singledispatch import Marker, State

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

_RESOURCE_ID = re.compile(r"[A-Za-z0-9]{1,64}\Z")
_SUBSCRIBER_HASH = re.compile(r"[0-9a-f]{32}\Z")
_MERGE_TAG = re.compile(r"[A-Z0-9_]{1,50}\Z")
_ADDRESS_KEY = re.compile(r"[a-z][a-z0-9_]{0,31}\Z")
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
_WRITABLE_STATUSES = frozenset(("subscribed", "pending", "unsubscribed", "transactional"))
_READABLE_STATUSES = _WRITABLE_STATUSES | {"cleaned", "archived"}


def _resource_id(name: str, value: str) -> None:
    if not _RESOURCE_ID.fullmatch(value):
        raise ValueError(f"{name} must be a Mailchimp ID of 1 to 64 letters and digits")


def _member_address(list_id: str, email_address: str) -> None:
    _resource_id("listId", list_id)
    parsed_name, parsed_address = parseaddr(email_address, strict=True)
    if (
        not email_address
        or len(email_address.encode()) > 254
        or parsed_name
        or parsed_address != email_address
        or not parsed_address.partition("@")[0]
    ):
        raise ValueError("emailAddress must be one bare email address such as jane@example.com")


def _subscriber_hash(email_address: str) -> str:
    return hashlib.md5(email_address.lower().encode()).hexdigest()  # noqa: S324 -- Mailchimp's address key.


def _parse_time(value: object) -> datetime | None:
    if value in ("", None):
        return None
    if not isinstance(value, str):
        raise ValueError("a timestamp is not ISO 8601")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
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


def _decode_member(wire: dict[str, Any], expected_hash: str = "") -> Member:
    subscriber_hash = wire.get("id", "")
    if not isinstance(subscriber_hash, str) or not _SUBSCRIBER_HASH.fullmatch(subscriber_hash):
        raise ValueError("the contact has no subscriber hash")
    if expected_hash and subscriber_hash != expected_hash:
        raise ValueError("the contact's subscriber hash does not match the requested address")
    if not wire.get("email_address") or not wire.get("status"):
        raise ValueError("the contact has no email address or status")
    tags = wire.get("tags") or []
    return Member(
        subscriber_hash=subscriber_hash,
        email_address=wire["email_address"],
        contact_id=wire.get("contact_id") or "",
        full_name=wire.get("full_name") or "",
        status=wire["status"],
        email_type=wire.get("email_type") or "",
        merge_fields=wire.get("merge_fields") or {},
        tags=tuple(tag.get("name", "") for tag in tags),
        tag_count=wire.get("tags_count") or 0,
        is_vip=wire.get("vip") or False,
        language=wire.get("language") or "",
        list_id=wire.get("list_id") or "",
        web_id=wire.get("web_id") or 0,
        signed_up_at=_parse_time(wire.get("timestamp_signup")),
        opted_in_at=_parse_time(wire.get("timestamp_opt")),
        last_changed_at=_parse_time(wire.get("last_changed")),
    )


def _decode_campaign(body: bytes, campaign_id: str) -> Campaign:
    wire = _decode_object(body)
    if wire.get("id") != campaign_id or not wire.get("status") or not wire.get("type"):
        raise ValueError("the campaign has another ID, or no status or type")
    settings = wire.get("settings") or {}
    recipients = wire.get("recipients") or {}
    return Campaign(
        id=campaign_id,
        web_id=wire.get("web_id") or 0,
        type=wire["type"],
        status=wire["status"],
        title=settings.get("title") or "",
        subject_line=settings.get("subject_line") or "",
        list_id=recipients.get("list_id") or "",
        list_name=recipients.get("list_name") or "",
        recipient_count=recipients.get("recipient_count") or 0,
        emails_sent=wire.get("emails_sent") or 0,
        created_at=_parse_time(wire.get("create_time")),
        sent_at=_parse_time(wire.get("send_time")),
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


def _mutation_exchange(
    exchange: Exchange,
    receipt: Receipt,
    empty: Any,
    *,
    has_not_found: bool = True,
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rateLimited", "notSent", "unavailable"):
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
            members = page.get("members") or []
            total_items = page.get("total_items")
            if not isinstance(total_items, int) or total_items < 0 or len(members) > page_size:
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
    """Read a campaign and send it once behind a Dex heartbeat checkpoint."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the stable sendCampaign contract."""
        return SEND_CAMPAIGN_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: SendCampaignInput) -> IdempotencyKey:
        """Correlate the send with its logical Call ID; Mailchimp has no deduplication key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: SendCampaignInput) -> MutationAttempt[SendCampaignOutput]:
        """Read, checkpoint, and at most once dispatch an approved draft."""
        empty = SendCampaignOutput(Campaign(status=""))
        try:
            _resource_id("campaignId", input.campaign_id)
            if input.expected_list_id:
                _resource_id("expectedListId", input.expected_list_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, "sendCampaign", str(error))
            )
        connection = await self._client.resolve_credentials(call, "sendCampaign")
        if not isinstance(connection, tuple):
            return MutationAttempt.branch_result("defect", empty, Receipt(), connection)
        context = call.context
        earlier_dispatch = False
        if context is not None:
            try:
                if context.has_last_heartbeat_value():
                    marker = context.get_last_heartbeat_value(Marker)
                    earlier_dispatch = not (
                        isinstance(marker, Marker)
                        and marker.call_id == call.id
                        and marker.state in (State.NOT_SENT, State.CAN_RESEND)
                    )
            except Exception:
                earlier_dispatch = True
        exchange = await self._client.exchange(
            call,
            "sendCampaign",
            connection,
            "GET",
            f"/campaigns/{input.campaign_id}",
            query={"fields": _CAMPAIGN_FIELDS},
        )
        receipt = self._client.receipt(call, exchange, input.campaign_id)
        if earlier_dispatch:
            try:
                campaign = _decode_campaign(exchange.body, input.campaign_id)
                if exchange.outcome == "succeeded":
                    output = SendCampaignOutput(campaign)
                    if campaign.status in ("sending", "sent"):
                        return MutationAttempt.branch_result("alreadySent", output, receipt)
                    return MutationAttempt.uncertain(
                        output,
                        failure(
                            FailureKind.TRANSPORT,
                            "sendCampaign",
                            "an earlier attempt may have sent the campaign; it is not sent again",
                        ),
                        receipt,
                    )
            except (ValueError, TypeError, KeyError):
                pass
            return MutationAttempt.uncertain(
                empty,
                failure(
                    FailureKind.TRANSPORT,
                    "sendCampaign",
                    "an earlier attempt may have sent the campaign and its status could not be read",
                ),
                receipt,
            )
        terminal = _mutation_exchange(exchange, receipt, empty)
        if terminal is not None:
            return terminal
        try:
            campaign = _decode_campaign(exchange.body, input.campaign_id)
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(FailureKind.PROTOCOL, "sendCampaign", "Mailchimp returned an invalid campaign"),
            )
        output = SendCampaignOutput(campaign)
        if campaign.status in ("sending", "sent"):
            return MutationAttempt.branch_result("alreadySent", output, receipt)
        if (
            campaign.status != "save"
            or campaign.type not in ("regular", "plaintext", "absplit", "variate")
            or (input.expected_list_id and campaign.list_id != input.expected_list_id)
        ):
            return MutationAttempt.branch_result(
                "notSendable",
                output,
                receipt,
                failure(
                    FailureKind.CONFLICT, "sendCampaign", "the campaign is not sendable; nothing was sent"
                ),
            )
        if context is None:
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    "sendCampaign",
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            )
        try:
            await context.heartbeat(Marker(call.id, context.attempt, State.CLAIMED))
        except Exception:
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    "sendCampaign",
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            )
        sent = await self._client.exchange(
            call, "sendCampaign", connection, "POST", f"/campaigns/{input.campaign_id}/actions/send"
        )
        send_receipt = self._client.receipt(call, sent, input.campaign_id)
        if sent.outcome == "succeeded":
            return MutationAttempt.branch_result("sent", output, send_receipt)
        if sent.outcome in ("rateLimited", "notSent"):
            try:
                await context.heartbeat(Marker(call.id, context.attempt, State.CAN_RESEND))
            except Exception:
                pass  # A retained claim fails closed on the next attempt.
            assert sent.failure is not None
            return MutationAttempt.retry(sent.failure, sent.retry_after)
        if sent.outcome in ("unavailable", "invalid"):
            assert sent.failure is not None
            return MutationAttempt.uncertain(output, sent.failure, send_receipt)
        if sent.outcome == "notFound":
            return MutationAttempt.branch_result("notFound", output, send_receipt, sent.failure)
        if sent.outcome == "defect":
            return MutationAttempt.branch_result("defect", output, send_receipt, sent.failure)
        return MutationAttempt.branch_result("providerRejected", output, send_receipt, sent.failure)
