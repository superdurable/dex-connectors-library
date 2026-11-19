# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Discord operation invocation, validation, and Go-compatible classification."""

import base64
import hashlib
import json
import math
import re
import unicodedata
from datetime import UTC, datetime
from typing import TypeVar
from urllib.parse import quote, urlencode

import httpx
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
from dex_connectors.sdk.providerhttp import ProviderTransportError, parse_retry_after, read_error_tokens
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import ADD_REACTION_DEFINITION as ADD_REACTION_DEFINITION
from ._generated.definitions import CREATE_MESSAGE_DEFINITION as CREATE_MESSAGE_DEFINITION
from ._generated.definitions import CREATED, DEFECT, NOT_FOUND, PROVIDER_REJECTED
from ._generated.definitions import GET_CHANNEL_DEFINITION as GET_CHANNEL_DEFINITION
from ._generated.definitions import LIST_CHANNEL_MESSAGES_DEFINITION as LIST_CHANNEL_MESSAGES_DEFINITION
from .client import USER_AGENT, Client, FencedExchange, ProviderResponse
from .models import (
    AddedReaction,
    AddReactionInput,
    Channel,
    CreateMessageInput,
    GetChannelInput,
    ListChannelMessagesInput,
    Message,
    MessageAuthor,
    MessagePage,
)

_Output = TypeVar("_Output")

_SNOWFLAKE = re.compile(r"[0-9]{17,20}\Z")
_CUSTOM_EMOJI = re.compile(r"[A-Za-z0-9_]{2,32}:[0-9]{17,20}\Z")
_DOCUMENTED_CODES = frozenset(
    {
        "0",
        "10003",
        "10008",
        "10014",
        "20028",
        "30010",
        "40001",
        "40005",
        "50001",
        "50006",
        "50008",
        "50013",
        "50035",
        "50083",
        "90001",
    }
)
_NOT_FOUND_CODES = frozenset({"10003", "10008", "10014"})
# Only these statuses prove Discord refused a send; 408, 409, 429, and other 4xx leave the outcome unknown.
_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "discord", operation, message)


def _snowflake(name: str, value: str) -> None:
    if not isinstance(value, str) or _SNOWFLAKE.fullmatch(value) is None:
        raise ValueError(f"{name} must be a Discord snowflake ID of 17 to 20 digits")


def _decode_json(body: bytes) -> object:
    try:
        return json.loads(
            body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
        )
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("Discord returned a malformed response") from error


def _channel(body: bytes, channel_id: str) -> Channel:
    item = _decode_json(body)
    if not isinstance(item, dict) or item.get("id") != channel_id or type(item.get("type")) is not int:
        raise ValueError("Discord returned an invalid channel")
    for name in ("guild_id", "last_message_id"):
        value = item.get(name) or ""
        if not isinstance(value, str) or (value and _SNOWFLAKE.fullmatch(value) is None):
            raise ValueError("Discord returned a channel with an invalid snowflake ID")
    for name in ("name", "topic"):
        if item.get(name) is not None and not isinstance(item[name], str):
            raise ValueError("Discord returned an invalid channel field")
    return Channel(
        item["id"],
        item["type"],
        item.get("guild_id") or "",
        item.get("name") or "",
        item.get("topic") or "",
        item.get("last_message_id") or "",
    )


def _message(item: object, channel_id: str) -> Message:
    if (
        not isinstance(item, dict)
        or not isinstance(item.get("id"), str)
        or _SNOWFLAKE.fullmatch(item["id"]) is None
    ):
        raise ValueError("Discord returned a message without a valid snowflake ID")
    if item.get("channel_id") != channel_id:
        raise ValueError("Discord returned a message from a different channel")
    author = item.get("author")
    if (
        not isinstance(author, dict)
        or not isinstance(author.get("id"), str)
        or _SNOWFLAKE.fullmatch(author["id"]) is None
    ):
        raise ValueError("Discord returned a message without a valid author")
    if not isinstance(item.get("content"), str) or type(item.get("type")) is not int:
        raise ValueError("Discord returned a message without content or type")
    try:
        timestamp = datetime.fromisoformat(item["timestamp"].replace("Z", "+00:00"))
        if timestamp.tzinfo is None:
            raise ValueError("timestamp timezone required")
        timestamp = timestamp.astimezone(UTC)
        edited = item.get("edited_timestamp")
        edited_timestamp = (
            datetime.fromisoformat(edited.replace("Z", "+00:00")) if edited is not None else None
        )
        if edited_timestamp is not None:
            if edited_timestamp.tzinfo is None:
                raise ValueError("edited timestamp timezone required")
            edited_timestamp = edited_timestamp.astimezone(UTC)
    except (ValueError, TypeError, AttributeError) as error:
        raise ValueError("Discord returned a message without a valid timestamp") from error
    if not isinstance(author.get("username", ""), str) or type(author.get("bot", False)) is not bool:
        raise ValueError("Discord returned a message without a valid author")
    return Message(
        item["id"],
        channel_id,
        MessageAuthor(author["id"], author.get("username", ""), author.get("bot", False)),
        item["content"],
        timestamp,
        edited_timestamp,
        item["type"],
    )


