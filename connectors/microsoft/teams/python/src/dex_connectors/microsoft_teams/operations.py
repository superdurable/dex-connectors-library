# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Teams Query and single-dispatch Mutation operations."""

import json
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import urlsplit

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
    ReauthorizationRequiredError,
    Receipt,
    RejectedCredentialRefreshingProvider,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions
from .client import (
    CHAT_ID,
    MESSAGE_ID,
    Client,
    Exchange,
    channel_messages_path,
    decode_message,
    extract_message_text,
    safe_token_url,
    thread_replies_path,
    valid_message_id,
    valid_team_channel,
)
from .models import (
    ListThreadRepliesInput,
    ListThreadRepliesOutput,
    Message,
    PostChannelMessageInput,
    PostChatMessageInput,
    PostMessageOutput,
    PostThreadReplyInput,
)


def _empty_receipt() -> Receipt:
    return Receipt()


def _go_json_body(value: object) -> bytes:
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False)
    for character, escaped in (
        ("<", "\\u003c"),
        (">", "\\u003e"),
        ("&", "\\u0026"),
        ("\u2028", "\\u2028"),
        ("\u2029", "\\u2029"),
    ):
        encoded = encoded.replace(character, escaped)
    return encoded.encode()


def _failure(client: Client, operation: str, kind: FailureKind, message: str) -> Failure:
    return client.failure(operation, kind, message)


def _status_failure_kind(status: int) -> FailureKind:
    return {
        400: FailureKind.VALIDATION,
        413: FailureKind.VALIDATION,
        422: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        410: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        412: FailureKind.CONFLICT,
        429: FailureKind.RATE_LIMIT,
    }.get(status, FailureKind.PROVIDER_REJECTION)


def _rejection(client: Client, operation: str, subject: str, exchange: Exchange) -> Failure:
    message = (
        f"Microsoft Graph rejected the {subject} with HTTP {exchange.status}"
        f"{client.error_code_suffix(exchange)}"
    )
    if exchange.status == 401:
        message += "; reconnect the connection"
    elif exchange.status == 403:
        message += (
            "; the connected account must belong to the team, channel, or chat, and the app needs "
            "every requested Graph permission, with administrator consent for ChannelMessage.Read.All"
        )
    return _failure(client, operation, _status_failure_kind(exchange.status), message)


def _read_classification(
    client: Client, operation: str, subject: str, exchange: Exchange
) -> tuple[str, Failure | None, float]:
    if exchange.error is not None:
        return (
            "retry",
            _failure(
                client,
                operation,
                FailureKind.TRANSPORT,
                f"Microsoft Graph {subject} is temporarily unavailable",
            ),
            0,
        )
    status = exchange.status
    if status == 404:
        return "notFound", _rejection(client, operation, subject, exchange), 0
    if status == 429:
        return (
            "retry",
            _failure(client, operation, FailureKind.RATE_LIMIT, f"Microsoft Graph throttled the {subject}"),
            client.retry_after(exchange),
        )
    if status == 408 or (status >= 500 and status != 501):
        return (
            "retry",
            _failure(
                client,
                operation,
                FailureKind.AVAILABILITY,
                f"Microsoft Graph {subject} is temporarily unavailable "
                f"(HTTP {status}{client.error_code_suffix(exchange)})",
            ),
            client.retry_after(exchange),
        )
    if status >= 400:
        return "providerRejected", _rejection(client, operation, subject, exchange), 0
    if not 200 <= status < 300:
        return (
            "invalidResponse",
            _failure(
                client,
                operation,
                FailureKind.PROTOCOL,
                f"Microsoft Graph returned unexpected HTTP {status} for the {subject}",
            ),
            0,
        )
    if exchange.body_read_failed:
        return (
            "retry",
            _failure(
                client,
                operation,
                FailureKind.TRANSPORT,
                f"Microsoft Graph {subject} response was interrupted",
            ),
            0,
        )
    if exchange.body_too_large:
        return (
            "invalidResponse",
            _failure(
                client,
                operation,
                FailureKind.RESPONSE_TOO_LARGE,
                f"Microsoft Graph {subject} response exceeds the configured size limit",
            ),
            0,
        )
    if exchange.reflects_secret:
        return (
            "invalidResponse",
            _failure(
                client,
                operation,
                FailureKind.PROTOCOL,
                f"Microsoft Graph {subject} response contains credential material",
            ),
            0,
        )
    return "read", None, 0


