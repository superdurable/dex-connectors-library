# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Slack Web API calls and Go-compatible Dex operation decisions."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    DEFECT,
    FOUND,
    GET_THREAD_REPLY_DEFINITION,
    GET_WORKSPACE_IDENTITY_DEFINITION,
    INVALID_RESPONSE,
    LIST_CHANNELS_DEFINITION,
    LIST_THREAD_MESSAGES_DEFINITION,
    LISTED,
    NOT_FOUND,
    POST_CHANNEL_MESSAGE_DEFINITION,
    POST_THREAD_REPLY_DEFINITION,
    PROVIDER_REJECTED,
    READ,
    SENT,
)
from .models import (
    Channel,
    GetThreadReplyInput,
    GetThreadReplyOutput,
    GetWorkspaceIdentityInput,
    GetWorkspaceIdentityOutput,
    ListChannelsInput,
    ListChannelsOutput,
    ListThreadMessagesInput,
    ListThreadMessagesOutput,
    Message,
    PostChannelMessageInput,
    PostMessageOutput,
    PostThreadReplyInput,
)


@dataclass(slots=True)
class _Response:
    status: int = 0
    headers: httpx.Headers | None = None
    decoded: dict[str, Any] = field(default_factory=dict)
    not_sent: bool = False
    error: str = ""


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "slack", operation, message)


def _encode_query(values: dict[str, str]) -> str:
    """Match Go url.Values.Encode for sorted query pairs."""
    return urlencode(sorted(values.items()))


def _query_branch(operation: str, branch: str, kind: FailureKind, message: str) -> QueryAttempt[Any]:
    return QueryAttempt.branch_result(
        branch, _empty_output(operation), Receipt(), _failure(operation, kind, message)
    )


def _empty_output(operation: str) -> Any:
    if operation == "listChannels":
        return ListChannelsOutput()
    if operation == "getWorkspaceIdentity":
        return GetWorkspaceIdentityOutput()
    return GetThreadReplyOutput() if operation == "getThreadReply" else ListThreadMessagesOutput()


def _message(channel: str, value: dict[str, Any]) -> Message:
    return Message(
        channel,
        value.get("ts") or "",
        value.get("thread_ts") or "",
        value.get("user") or "",
        value.get("text") or "",
    )