def _page(body: bytes, channel_id: str, limit: int) -> MessagePage:
    items = _decode_json(body)
    if not isinstance(items, list) or len(items) > limit:
        raise ValueError("Discord returned an invalid message page")
    messages = tuple(_message(item, channel_id) for item in items)
    return MessagePage(messages, messages[-1].id if messages else "", len(messages) == limit)


def _classify(response: ProviderResponse, operation: str, now: datetime) -> tuple[str, Failure, float]:
    status = response.status_code
    codes = read_error_tokens(response.body, ("/code",))
    code = codes[0] if len(codes) == 1 and codes[0] in _DOCUMENTED_CODES else ""
    detail = f", code {code}" if code else ""
    if status == 429:
        delay = parse_retry_after(response.headers.get("Retry-After", ""), now)
        try:
            body = _decode_json(response.body)
            seconds = body.get("retry_after") if isinstance(body, dict) else None
            if (
                isinstance(seconds, (int, float))
                and not isinstance(seconds, bool)
                and seconds > 0
                and math.isfinite(seconds)
            ):
                delay = min(math.ceil(seconds * 1000) / 1000, 60 * 60)
        except ValueError:
            pass
        return "retry", _failure(operation, FailureKind.RATE_LIMIT, "Discord rate limited the request"), delay
    if status == 408 or status >= 500:
        delay = parse_retry_after(response.headers.get("Retry-After", ""), now)
        return (
            "retry",
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                f"Discord is temporarily unavailable (HTTP {status}{detail})",
            ),
            delay,
        )
    if status == 401:
        return (
            "providerRejected",
            _failure(
                operation, FailureKind.AUTHENTICATION, f"Discord rejected the bot token (HTTP 401{detail})"
            ),
            0,
        )
    if status == 403:
        return (
            "providerRejected",
            _failure(
                operation,
                FailureKind.AUTHORIZATION,
                (
                    "Discord denied access: check that the bot is in the server "
                    "and has the channel permissions "
                    f"(HTTP 403{detail})"
                ),
            ),
            0,
        )
    if status == 404 and (not code or code in _NOT_FOUND_CODES):
        return (
            "notFound",
            _failure(
                operation,
                FailureKind.NOT_FOUND,
                f"Discord found no such channel, message, or emoji (HTTP 404{detail})",
            ),
            0,
        )
    if status in (400, 413):
        return (
            "providerRejected",
            _failure(
                operation,
                FailureKind.VALIDATION,
                f"Discord rejected the request as invalid (HTTP {status}{detail})",
            ),
            0,
        )
    if status in (404, 411, 414, 415, 422):
        return (
            "providerRejected",
            _failure(
                operation,
                FailureKind.PROVIDER_REJECTION,
                f"Discord rejected the request (HTTP {status}{detail})",
            ),
            0,
        )
    # A redirect or unlisted status proves nothing, so the request is retried.
    return (
        "retry",
        _failure(
            operation,
            FailureKind.AVAILABILITY,
            f"Discord answered with an unexpected status (HTTP {status}{detail})",
        ),
        0,
    )