def _write_classification(
    client: Client, operation: str, subject: str, exchange: Exchange
) -> tuple[str, Failure | None, float]:
    if exchange.error is not None:
        if not exchange.dispatched:
            return (
                "notApplied",
                _failure(
                    client,
                    operation,
                    FailureKind.TRANSPORT,
                    f"Microsoft Graph could not be reached, so no {subject} was sent",
                ),
                0,
            )
        return (
            "ambiguous",
            _failure(
                client, operation, FailureKind.TRANSPORT, f"Microsoft Graph {subject} outcome is unknown"
            ),
            0,
        )
    status = exchange.status
    if 200 <= status < 300:
        if exchange.body_read_failed:
            return (
                "ambiguous",
                _failure(
                    client,
                    operation,
                    FailureKind.TRANSPORT,
                    f"Microsoft Graph accepted the {subject} but its response was interrupted",
                ),
                0,
            )
        if exchange.body_too_large:
            return (
                "ambiguous",
                _failure(
                    client,
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Microsoft Graph accepted the {subject} but its response exceeds "
                    "the configured size limit",
                ),
                0,
            )
        if exchange.reflects_secret:
            return (
                "ambiguous",
                _failure(
                    client,
                    operation,
                    FailureKind.PROTOCOL,
                    f"Microsoft Graph accepted the {subject} but its response contains credential material",
                ),
                0,
            )
        return "accepted", None, 0
    if status == 429:
        return (
            "notApplied",
            _failure(
                client,
                operation,
                FailureKind.RATE_LIMIT,
                f"Microsoft Graph throttled the {subject} before storing it",
            ),
            client.retry_after(exchange),
        )
    if status == 503:
        return (
            "notApplied",
            _failure(
                client,
                operation,
                FailureKind.AVAILABILITY,
                f"Microsoft Graph could not accept the {subject} while unavailable (HTTP 503)",
            ),
            client.retry_after(exchange),
        )
    if status == 501 or (400 <= status < 500 and status not in (408, 409, 429)):
        return "rejected", _rejection(client, operation, subject, exchange), 0
    return (
        "ambiguous",
        _failure(
            client,
            operation,
            FailureKind.AVAILABILITY,
            f"Microsoft Graph {subject} outcome is unknown after HTTP {status}"
            f"{client.error_code_suffix(exchange)}",
        ),
        client.retry_after(exchange),
    )


def _thread_link(client: Client, link: str, path: str) -> bool:
    if len(link.encode()) > 4096 or not safe_token_url(link):
        return False
    parsed = urlsplit(link)
    endpoint = urlsplit(client.endpoint)
    return bool(
        parsed.scheme == endpoint.scheme
        and parsed.netloc.lower() == endpoint.netloc.lower()
        and not parsed.username
        and not parsed.fragment
        and parsed.path.lower() == (endpoint.path + path).lower()
    )