def _decoded_message(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("Slack message has an invalid shape")
    for field_name in ("channel", "ts", "thread_ts", "user", "text"):
        if field_name in value and value[field_name] is not None and not isinstance(value[field_name], str):
            raise ValueError("Slack message has an invalid field")
    return value


def _decode_body(body: bytes) -> dict[str, Any]:
    if not body:
        raise ValueError("Slack returned an empty response")
    try:
        decoded = json.loads(body, parse_constant=_reject_nonfinite)
    except (UnicodeError, ValueError) as error:
        raise ValueError("Slack returned malformed JSON") from error
    if decoded is None:
        return {}
    if not isinstance(decoded, dict):
        raise ValueError("Slack returned malformed JSON")
    for field_name in ("ok", "error", "ts", "channel"):
        expected = bool if field_name == "ok" else str
        if (
            field_name in decoded
            and decoded[field_name] is not None
            and not isinstance(decoded[field_name], expected)
        ):
            raise ValueError("Slack returned malformed JSON")
    if "messages" in decoded and decoded["messages"] is not None:
        if not isinstance(decoded["messages"], list):
            raise ValueError("Slack returned malformed JSON")
        for item in decoded["messages"]:
            _decoded_message(item)
    if "message" in decoded and decoded["message"] is not None:
        _decoded_message(decoded["message"])
    if "response_metadata" in decoded and decoded["response_metadata"] is not None:
        metadata = decoded["response_metadata"]
        if not isinstance(metadata, dict) or (
            "next_cursor" in metadata
            and metadata["next_cursor"] is not None
            and not isinstance(metadata["next_cursor"], str)
        ):
            raise ValueError("Slack returned malformed JSON")
    return decoded


def _reject_nonfinite(value: str) -> object:
    raise ValueError("Slack returned a non-finite JSON number")


def _rejection_kind(response: _Response) -> FailureKind:
    code = (response.decoded or {}).get("error", "")
    if code in ("invalid_auth", "not_authed", "token_expired", "token_revoked", "account_inactive"):
        return FailureKind.AUTHENTICATION
    if code in ("missing_scope", "no_permission"):
        return FailureKind.AUTHORIZATION
    if code in ("channel_not_found", "thread_not_found", "message_not_found"):
        return FailureKind.NOT_FOUND
    return {401: FailureKind.AUTHENTICATION, 403: FailureKind.AUTHORIZATION, 404: FailureKind.NOT_FOUND}.get(
        response.status, FailureKind.PROVIDER_REJECTION
    )


def _retry_after(response: _Response) -> float:
    raw = (response.headers or httpx.Headers()).get("Retry-After", "")
    return parse_retry_after(raw, datetime.now(UTC))


class Client:
    """Own provider requests and close the supplied transport at shutdown."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Any = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Slack configuration and credentials are required")
        config = Config(
            config.endpoint or Config().endpoint,
            config.max_response_bytes or Config().max_response_bytes,
            config.max_message_characters or Config().max_message_characters,
        )
        parsed = urlsplit(config.endpoint)
        if not parsed.scheme or not parsed.hostname:
            raise ValueError("Slack endpoint must be absolute")
        if parsed.scheme != "https" and parsed.hostname not in ("localhost", "127.0.0.1"):
            raise ValueError("Slack endpoint must use HTTPS")
        if config.max_response_bytes <= 0 or config.max_message_characters <= 0:
            raise ValueError("Slack limits must be positive")
        self.config = config
        self.endpoint = config.endpoint.rstrip("/")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the provider and generated OAuth refresh transport."""
        await self.http_client.aclose()

    def list_channels(self) -> ListChannelsOperation:
        """Bind the channel page Query."""
        return ListChannelsOperation(self)

    def get_workspace_identity(self) -> GetWorkspaceIdentityOperation:
        """Bind the workspace identity Query."""
        return GetWorkspaceIdentityOperation(self)

    def list_thread_messages(self) -> ListThreadMessagesOperation:
        """Bind the thread page Query."""
        return ListThreadMessagesOperation(self)

    def get_thread_reply(self) -> GetThreadReplyOperation:
        """Bind the reply lookup Query."""
        return GetThreadReplyOperation(self)

    def post_channel_message(self) -> PostChannelMessageOperation:
        """Bind the channel message Mutation."""
        return PostChannelMessageOperation(self)

    def post_thread_reply(self) -> PostThreadReplyOperation:
        """Bind the thread reply Mutation."""
        return PostThreadReplyOperation(self)

    async def _resolve(self, call: Call, token_field: str) -> tuple[Credentials | None, bool]:
        try:
            credentials = await self.credentials.resolve(call)
        except Exception as error:
            return None, isinstance(error, ReauthorizationRequiredError)
        if not getattr(credentials, token_field).reveal():
            return None, True
        return credentials, False

    async def _request(
        self,
        call: Call,
        credentials: Credentials,
        token_field: str,
        method: str,
        api_method: str,
        *,
        query: dict[str, str] | None = None,
        payload: dict[str, str] | None = None,
        form: dict[str, str] | None = None,
    ) -> _Response:
        url = self.endpoint + "/" + api_method
        if query:
            url += "?" + _encode_query(query)
        headers = {"Authorization": "Bearer " + getattr(credentials, token_field).reveal()}
        content: str | None = None
        if payload is not None:
            headers["Content-Type"] = "application/json; charset=utf-8"
            content = json.dumps(payload, separators=(",", ":"))
        elif form is not None:
            headers["Content-Type"] = "application/x-www-form-urlencoded"
            content = _encode_query(form)
        request = self.http_client.build_request(method, url, headers=headers, content=content)
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return _Response(not_sent=error.provably_not_sent, error="transport")
        result = _Response(response.status_code, response.headers)
        try:
            body = await read_bounded_body(response, self.config.max_response_bytes)
        except BodyTooLargeError:
            result.error = "too_large"
            return result
        except ProviderTransportError:
            result.error = "transport"
            return result
        try:
            result.decoded = _decode_body(body)
        except ValueError:
            result.error = "invalid"
        return result

    async def _get(
        self,
        call: Call,
        credentials: Credentials,
        query: dict[str, str],
        token_field: str = "user_token",
        api_method: str = "conversations.replies",
    ) -> _Response:
        return await self._read_with_refresh(call, credentials, token_field, "GET", api_method, query=query)

    async def _post_form(
        self, call: Call, credentials: Credentials, form: dict[str, str], token_field: str, api_method: str
    ) -> _Response:
        """Send a form-encoded read, such as auth.test, and refresh a rejected credential once."""
        return await self._read_with_refresh(call, credentials, token_field, "POST", api_method, form=form)

    async def _read_with_refresh(
        self,
        call: Call,
        credentials: Credentials,
        token_field: str,
        method: str,
        api_method: str,
        *,
        query: dict[str, str] | None = None,
        form: dict[str, str] | None = None,
    ) -> _Response:
        for attempt in range(2):
            result = await self._request(
                call, credentials, token_field, method, api_method, query=query, form=form
            )
            if result.error or not _credential_rejected(result) or attempt:
                return result
            refresh = getattr(self.credentials, "resolve_after_rejection", None)
            if refresh is None:
                return result
            try:
                replacement = await refresh(call)
            except Exception:
                return result
            if not getattr(replacement, token_field).reveal():
                return result
            credentials = replacement
        return result

    def _receipt(self, call: Call, response: _Response, object_id: str = "") -> Receipt:
        return Receipt(
            call.id,
            "slack",
            self.now(),
            call.idempotency_key,
            object_id,
            (response.headers or httpx.Headers()).get("X-Slack-Req-Id", ""),
        )


def _credential_rejected(response: _Response) -> bool:
    return response.status == 401 or (response.decoded or {}).get("error") in (
        "invalid_auth",
        "token_expired",
        "token_revoked",
    )


class ListThreadMessagesOperation:
    """Read one bounded Slack thread page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation definition."""
        return LIST_THREAD_MESSAGES_DEFINITION

    async def invoke(
        self, call: Call, input: ListThreadMessagesInput
    ) -> QueryAttempt[ListThreadMessagesOutput]:
        """Validate, read, and classify one provider attempt."""
        operation = "listThreadMessages"
        if (
            not input.channel_id.strip()
            or not input.thread_timestamp.strip()
            or not 1 <= input.page_size <= 15
        ):
            return _query_branch(
                operation,
                DEFECT,
                FailureKind.VALIDATION,
                "channel, thread timestamp, and page size from 1 through 15 are required",
            )
        credentials, _ = await self.client._resolve(call, "user_token")
        if credentials is None:
            return _query_branch(
                operation, DEFECT, FailureKind.AUTHENTICATION, "Slack connection credentials are unavailable"
            )
        query = {"channel": input.channel_id, "ts": input.thread_timestamp, "limit": str(input.page_size)}
        if input.cursor:
            query["cursor"] = input.cursor
        response = await self.client._get(call, credentials, query)
        classified = _classify_query_response(
            self.client,
            call,
            operation,
            response,
            ListThreadMessagesOutput(),
            "Slack thread messages are temporarily unavailable",
            "Slack rate limited the thread query",
        )
        if classified is not None:
            return classified
        output = ListThreadMessagesOutput(
            tuple(
                _message(input.channel_id, message) for message in (response.decoded.get("messages") or [])
            ),
            (response.decoded.get("response_metadata") or {}).get("next_cursor") or "",
        )
        return QueryAttempt.branch_result(READ, output, self.client._receipt(call, response))


class GetThreadReplyOperation:
    """Find a matching reply in one bounded provider page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation definition."""
        return GET_THREAD_REPLY_DEFINITION

    async def invoke(self, call: Call, input: GetThreadReplyInput) -> QueryAttempt[GetThreadReplyOutput]:
        """Validate, read, and classify one provider attempt."""
        operation = "getThreadReply"
        if (
            not input.channel_id.strip()
            or not input.thread_timestamp.strip()
            or not input.reply_timestamp.strip()
            or input.reply_timestamp == input.thread_timestamp
        ):
            return _query_branch(
                operation,
                DEFECT,
                FailureKind.VALIDATION,
                "channel, thread timestamp, and a distinct reply timestamp are required",
            )
        credentials, _ = await self.client._resolve(call, "user_token")
        if credentials is None:
            return _query_branch(
                operation, DEFECT, FailureKind.AUTHENTICATION, "Slack connection credentials are unavailable"
            )
        query = {
            "channel": input.channel_id,
            "ts": input.thread_timestamp,
            "oldest": input.reply_timestamp,
            "latest": input.reply_timestamp,
            "inclusive": "true",
            "limit": "1",
        }
        response = await self.client._get(call, credentials, query)
        classified = _classify_query_response(
            self.client,
            call,
            operation,
            response,
            GetThreadReplyOutput(),
            "Slack thread reply is temporarily unavailable",
            "Slack rate limited the reply query",
        )
        if classified is not None:
            return classified
        for message in response.decoded.get("messages") or []:
            if (
                message.get("ts") == input.reply_timestamp
                and message.get("thread_ts") == input.thread_timestamp
            ):
                return QueryAttempt.branch_result(
                    FOUND,
                    GetThreadReplyOutput(_message(input.channel_id, message)),
                    self.client._receipt(call, response, message["ts"]),
                )
        return QueryAttempt.branch_result(
            NOT_FOUND,
            GetThreadReplyOutput(),
            self.client._receipt(call, response),
            _failure(operation, FailureKind.NOT_FOUND, "Slack thread reply was not found"),
        )


_CHANNEL_TYPES = {
    "": "public_channel,private_channel",
    "public": "public_channel",
    "private": "private_channel",
}


def _optional_value(value: dict[str, Any], field_name: str, expected: type) -> Any:
    """Return a Go-decodable optional JSON field, treating null as the zero value."""
    item = value.get(field_name)
    if item is None:
        return expected()
    if not isinstance(item, expected):
        raise ValueError("Slack field has an invalid type")
    return item


def _decoded_channels(decoded: dict[str, Any]) -> list[dict[str, Any]]:
    """Match Go decoding of conversations.list and users.conversations channels."""
    channels = decoded.get("channels")
    if channels is None:
        return []
    if not isinstance(channels, list):
        raise ValueError("Slack channels have an invalid shape")
    result = []
    for channel in channels:
        if channel is None:
            channel = {}
        if not isinstance(channel, dict):
            raise ValueError("Slack channel has an invalid shape")
        topic = _optional_value(channel, "topic", dict)
        purpose = _optional_value(channel, "purpose", dict)
        result.append(
            {
                "id": _optional_value(channel, "id", str),
                "name": _optional_value(channel, "name", str),
                "is_private": _optional_value(channel, "is_private", bool),
                "is_member": _optional_value(channel, "is_member", bool),
                "topic": _optional_value(topic, "value", str),
                "purpose": _optional_value(purpose, "value", str),
            }
        )
    return result


def _channel_name_filter(name_filter: str) -> str:
    """Trim spaces and one leading # and lowercase the filter."""
    trimmed = name_filter.strip()
    return (trimmed[1:] if trimmed.startswith("#") else trimmed).lower()


def _classify_query_response(
    client: Client,
    call: Call,
    operation: str,
    response: _Response,
    empty: Any,
    unavailable_message: str,
    rate_limited_message: str,
) -> QueryAttempt[Any] | None:
    """Select the status, transport, and Slack envelope outcome shared by every Slack query.

    Status comes first because Slack may send 429 or 5xx without a JSON body.
    """
    if response.status == 429:
        return QueryAttempt.retry(
            _failure(operation, FailureKind.RATE_LIMIT, rate_limited_message), _retry_after(response)
        )
    if response.status >= 500:
        return QueryAttempt.retry(
            _failure(operation, FailureKind.AVAILABILITY, unavailable_message), _retry_after(response)
        )
    if response.error in ("invalid", "too_large"):
        kind = FailureKind.PROTOCOL if response.error == "invalid" else FailureKind.RESPONSE_TOO_LARGE
        return QueryAttempt.branch_result(
            INVALID_RESPONSE,
            empty,
            client._receipt(call, response),
            _failure(operation, kind, "Slack returned an invalid response"),
        )
    if response.error:
        return QueryAttempt.retry(_failure(operation, FailureKind.AVAILABILITY, unavailable_message))
    if not 200 <= response.status < 300 or not response.decoded.get("ok", False):
        return QueryAttempt.branch_result(
            PROVIDER_REJECTED,
            empty,
            client._receipt(call, response),
            _failure(operation, _rejection_kind(response), "Slack rejected the request"),
        )
    return None


class ListChannelsOperation:
    """Read one Slack channel page with the bot token and filter it by name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation definition."""
        return LIST_CHANNELS_DEFINITION

    async def invoke(self, call: Call, input: ListChannelsInput) -> QueryAttempt[ListChannelsOutput]:
        """Validate, read, filter, and classify one provider attempt."""
        operation = "listChannels"
        channel_types = _CHANNEL_TYPES.get(input.visibility)
        if channel_types is None or not 0 <= input.limit <= 999:
            return _query_branch(
                operation,
                DEFECT,
                FailureKind.VALIDATION,
                "visibility must be blank, public, or private, and limit must be from 0 through 999",
            )
        credentials, _ = await self.client._resolve(call, "bot_token")
        if credentials is None:
            return _query_branch(
                operation, DEFECT, FailureKind.AUTHENTICATION, "Slack connection credentials are unavailable"
            )
        query = {"exclude_archived": "true", "types": channel_types, "limit": str(input.limit or 200)}
        if input.cursor:
            query["cursor"] = input.cursor
        # users.conversations lists only joined channels and omits is_member.
        api_method = "users.conversations" if input.member_only else "conversations.list"
        response = await self.client._get(call, credentials, query, "bot_token", api_method)
        classified = _classify_query_response(
            self.client,
            call,
            operation,
            response,
            ListChannelsOutput(),
            "Slack channels are temporarily unavailable",
            "Slack rate limited the channel query",
        )
        if classified is not None:
            return classified
        try:
            channels = _decoded_channels(response.decoded)
        except ValueError:
            channels = None
        if channels is None or any(not channel["id"] for channel in channels):
            return QueryAttempt.branch_result(
                INVALID_RESPONSE,
                ListChannelsOutput(),
                self.client._receipt(call, response),
                _failure(operation, FailureKind.PROTOCOL, "Slack returned an invalid channel list"),
            )
        name_filter = _channel_name_filter(input.name_filter)
        output = ListChannelsOutput(
            tuple(
                Channel(
                    channel["id"],
                    channel["name"],
                    channel["is_private"],
                    channel["is_member"] or input.member_only,
                    channel["topic"],
                    channel["purpose"],
                )
                for channel in channels
                if not name_filter or name_filter in channel["name"].lower()
            ),
            (response.decoded.get("response_metadata") or {}).get("next_cursor") or "",
        )
        return QueryAttempt.branch_result(LISTED, output, self.client._receipt(call, response))


class GetWorkspaceIdentityOperation:
    """Read the bot token's workspace and identity with a POST to auth.test, as Slack documents it."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation definition."""
        return GET_WORKSPACE_IDENTITY_DEFINITION

    async def invoke(
        self, call: Call, input: GetWorkspaceIdentityInput
    ) -> QueryAttempt[GetWorkspaceIdentityOutput]:
        """Read and classify one provider attempt."""
        operation = "getWorkspaceIdentity"
        credentials, _ = await self.client._resolve(call, "bot_token")
        if credentials is None:
            return _query_branch(
                operation, DEFECT, FailureKind.AUTHENTICATION, "Slack connection credentials are unavailable"
            )
        response = await self.client._post_form(call, credentials, {}, "bot_token", "auth.test")
        classified = _classify_query_response(
            self.client,
            call,
            operation,
            response,
            GetWorkspaceIdentityOutput(),
            "Slack workspace identity is temporarily unavailable",
            "Slack rate limited the identity query",
        )
        if classified is not None:
            return classified
        try:
            decoded = response.decoded
            output = GetWorkspaceIdentityOutput(
                _optional_value(decoded, "team_id", str),
                _optional_value(decoded, "team", str),
                _optional_value(decoded, "url", str),
                _optional_value(decoded, "enterprise_id", str),
                _optional_value(decoded, "user_id", str),
                _optional_value(decoded, "user", str),
                _optional_value(decoded, "bot_id", str),
            )
        except ValueError:
            output = None
        if output is None or not output.bot_user_id or not (output.team_id or output.enterprise_id):
            return QueryAttempt.branch_result(
                INVALID_RESPONSE,
                GetWorkspaceIdentityOutput(),
                self.client._receipt(call, response),
                _failure(operation, FailureKind.PROTOCOL, "Slack returned an invalid workspace identity"),
            )
        return QueryAttempt.branch_result(READ, output, self.client._receipt(call, response, output.team_id))


class PostChannelMessageOperation:
    """Post a top-level channel message with durable single dispatch."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest operation definition."""
        return POST_CHANNEL_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostChannelMessageInput) -> IdempotencyKey:
        """Use the stable Call ID as Slack client message identity."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PostChannelMessageInput) -> MutationAttempt[PostMessageOutput]:
        """Dispatch or read back one channel message."""
        return await _post_message(self.client, call, "postChannelMessage", input.channel_id, "", input.text)


class PostThreadReplyOperation:
    """Post a reply with durable single dispatch."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest operation definition."""
        return POST_THREAD_REPLY_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostThreadReplyInput) -> IdempotencyKey:
        """Use the stable Call ID as Slack client message identity."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PostThreadReplyInput) -> MutationAttempt[PostMessageOutput]:
        """Dispatch or read back one thread reply."""
        return await _post_message(
            self.client, call, "postThreadReply", input.channel_id, input.thread_timestamp, input.text
        )


async def _read_back(
    client: Client, call: Call, operation: str, channel: str, thread: str, object_id: str
) -> MutationAttempt[PostMessageOutput]:
    if not object_id:
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(operation, FailureKind.TRANSPORT, "an earlier Slack message cannot be identified"),
            client._receipt(call, _Response()),
        )
    credentials, terminal = await client._resolve(call, "user_token")
    if credentials is None:
        if terminal:
            return MutationAttempt.uncertain(
                PostMessageOutput(),
                _failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Slack message read-back credentials are unavailable; "
                    "reconnect the connection to confirm the post",
                ),
                client._receipt(call, _Response(), object_id),
            )
        return MutationAttempt.retry(
            _failure(
                operation, FailureKind.AVAILABILITY, "Slack message read-back credentials are unavailable"
            )
        )
    query = {
        "channel": channel,
        "ts": thread or object_id,
        "oldest": object_id,
        "latest": object_id,
        "inclusive": "true",
        "limit": "1",
    }
    response = await client._get(call, credentials, query)
    if response.error in ("invalid", "too_large"):
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(
                operation, FailureKind.PROTOCOL, "Slack message read-back returned an unusable response"
            ),
            client._receipt(call, response, object_id),
        )
    if response.error or response.status == 429 or response.status >= 500:
        return MutationAttempt.retry(
            _failure(
                operation, FailureKind.AVAILABILITY, "Slack message read-back is temporarily unavailable"
            ),
            _retry_after(response),
        )
    if not 200 <= response.status < 300 or not response.decoded.get("ok", False):
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(operation, FailureKind.PROTOCOL, "Slack message read-back could not confirm the post"),
            client._receipt(call, response, object_id),
        )
    for message in response.decoded.get("messages") or []:
        if message.get("ts") == object_id and (not thread or message.get("thread_ts") == thread):
            return MutationAttempt.branch_result(
                SENT,
                PostMessageOutput(_message(channel, message)),
                client._receipt(call, response, object_id),
            )
    return MutationAttempt.uncertain(
        PostMessageOutput(),
        _failure(operation, FailureKind.PROTOCOL, "Slack message read-back did not find the post"),
        client._receipt(call, response, object_id),
    )


async def _post_message(
    client: Client, call: Call, operation: str, channel: str, thread: str, text: str
) -> MutationAttempt[PostMessageOutput]:
    if not channel.strip() or not text.strip() or len(text) > client.config.max_message_characters:
        return MutationAttempt.branch_result(
            DEFECT,
            PostMessageOutput(),
            Receipt(),
            _failure(operation, FailureKind.VALIDATION, "channel and bounded non-empty text are required"),
        )
    try:
        fence = Fence(call, timedelta(seconds=30))
        decision = await fence.decide()
    except ValueError:
        return MutationAttempt.branch_result(
            DEFECT,
            PostMessageOutput(),
            Receipt(),
            _failure(operation, FailureKind.LOCAL_DEFECT, "Slack dispatch fence could not be constructed"),
        )
    if decision.kind == DecisionKind.FAIL_CLOSED:
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(
                operation, FailureKind.TRANSPORT, "an earlier attempt may have posted the Slack message"
            ),
            client._receipt(call, _Response()),
        )
    if decision.kind == DecisionKind.READ_BACK_REQUIRED:
        return await _read_back(
            client, call, operation, channel, thread, decision.marker.provider_id if decision.marker else ""
        )
    credentials, terminal = await client._resolve(call, "bot_token")
    if credentials is None:
        try:
            await fence.record_not_sent()
        except CheckpointError:
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Slack credentials and no-send checkpoint are unavailable",
                )
            )
        if terminal:
            return MutationAttempt.branch_result(
                DEFECT,
                PostMessageOutput(),
                Receipt(),
                _failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Slack connection credentials are unavailable; reconnect the connection",
                ),
            )
        return MutationAttempt.retry(
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "Slack credentials are temporarily unavailable; no message was sent",
            )
        )
    payload = {"channel": channel, "text": text, "client_msg_id": str(call.idempotency_key)}
    if thread:
        payload["thread_ts"] = thread
    try:
        response = await fence.dispatch(
            lambda deadline: client._request(
                call, credentials, "bot_token", "POST", "chat.postMessage", payload=payload
            )
        )
    except (CheckpointError, InsufficientAttemptTimeError):
        return MutationAttempt.retry(
            _failure(operation, FailureKind.AVAILABILITY, "Slack dispatch claim could not be recorded")
        )
    except TimeoutError:
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(operation, FailureKind.TRANSPORT, "Slack message outcome is unknown"),
            client._receipt(call, _Response()),
        )
    if response.status in (429, 503):
        try:
            await fence.record_can_resend()
        except CheckpointError:
            return MutationAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "Slack retry checkpoint could not be recorded")
            )
        kind = FailureKind.RATE_LIMIT if response.status == 429 else FailureKind.AVAILABILITY
        message = (
            "Slack rate limited the message"
            if response.status == 429
            else "Slack message service is temporarily unavailable"
        )
        return MutationAttempt.retry(_failure(operation, kind, message), _retry_after(response))
    if response.status == 401 or (
        not response.error
        and 200 <= response.status < 300
        and not response.decoded.get("ok", False)
        and _credential_rejected(response)
    ):
        refresh = getattr(client.credentials, "resolve_after_rejection", None)
        replacement = None
        if refresh is not None:
            try:
                replacement = await refresh(call)
            except Exception:
                pass  # A failed refresh leaves Slack's rejection terminal for this attempt.
        if (
            replacement is None
            or not replacement.bot_token.reveal()
            or (replacement.bot_token.reveal() == credentials.bot_token.reveal())
        ):
            return MutationAttempt.branch_result(
                PROVIDER_REJECTED,
                PostMessageOutput(),
                client._receipt(call, response),
                _failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Slack rejected the connection credential; reconnect the connection",
                ),
            )
        try:
            await fence.record_can_resend()
        except CheckpointError:
            return MutationAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "Slack retry checkpoint could not be recorded")
            )
        return MutationAttempt.retry(
            _failure(
                operation,
                FailureKind.AUTHENTICATION,
                "Slack connection credential was refreshed after rejection",
            )
        )
    if response.error:
        if response.not_sent:
            try:
                await fence.record_can_resend()
            except CheckpointError:
                return MutationAttempt.retry(
                    _failure(
                        operation, FailureKind.AVAILABILITY, "Slack retry checkpoint could not be recorded"
                    )
                )
            return MutationAttempt.retry(
                _failure(operation, FailureKind.TRANSPORT, "Slack message request did not reach the provider")
            )
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(operation, FailureKind.TRANSPORT, "Slack message outcome is unknown"),
            client._receipt(call, response),
        )
    if response.status == 409:
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "Slack returned a conflict without a confirmable message identity",
            ),
            client._receipt(call, response),
        )
    if not response.decoded.get("ok", False):
        code = response.decoded.get("error", "")
        if code in ("service_unavailable", "ratelimited") and 200 <= response.status < 300:
            try:
                await fence.record_can_resend()
            except CheckpointError:
                return MutationAttempt.retry(
                    _failure(
                        operation, FailureKind.AVAILABILITY, "Slack retry checkpoint could not be recorded"
                    )
                )
            kind = FailureKind.RATE_LIMIT if code == "ratelimited" else FailureKind.AVAILABILITY
            return MutationAttempt.retry(
                _failure(operation, kind, "Slack did not process the message"), _retry_after(response)
            )
        if code in ("internal_error", "fatal_error"):
            provider_message = response.decoded.get("message") or {}
            object_id = provider_message.get("ts", "") or response.decoded.get("ts", "")
            if object_id:
                try:
                    await fence.record_provider_id(object_id)
                except CheckpointError:
                    pass  # Read-back still uses the provider ID from this response.
                return await _read_back(client, call, operation, channel, thread, object_id)
            return MutationAttempt.uncertain(
                PostMessageOutput(),
                _failure(operation, FailureKind.AVAILABILITY, "Slack message outcome is unknown"),
                client._receipt(call, response),
            )
    if response.status >= 500:
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(operation, FailureKind.AVAILABILITY, "Slack message outcome is unknown"),
            client._receipt(call, response),
        )
    if not 200 <= response.status < 300 or not response.decoded.get("ok", False):
        return MutationAttempt.branch_result(
            PROVIDER_REJECTED,
            PostMessageOutput(),
            client._receipt(call, response),
            _failure(operation, _rejection_kind(response), "Slack rejected the request"),
        )
    provider_message = response.decoded.get("message") or {}
    object_id = provider_message.get("ts", "") or response.decoded.get("ts", "")
    provider_channel = provider_message.get("channel", "") or response.decoded.get("channel", "")
    if object_id:
        try:
            await fence.record_provider_id(object_id)
        except CheckpointError:
            pass  # The accepted response still identifies the message.
    if not object_id or not provider_channel:
        if object_id:
            return await _read_back(client, call, operation, channel, thread, object_id)
        return MutationAttempt.uncertain(
            PostMessageOutput(),
            _failure(operation, FailureKind.PROTOCOL, "Slack returned an invalid message response"),
            client._receipt(call, response),
        )
    provider_message = {**provider_message, "ts": object_id}
    return MutationAttempt.branch_result(
        SENT,
        PostMessageOutput(_message(provider_channel, provider_message)),
        client._receipt(call, response, object_id),
    )