async def _invoke(
    client: Client,
    call: Call,
    operation: str,
    method: str,
    path: str,
    object_id: str,
    body: bytes | None = None,
) -> tuple[str, ProviderResponse | None, Failure | None, float, Receipt]:
    token, failure, branch = await client.resolve_token(call, operation)
    if failure is not None or token is None:
        return branch, None, failure, 0, Receipt()
    try:
        response = await client.request(call, token, method, path, body)
    except ProviderTransportError:
        return (
            "retry",
            None,
            _failure(operation, FailureKind.TRANSPORT, "Discord did not return a complete response"),
            0,
            Receipt(),
        )
    except (ValueError, TypeError):
        return (
            "defect",
            None,
            _failure(operation, FailureKind.LOCAL_DEFECT, "Discord request could not be built"),
            0,
            Receipt(),
        )
    receipt = client.receipt(call, object_id)
    if response.is_body_too_large:
        return (
            "invalidResponse",
            response,
            _failure(
                operation,
                FailureKind.RESPONSE_TOO_LARGE,
                "Discord response exceeds the configured size limit",
            ),
            0,
            receipt,
        )
    if not 200 <= response.status_code < 300:
        branch, failure, delay = _classify(response, operation, client.now())
        return branch, response, failure, delay, receipt
    return "", response, None, 0, receipt


def _query_outcome(
    branch: str, output: _Output, failure: Failure | None, delay: float, receipt: Receipt
) -> QueryAttempt[_Output]:
    if branch == "retry":
        assert failure is not None
        return QueryAttempt.retry(failure, delay)
    return QueryAttempt.branch_result(branch, output, receipt, failure)


def _mutation_outcome(
    branch: str, output: _Output, failure: Failure | None, delay: float, receipt: Receipt
) -> MutationAttempt[_Output]:
    # addReaction is declared safe, so an oversized answer retries instead of selecting invalidResponse.
    if branch in ("retry", "invalidResponse"):
        assert failure is not None
        return MutationAttempt.retry(failure, delay)
    return MutationAttempt.branch_result(branch, output, receipt, failure)


class GetChannelOperation:
    """Read one Discord channel by snowflake ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest's channel Query definition."""
        return GET_CHANNEL_DEFINITION

    async def invoke(self, call: Call, input: GetChannelInput) -> QueryAttempt[Channel]:
        """Validate and read one channel per Dex attempt."""
        empty = Channel()
        try:
            _snowflake("channelId", input.channel_id)
        except ValueError as error:
            return _query_outcome(
                "defect", empty, _failure("getChannel", FailureKind.VALIDATION, str(error)), 0, Receipt()
            )
        branch, response, failure, delay, receipt = await _invoke(
            self.client, call, "getChannel", "GET", "/channels/" + input.channel_id, input.channel_id
        )
        if branch:
            return _query_outcome(branch, empty, failure, delay, receipt)
        assert response is not None
        try:
            channel = _channel(response.body, input.channel_id)
        except ValueError as error:
            return _query_outcome(
                "invalidResponse", empty, _failure("getChannel", FailureKind.PROTOCOL, str(error)), 0, receipt
            )
        return _query_outcome("found", channel, None, 0, receipt)


class ListChannelMessagesOperation:
    """Read one bounded Discord message page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest's message-page Query definition."""
        return LIST_CHANNEL_MESSAGES_DEFINITION

    async def invoke(self, call: Call, input: ListChannelMessagesInput) -> QueryAttempt[MessagePage]:
        """Validate pagination and read one message page."""
        empty = MessagePage()
        try:
            _snowflake("channelId", input.channel_id)
            if type(input.limit) is not int or input.limit < 0 or input.limit > 100:
                raise ValueError("limit must be from 1 through 100, or zero for Discord's default of 50")
            if input.before_message_id and input.after_message_id:
                raise ValueError("set at most one of beforeMessageId and afterMessageId")
            parameters = {"limit": str(input.limit or 50)}
            if input.before_message_id:
                _snowflake("beforeMessageId", input.before_message_id)
                parameters["before"] = input.before_message_id
            if input.after_message_id:
                _snowflake("afterMessageId", input.after_message_id)
                parameters["after"] = input.after_message_id
        except ValueError as error:
            return _query_outcome(
                "defect",
                empty,
                _failure("listChannelMessages", FailureKind.VALIDATION, str(error)),
                0,
                Receipt(),
            )
        path = "/channels/" + input.channel_id + "/messages?" + urlencode(sorted(parameters.items()))
        branch, response, failure, delay, receipt = await _invoke(
            self.client, call, "listChannelMessages", "GET", path, input.channel_id
        )
        if branch:
            return _query_outcome(branch, empty, failure, delay, receipt)
        assert response is not None
        try:
            page = _page(response.body, input.channel_id, input.limit or 50)
        except ValueError as error:
            return _query_outcome(
                "invalidResponse",
                empty,
                _failure("listChannelMessages", FailureKind.PROTOCOL, str(error)),
                0,
                receipt,
            )
        return _query_outcome("listed", page, None, 0, receipt)