class ListThreadRepliesOperation:
    """Read one bounded page of channel message replies."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable listThreadReplies definition."""
        return definitions.LIST_THREAD_REPLIES_DEFINITION

    async def invoke(
        self, call: Call, input: ListThreadRepliesInput
    ) -> QueryAttempt[ListThreadRepliesOutput]:
        """Validate a reply cursor and read the selected page."""
        client = self.client
        output = ListThreadRepliesOutput()
        team_id, channel_id, message_id = (
            input.team_id.strip(),
            input.channel_id.strip(),
            input.message_id.strip(),
        )
        try:
            valid_team_channel(team_id, channel_id)
            valid_message_id("messageId", message_id)
            page_size = input.page_size or 20
            if type(page_size) is not int or not 1 <= page_size <= 50:
                raise ValueError("pageSize must be from 1 to 50")
            maximum = input.max_text_characters or 4000
            if type(maximum) is not int or not 1 <= maximum <= 100000:
                raise ValueError("maxTextCharacters must be from 1 to 100000")
            path = thread_replies_path(team_id, channel_id, message_id)
            if input.cursor and not _thread_link(client, input.cursor, path):
                raise ValueError("cursor must be a nextCursor returned for the same thread")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                output,
                _empty_receipt(),
                _failure(client, "listThreadReplies", FailureKind.VALIDATION, str(error)),
            )
        try:
            credentials = await client.resolve_credentials(call)
        except Exception as error:
            credential_failure = _failure(
                client,
                "listThreadReplies",
                FailureKind.AUTHENTICATION,
                "Microsoft Teams connection credentials are unavailable; reconnect the connection"
                if isinstance(error, ReauthorizationRequiredError)
                else "Microsoft Teams connection credentials are temporarily unavailable",
            )
            return (
                QueryAttempt.branch_result("defect", output, _empty_receipt(), credential_failure)
                if isinstance(error, ReauthorizationRequiredError)
                else QueryAttempt.retry(credential_failure)
            )
        exchange, _ = await client.exchange(
            call,
            credentials,
            "GET",
            path,
            absolute_url=input.cursor,
            query=None if input.cursor else {"$top": str(page_size)},
        )
        classification, failure, delay = _read_classification(
            client, "listThreadReplies", "thread replies", exchange
        )
        receipt = client.receipt(call, exchange, message_id)
        if classification == "retry":
            assert failure is not None
            return QueryAttempt.retry(failure, delay)
        if classification != "read":
            return QueryAttempt.branch_result(classification, output, receipt, failure)
        try:
            collection = json.loads(exchange.body)
            if not isinstance(collection, dict) or not isinstance(collection.get("value"), list):
                raise ValueError("the body is not a chatMessage collection")
            replies = tuple(decode_message(resource, maximum) for resource in collection["value"])
            next_link = collection.get("@odata.nextLink") or ""
            if not isinstance(next_link, str) or (next_link and not _thread_link(client, next_link, path)):
                raise ValueError("the next-page link does not name this thread on the Graph endpoint")
            output = ListThreadRepliesOutput(replies=replies, next_cursor=next_link)
        except (ValueError, TypeError, UnicodeDecodeError, json.JSONDecodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListThreadRepliesOutput(),
                receipt,
                _failure(
                    client,
                    "listThreadReplies",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid reply page: " + str(error),
                ),
            )
        return QueryAttempt.branch_result("read", output, receipt)


@dataclass(frozen=True)
class _Post:
    operation: str
    subject: str
    path: str
    read_back_path: str
    read_back_reply_to_id: str
    body: dict[str, Any]
    comparison_text: str
    requested: PostMessageOutput


def _content(
    content: str, content_type: str | None, importance: str | None, subject: str, maximum_bytes: int
) -> tuple[dict[str, Any], str]:
    content_type = content_type or "text"
    if content_type not in ("text", "html"):
        raise ValueError("contentType must be text or html")
    if importance not in (None, "", "normal", "high", "urgent"):
        raise ValueError("importance must be normal, high, or urgent")
    if not content.strip():
        raise ValueError("content is required")
    if "\r" in subject or "\n" in subject:
        raise ValueError("subject must be one line")
    if len(subject) > 1024:
        raise ValueError("subject must be at most 1024 characters")
    if len(content.encode()) + len(subject.encode()) > maximum_bytes:
        raise ValueError(
            f"content and subject exceed the connection's maxMessageBytes limit of {maximum_bytes} bytes"
        )
    text = extract_message_text(content_type, content)
    if not text.strip():
        raise ValueError("content has no visible text")
    body: dict[str, Any] = {"body": {"contentType": content_type, "content": content}}
    if subject:
        body["subject"] = subject
    if importance:
        body["importance"] = importance
    return body, " ".join(text.split())


def _posted_output(requested: PostMessageOutput, message: Message, confirmed: bool) -> PostMessageOutput:
    return replace(
        requested,
        message_id=message.id,
        created_at=message.created_at,
        sender=message.sender,
        web_url=message.web_url,
        is_confirmed_by_read_back=confirmed,
    )


class _PostOperation:
    def __init__(self, client: Client) -> None:
        self.client = client

    async def _send(self, call: Call, post: _Post) -> MutationAttempt[PostMessageOutput]:
        client = self.client
        try:
            fence = Fence(call, timedelta(seconds=30))
            decision = await fence.decide()
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                post.requested,
                _empty_receipt(),
                _failure(
                    client,
                    post.operation,
                    FailureKind.LOCAL_DEFECT,
                    "Microsoft Teams dispatch fence could not be constructed",
                ),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    FailureKind.TRANSPORT,
                    "the Microsoft Teams dispatch checkpoint is unavailable, "
                    "so the message is not sent again",
                ),
                _empty_receipt(),
            )
        try:
            credentials = await client.resolve_credentials(call)
        except Exception as error:
            must_reauthorize = isinstance(error, ReauthorizationRequiredError)
            message = (
                "Microsoft Teams connection credentials are unavailable; reconnect the connection"
                if must_reauthorize
                else "Microsoft Teams connection credentials are temporarily unavailable"
            )
            failure = _failure(client, post.operation, FailureKind.AUTHENTICATION, message)
            if decision.kind == DecisionKind.SEND_ALLOWED:
                try:
                    await fence.record_not_sent()
                except Exception:
                    failure = replace(
                        failure, message=failure.message + "; the not-sent checkpoint also failed"
                    )
                if must_reauthorize:
                    return MutationAttempt.branch_result("defect", post.requested, _empty_receipt(), failure)
                return MutationAttempt.retry(failure)
            return (
                MutationAttempt.uncertain(post.requested, failure, _empty_receipt())
                if must_reauthorize
                else MutationAttempt.retry(failure)
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return await self._reconcile(
                call, credentials, post, decision.marker.provider_id if decision.marker else ""
            )
        payload = _go_json_body(post.body)
        try:
            exchange, credentials = await fence.dispatch(
                lambda deadline: client.exchange(
                    call,
                    credentials,
                    "POST",
                    post.path,
                    payload,
                    retry_rejected=False,
                    timeout=max(0, (deadline - datetime.now(UTC)).total_seconds()),
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                _failure(
                    client,
                    post.operation,
                    FailureKind.AVAILABILITY,
                    f"the {post.subject} was not sent before the dispatch deadline",
                )
            )
        except Exception:
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    FailureKind.TRANSPORT,
                    "Microsoft Teams message dispatch outcome is unknown",
                ),
                _empty_receipt(),
            )
        if exchange.status == 401 and isinstance(client.credentials, RejectedCredentialRefreshingProvider):
            from dex_connectors.sdk import resolve_credential_after_rejection

            try:
                replacement = await resolve_credential_after_rejection(
                    client.credentials, call, client.refresh_driver
                )
                if replacement.access_token.reveal() != credentials.access_token.reveal():
                    failure = _failure(
                        client,
                        post.operation,
                        FailureKind.AUTHENTICATION,
                        "Microsoft Graph rejected the access token; the refreshed credential "
                        "will be used on the next attempt",
                    )
                    try:
                        await fence.record_can_resend()
                    except Exception:
                        failure = replace(
                            failure, message=failure.message + "; the safe-resend checkpoint failed"
                        )
                    return MutationAttempt.retry(failure)
            except Exception:
                pass
        category, write_failure, delay = _write_classification(client, post.operation, post.subject, exchange)
        failure = write_failure or _failure(
            client, post.operation, FailureKind.PROTOCOL, "Microsoft Graph response is invalid"
        )
        receipt = client.receipt(call, exchange)
        if category == "accepted":
            try:
                resource = json.loads(exchange.body)
                if (
                    isinstance(resource, dict)
                    and isinstance(resource.get("id"), str)
                    and MESSAGE_ID.fullmatch(resource["id"])
                ):
                    try:
                        await fence.record_provider_id(resource["id"])
                    except Exception:
                        pass
                decoded_message = decode_message(resource)
                return MutationAttempt.branch_result(
                    "sent",
                    _posted_output(post.requested, decoded_message, False),
                    client.receipt(call, exchange, decoded_message.id),
                )
            except (ValueError, TypeError, UnicodeDecodeError, json.JSONDecodeError):
                failure = _failure(
                    client,
                    post.operation,
                    FailureKind.PROTOCOL,
                    f"Microsoft Graph accepted the {post.subject} but returned an unusable message",
                )
        elif category == "notApplied":
            assert failure is not None
            try:
                await fence.record_can_resend()
            except Exception:
                failure = replace(failure, message=failure.message + "; the safe-resend checkpoint failed")
            return MutationAttempt.retry(failure, delay)
        elif category == "rejected":
            return MutationAttempt.branch_result("providerRejected", post.requested, receipt, failure)
        elif category == "defect":
            return MutationAttempt.branch_result("defect", post.requested, receipt, failure)
        assert failure is not None
        if not post.read_back_path:
            return MutationAttempt.uncertain(post.requested, failure, receipt)
        return MutationAttempt.retry(failure, max(delay, 5))

    async def _reconcile(
        self, call: Call, credentials: Any, post: _Post, provider_id: str
    ) -> MutationAttempt[PostMessageOutput]:
        client = self.client
        if not post.read_back_path:
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    FailureKind.TRANSPORT,
                    f"an earlier attempt of this Step may have sent the {post.subject}, "
                    "and the connection cannot read it back, so it is not sent again",
                ),
                client.receipt(call, Exchange()),
            )
        if provider_id and not MESSAGE_ID.fullmatch(provider_id):
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    FailureKind.PROTOCOL,
                    "the earlier Microsoft Teams message has an invalid provider ID",
                ),
                _empty_receipt(),
            )
        path = post.read_back_path + ("/" + provider_id if provider_id else "")
        exchange, _ = await client.exchange(
            call, credentials, "GET", path, query=None if provider_id else {"$top": "50"}
        )
        category, failure, delay = _read_classification(
            client, post.operation, post.subject + " read-back", exchange
        )
        receipt = client.receipt(call, exchange, provider_id)
        if category == "retry":
            assert failure is not None
            return MutationAttempt.retry(failure, delay)
        if provider_id:
            if category == "read":
                try:
                    resource = json.loads(exchange.body)
                    if resource["id"] == provider_id:
                        decoded_message = decode_message(resource)
                        return MutationAttempt.branch_result(
                            "sent", _posted_output(post.requested, decoded_message, True), receipt
                        )
                except (ValueError, TypeError, KeyError, UnicodeDecodeError, json.JSONDecodeError):
                    pass
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    FailureKind.PROTOCOL,
                    "the earlier Microsoft Teams message could not be confirmed by its provider ID",
                ),
                receipt,
            )
        if category != "read":
            assert failure is not None
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    failure.kind,
                    f"an earlier attempt of this Step sent the {post.subject} and it could not "
                    f"be read back, so it is not sent again: {failure.message}",
                ),
                receipt,
            )
        try:
            collection = json.loads(exchange.body)
            if not isinstance(collection, dict) or not isinstance(collection.get("value"), list):
                raise ValueError("invalid collection")
        except (ValueError, UnicodeDecodeError, json.JSONDecodeError):
            return MutationAttempt.uncertain(
                post.requested,
                _failure(
                    client,
                    post.operation,
                    FailureKind.PROTOCOL,
                    f"an earlier attempt of this Step sent the {post.subject}, "
                    "and the messages read back are invalid",
                ),
                receipt,
            )
        first_attempt = (
            getattr(
                call.context,
                "first_attempt_at",
                getattr(call.context, "fixed_clock", datetime.min.replace(tzinfo=UTC)),
            )
            if call.context
            else datetime.min.replace(tzinfo=UTC)
        )
        matches: list[Message] = []
        for resource in collection["value"]:
            if (
                not isinstance(resource, dict)
                or not MESSAGE_ID.fullmatch(str(resource.get("id") or ""))
                or resource.get("messageType") != "message"
                or resource.get("deletedDateTime") is not None
            ):
                continue
            identity = resource.get("from")
            body = resource.get("body")
            if (
                not isinstance(identity, dict)
                or not isinstance(identity.get("user"), dict)
                or not isinstance(body, dict)
            ):
                continue
            try:
                from .client import _parse_go_time

                created_at = _parse_go_time(resource.get("createdDateTime"), "createdDateTime")
            except ValueError:
                continue
            if (
                created_at < first_attempt - timedelta(minutes=1)
                or (resource.get("replyToId") or "") != post.read_back_reply_to_id
                or str(resource.get("subject") or "").strip() != str(post.body.get("subject") or "").strip()
            ):
                continue
            if (
                " ".join(
                    extract_message_text(
                        str(body.get("contentType") or ""), str(body.get("content") or "")
                    ).split()
                )
                != post.comparison_text
            ):
                continue
            try:
                matches.append(decode_message(resource))
            except ValueError:
                continue
        if len(matches) == 1:
            return MutationAttempt.branch_result(
                "sent",
                _posted_output(post.requested, matches[0], True),
                client.receipt(call, exchange, matches[0].id),
            )
        kind = FailureKind.TRANSPORT if not matches else FailureKind.CONFLICT
        reason = (
            "the newest messages show no match" if not matches else "several identical messages were found"
        )
        return MutationAttempt.uncertain(
            post.requested,
            _failure(
                client,
                post.operation,
                kind,
                f"an earlier attempt of this Step sent the {post.subject} without a confirmed outcome, "
                f"and {reason}, so it is not sent again",
            ),
            receipt,
        )