class CreateMessageOperation:
    """Post one bot message with a nonce derived from Dex's idempotency key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's message Mutation definition."""
        return CREATE_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateMessageInput) -> IdempotencyKey:
        """Use the stable call ID as the operation's idempotency key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateMessageInput) -> MutationAttempt[Message]:
        """Send one mention-safe message under a single-dispatch fence and classify this attempt.

        An unknown outcome resends with the same enforced nonce while possible sends stay
        below ``call.max_sends`` and selects uncertain at the cap. A definite refusal of the
        first send selects its branch and is never resent.
        """
        empty = Message()
        try:
            _snowflake("channelId", input.channel_id)
            if not isinstance(input.content, str) or not input.content.strip():
                raise ValueError("content must not be blank")
            if len(input.content) > 2000:
                raise ValueError("content must be valid UTF-8 of at most 2000 characters")
            if not call.idempotency_key:
                raise ValueError("createMessage requires an idempotency key")
            nonce = (
                base64.urlsafe_b64encode(hashlib.sha256(str(call.idempotency_key).encode()).digest())
                .decode()
                .rstrip("=")[:25]
            )
            body: dict[str, object] = {
                "content": input.content,
                "nonce": nonce,
                "enforce_nonce": True,
                "allowed_mentions": {"parse": []},
            }
            if input.reply_to_message_id:
                _snowflake("replyToMessageId", input.reply_to_message_id)
                body["message_reference"] = {
                    "message_id": input.reply_to_message_id,
                    "fail_if_not_exists": True,
                }
            encoded = json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode()
        except (ValueError, UnicodeEncodeError) as error:
            return _mutation_outcome(
                "defect", empty, _failure("createMessage", FailureKind.VALIDATION, str(error)), 0, Receipt()
            )
        try:
            fence = Fence(
                call,
                CREATE_MESSAGE_DEFINITION.step_defaults.execute_method_timeout,
                max_sends=call.max_sends,
            )
            decision = await fence.decide()
        except ValueError:
            return _create_message_branch(
                DEFECT, FailureKind.LOCAL_DEFECT, "Discord dispatch fence could not be constructed", Receipt()
            )
        receipt = self.client.receipt(call, input.channel_id)
        if decision.kind == DecisionKind.FAIL_CLOSED:
            # An unreadable checkpoint is an unknown outcome; the retry budget bounds these attempts.
            return _create_message_retry(
                FailureKind.AVAILABILITY,
                "Discord dispatch checkpoint is unreadable; no message was sent in this attempt",
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return _create_message_uncertain(
                FailureKind.TRANSPORT,
                "an earlier attempt may have created the Discord message, and the send cap is reached",
                receipt,
            )
        if decision.marker is not None and decision.marker.provider_id:
            # No message ID is checkpointed, but a saved one would prove a created message; never resend it.
            return _create_message_uncertain(
                FailureKind.TRANSPORT,
                "an earlier attempt created the Discord message, but its confirmation was lost",
                receipt,
            )
        # The resend with the same enforced nonce is Discord's only read-back.
        # This attempt's send would be the last one the cap allows when no further send remains.
        can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        # An earlier send may have created the message, so a refusal of this resend proves nothing.
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        token, failure, branch = await self.client.resolve_token(call, "createMessage")
        if failure is not None or token is None:
            assert failure is not None
            return await _local_create_message_failure(
                fence, has_possible_earlier_send, branch, failure, receipt
            )
        headers = {
            "Accept": "application/json",
            "Authorization": "Bot " + token,
            "Content-Type": "application/json",
            "User-Agent": USER_AGENT,
        }
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/channels/" + input.channel_id + "/messages",
                headers=headers,
                content=encoded,
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return await _local_create_message_failure(
                fence,
                has_possible_earlier_send,
                "defect",
                _failure("createMessage", FailureKind.LOCAL_DEFECT, "Discord request could not be built"),
                receipt,
            )
        observed_statuses: list[int] = []

        async def send(_deadline: datetime) -> FencedExchange:
            return await self.client.send_fenced(request, observed_statuses)

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _create_message_retry(FailureKind.AVAILABILITY, "Discord dispatch was not started")
        except TimeoutError:
            # The write window cut the exchange; a status seen at headers still classifies it.
            status = observed_statuses[-1] if observed_statuses else 0
            exchange = FencedExchange(status, read_error="read" if status else "request")
        except Exception:
            exchange = FencedExchange(read_error="request")
        return await _classify_create_message(
            self.client,
            call,
            fence,
            exchange,
            input.channel_id,
            can_resend_unknown_outcome,
            has_possible_earlier_send,
            receipt,
        )


async def _classify_create_message(
    client: Client,
    call: Call,
    fence: Fence,
    exchange: FencedExchange,
    channel_id: str,
    can_resend_unknown_outcome: bool,
    has_possible_earlier_send: bool,
    receipt: Receipt,
) -> MutationAttempt[Message]:
    """Classify one dispatched createMessage exchange."""
    if exchange.status_code == 0:
        if not exchange.is_request_sent:
            try:
                await fence.record_can_resend()
            except Exception:
                pass  # A lost canResend checkpoint leaves the claim, which the next attempt counts.
            return _create_message_retry(FailureKind.TRANSPORT, "Discord could not be reached before sending")
        return _resend_or_select_uncertain(
            can_resend_unknown_outcome,
            FailureKind.TRANSPORT,
            "Discord message outcome is unknown",
            0,
            receipt,
        )
    if not 200 <= exchange.status_code < 300:
        return await _classify_create_message_refusal(
            client,
            fence,
            exchange,
            channel_id,
            can_resend_unknown_outcome,
            has_possible_earlier_send,
            receipt,
        )
    if exchange.read_error:
        # Discord answered 2xx before the body broke or overflowed; a resend with the same nonce returns it.
        return _resend_or_select_uncertain(
            can_resend_unknown_outcome,
            FailureKind.TRANSPORT,
            "Discord answered the message, but its response could not be read",
            0,
            receipt,
        )
    try:
        message = _message(_decode_json(exchange.body), channel_id)
    except ValueError as error:
        # A resend inside the nonce window returns the same message, so a malformed answer is unknown.
        return _resend_or_select_uncertain(
            can_resend_unknown_outcome, FailureKind.PROTOCOL, str(error), 0, receipt
        )
    # No message ID is checkpointed: a later resend with the same nonce returns this message.
    return MutationAttempt.branch_result(CREATED, message, client.receipt(call, message.id))


async def _classify_create_message_refusal(
    client: Client,
    fence: Fence,
    exchange: FencedExchange,
    channel_id: str,
    can_resend_unknown_outcome: bool,
    has_possible_earlier_send: bool,
    receipt: Receipt,
) -> MutationAttempt[Message]:
    """Classify a non-2xx answer: 429 and 503 prove no processing, and only a readable
    allowlisted refusal selects its branch; every other answer is an unknown outcome."""
    response = ProviderResponse(exchange.status_code, exchange.headers, exchange.body)
    branch, failure, delay = _classify(response, "createMessage", client.now())
    if exchange.status_code in (429, 503):
        try:
            await fence.record_can_resend()
        except Exception:
            return _create_message_retry(
                FailureKind.AVAILABILITY, "Discord retry checkpoint could not be recorded"
            )
        return MutationAttempt.retry(failure, delay)
    is_definite_refusal = (
        not exchange.read_error
        and exchange.status_code in _REFUSAL_STATUSES
        and branch in (NOT_FOUND, PROVIDER_REJECTED)
        and not _is_accepted_message_body(exchange.body, channel_id)
    )
    if not is_definite_refusal:
        # An unreadable, contradictory, or unlisted answer cannot prove that Discord created nothing.
        return _resend_or_select_uncertain(
            can_resend_unknown_outcome, failure.kind, failure.message, delay, receipt
        )
    if has_possible_earlier_send:
        return _create_message_uncertain(
            failure.kind,
            "an earlier attempt may have created the Discord message: " + failure.message,
            receipt,
        )
    try:
        await fence.record_can_resend()
    except Exception:
        pass  # The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
    return MutationAttempt.branch_result(branch, Message(), receipt, failure)


async def _local_create_message_failure(
    fence: Fence, has_possible_earlier_send: bool, branch: str, failure: Failure, receipt: Receipt
) -> MutationAttempt[Message]:
    """Handle a failure before dispatch.

    A transient failure retries, recording notSent only when no earlier send may have created
    the message. A terminal one selects its branch before any possible send and uncertain after one.
    """
    if branch == "retry":
        if not has_possible_earlier_send:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # Without notSent the next attempt counts this attempt as a possible send.
        # After a possible send, notSent would hide that send, so nothing is recorded.
        return MutationAttempt.retry(failure)
    if has_possible_earlier_send:
        return _create_message_uncertain(
            failure.kind,
            "an earlier attempt may have created the Discord message: " + failure.message,
            receipt,
        )
    try:
        await fence.record_not_sent()
    except Exception:
        pass  # The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
    return MutationAttempt.branch_result(branch, Message(), Receipt(), failure)


def _resend_or_select_uncertain(
    can_resend_unknown_outcome: bool, kind: FailureKind, message: str, delay: float, receipt: Receipt
) -> MutationAttempt[Message]:
    """Retry a possibly created message while the send cap allows another send."""
    if can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send.
        return _create_message_retry(kind, message, delay)
    return _create_message_uncertain(kind, message, receipt)


def _is_accepted_message_body(body: bytes, channel_id: str) -> bool:
    """Report an error body that nevertheless describes a message in the channel."""
    try:
        _message(_decode_json(body), channel_id)
    except ValueError:
        return False
    return True


def _create_message_branch(
    branch: str, kind: FailureKind, message: str, receipt: Receipt
) -> MutationAttempt[Message]:
    return MutationAttempt.branch_result(branch, Message(), receipt, _failure("createMessage", kind, message))


def _create_message_retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[Message]:
    return MutationAttempt.retry(_failure("createMessage", kind, message), delay)


def _create_message_uncertain(kind: FailureKind, message: str, receipt: Receipt) -> MutationAttempt[Message]:
    return MutationAttempt.uncertain(Message(), _failure("createMessage", kind, message), receipt)


class AddReactionOperation:
    """Add a repeat-safe Unicode or custom emoji reaction."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's reaction Mutation definition."""
        return ADD_REACTION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddReactionInput) -> IdempotencyKey:
        """Use the stable call ID; Discord deduplicates repeated user reactions."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddReactionInput) -> MutationAttempt[AddedReaction]:
        """Send one PUT to add the bot's reaction."""
        empty = AddedReaction()
        try:
            _snowflake("channelId", input.channel_id)
            _snowflake("messageId", input.message_id)
            emoji = input.emoji
            unicode_candidate = (
                isinstance(emoji, str)
                and bool(emoji)
                and len(emoji.encode()) <= 64
                and emoji.strip() == emoji
                and all(
                    (ord(character) >= 128 or character.isdigit() or character in "#*")
                    and not character.isspace()
                    and unicodedata.category(character) != "Cc"
                    for character in emoji
                )
            )
            if not (_CUSTOM_EMOJI.fullmatch(emoji) or unicode_candidate):
                raise ValueError("emoji must be a Unicode emoji or a custom emoji as name:id")
        except (ValueError, UnicodeEncodeError) as error:
            return _mutation_outcome(
                "defect", empty, _failure("addReaction", FailureKind.VALIDATION, str(error)), 0, Receipt()
            )
        path = (
            f"/channels/{input.channel_id}/messages/{input.message_id}/reactions/"
            f"{quote(input.emoji, safe='$&+=:@')}/@me"
        )
        branch, response, failure, delay, receipt = await _invoke(
            self.client, call, "addReaction", "PUT", path, input.message_id
        )
        if branch:
            return _mutation_outcome(branch, empty, failure, delay, receipt)
        return _mutation_outcome(
            "added", AddedReaction(input.channel_id, input.message_id, input.emoji), None, 0, receipt
        )