class PostChannelMessageOperation(_PostOperation):
    """Post one top-level Teams channel message."""

    def definition(self) -> MutationDefinition:
        """Return the stable postChannelMessage definition."""
        return definitions.POST_CHANNEL_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostChannelMessageInput) -> IdempotencyKey:
        """Correlate the call without sending this key to Graph."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PostChannelMessageInput) -> MutationAttempt[PostMessageOutput]:
        """Validate and fence one channel post."""
        team_id, channel_id = input.team_id.strip(), input.channel_id.strip()
        requested = PostMessageOutput(team_id=team_id, channel_id=channel_id)
        try:
            valid_team_channel(team_id, channel_id)
            body, comparison = _content(
                input.content,
                input.content_type,
                input.importance,
                input.subject.strip(),
                self.client.max_message_bytes,
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                requested,
                _empty_receipt(),
                _failure(self.client, "postChannelMessage", FailureKind.VALIDATION, str(error)),
            )
        path = channel_messages_path(team_id, channel_id)
        return await self._send(
            call, _Post("postChannelMessage", "channel message", path, path, "", body, comparison, requested)
        )


class PostThreadReplyOperation(_PostOperation):
    """Post one reply to a Teams channel thread."""

    def definition(self) -> MutationDefinition:
        """Return the stable postThreadReply definition."""
        return definitions.POST_THREAD_REPLY_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostThreadReplyInput) -> IdempotencyKey:
        """Correlate the call without sending this key to Graph."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PostThreadReplyInput) -> MutationAttempt[PostMessageOutput]:
        """Validate and fence one thread reply."""
        team_id, channel_id, message_id = (
            input.team_id.strip(),
            input.channel_id.strip(),
            input.message_id.strip(),
        )
        requested = PostMessageOutput(team_id=team_id, channel_id=channel_id, reply_to_id=message_id)
        try:
            valid_team_channel(team_id, channel_id)
            valid_message_id("messageId", message_id)
            body, comparison = _content(
                input.content, input.content_type, input.importance, "", self.client.max_message_bytes
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                requested,
                _empty_receipt(),
                _failure(self.client, "postThreadReply", FailureKind.VALIDATION, str(error)),
            )
        path = thread_replies_path(team_id, channel_id, message_id)
        return await self._send(
            call,
            _Post("postThreadReply", "thread reply", path, path, message_id, body, comparison, requested),
        )


class PostChatMessageOperation(_PostOperation):
    """Post one message to an existing Teams chat."""

    def definition(self) -> MutationDefinition:
        """Return the stable postChatMessage definition."""
        return definitions.POST_CHAT_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostChatMessageInput) -> IdempotencyKey:
        """Correlate the call without sending this key to Graph."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PostChatMessageInput) -> MutationAttempt[PostMessageOutput]:
        """Validate and fence one chat post."""
        chat_id = input.chat_id.strip()
        requested = PostMessageOutput(chat_id=chat_id)
        try:
            if not CHAT_ID.fullmatch(chat_id):
                raise ValueError("chatId must be a chat ID such as 19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2")
            body, comparison = _content(
                input.content, input.content_type, input.importance, "", self.client.max_message_bytes
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                requested,
                _empty_receipt(),
                _failure(self.client, "postChatMessage", FailureKind.VALIDATION, str(error)),
            )
        return await self._send(
            call,
            _Post(
                "postChatMessage",
                "chat message",
                f"/chats/{chat_id}/messages",
                "",
                "",
                body,
                comparison,
                requested,
            ),
        )
