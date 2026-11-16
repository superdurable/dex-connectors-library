# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Gmail inbox poller shared by the messageReceived and replyReceived Triggers."""

import asyncio
import json
from collections.abc import Awaitable, Callable, Sequence
from dataclasses import dataclass
from datetime import timedelta
from email import policy
from email.parser import Parser

from dex_connectors.sdk import (
    Call,
    ConnectionRef,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerDeliveryReplayer,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerRef,
    TriggerTarget,
    bind_trigger_log_attributes,
    is_trigger_undeliverable,
    log_trigger_record,
    prepare_trigger_delivery,
    start_trigger_attempt,
    trigger_error_attributes,
)
from dex_connectors.sdk.pollingtrigger import new_trigger_credential_call

from ._generated.definitions import CONNECTOR_ID
from ._generated.models import MessageEvent, MessageMatcher
from .models import Credentials, Message
from .provider import GmailProvider, decode_message


class GmailPollError(Exception):
    """A poll that ended early; its message is safe to log and names no query, sender, or token."""


@dataclass(frozen=True, slots=True)
class MessageListing:
    """One inbox list call, delivered later by `MessagePollingTriggerSource.deliver`."""

    credentials: Credentials
    messages: Sequence[tuple[str, str]]


@dataclass(frozen=True, slots=True)
class _PendingMessageRetry:
    """An event that a target failed to consume, with the number of failed attempts."""

    event: TriggerEvent[MessageEvent]
    failures: int


def mailbox_address(value: str) -> str | None:
    """Return the address of one RFC 5322 mailbox, as Go's mail.ParseAddress, or None when invalid."""
    if "\r" in value or "\n" in value:
        return None
    try:
        parsed = Parser(policy=policy.default).parsestr(f"To: {value}\n\n")
        header = parsed["To"]
        addresses = list(header.addresses) if header is not None else []
    except Exception:  # noqa: BLE001
        # The email package raises assorted errors for malformed headers.
        return None
    if len(addresses) != 1 or "@" not in addresses[0].addr_spec:
        return None
    return str(addresses[0].addr_spec)


def validate_message_matcher(matcher: MessageMatcher | None) -> None:
    """Raise ValueError for an invalid or repeated sender address, as Go's MessageMatcher.validate."""
    seen: set[str] = set()
    for sender in matcher.sender_emails if matcher is not None else ():
        address = mailbox_address(sender) if isinstance(sender, str) else None
        if not address:
            raise ValueError("Gmail sender email is invalid")
        canonical = address.lower()
        if canonical in seen:
            raise ValueError("Gmail sender emails must be unique")
        seen.add(canonical)


def message_matcher_matches(matcher: MessageMatcher | None, subject: str, snippet: str, sender: str) -> bool:
    """Report whether a message passes matcher: subject or snippet text, then the sender allowlist."""
    if matcher is None:
        return True
    if matcher.message_contains:
        if matcher.message_contains.lower() not in f"{subject}\n{snippet}".lower():
            return False
    if not matcher.sender_emails:
        return True
    address = mailbox_address(sender)
    if address is None:
        return False
    return any(
        (allowed_address := mailbox_address(allowed)) is not None
        and allowed_address.lower() == address.lower()
        for allowed in matcher.sender_emails
    )


async def wait_for_poll_interval(cancellation: TriggerCancellation, delay: timedelta) -> None:
    """Wait delay, raising TriggerDeliveryAbortedError as soon as cancellation ends delivery."""
    cancellation.raise_if_cancelled()
    sleeping = asyncio.ensure_future(asyncio.sleep(delay.total_seconds()))
    cancelled = asyncio.ensure_future(cancellation.wait())
    try:
        await asyncio.wait({sleeping, cancelled}, return_when=asyncio.FIRST_COMPLETED)
    finally:
        for task in (sleeping, cancelled):
            if not task.done():
                task.cancel()
    cancellation.raise_if_cancelled()


class MessagePollingTriggerSource:
    """Polls one Gmail binding, as the Go connector's poller does.

    It lists the newest inbox page, reads each new message's metadata, and delivers matching roots or
    replies oldest first. The delivered set covers the last listed page in memory; a restart rescans the
    page, and the Flow start request ID and the application's RPC absorb the duplicates.
    """

    def __init__(
        self,
        provider: GmailProvider,
        connection: ConnectionRef,
        trigger_name: str,
        binding_name: str,
        search_query: str,
        matcher: MessageMatcher | None,
        requires_reply: bool,
    ) -> None:
        """Bind one binding's query and matcher; an empty binding_name omits the binding log attribute."""
        self._provider = provider
        self._search_query = search_query
        self._matcher = matcher
        self._requires_reply = requires_reply
        self._call: Call = new_trigger_credential_call(connection, TriggerRef(CONNECTOR_ID, trigger_name))
        attributes: dict[str, TriggerLogValue] = {
            "connector": CONNECTOR_ID,
            "connection": connection.name,
            "trigger": trigger_name,
        }
        if binding_name:
            attributes["binding"] = binding_name
        self._logger: TriggerLogger = bind_trigger_log_attributes(provider.trigger_logger, attributes)
        self._delivered: set[str] = set()
        # Every later poll delivers this event before any listed message, until the target consumes it.
        self._retry: _PendingMessageRetry | None = None
        self._poll_failures = 0

    async def run(self, context: TriggerContext, target: TriggerTarget[MessageEvent]) -> None:
        """Scan, then wait one poll interval, until the context ends."""
        await run_message_trigger_polls(
            context, lambda: self.scan(context, target), self._provider.poll_interval
        )

    async def scan(self, context: TriggerContext, target: TriggerTarget[MessageEvent]) -> None:
        """List the inbox and deliver the listed messages."""
        await self.deliver(context, target, await self.list(context))

    async def list(self, context: TriggerContext) -> MessageListing:
        """Read the newest inbox page that matches the source's search query."""
        try:
            credentials = await self._provider.resolve_credentials(self._call)
        except Exception as error:
            raise self._poll_failed(
                context, GmailPollError("Gmail Trigger credentials are unavailable"), error
            ) from error
        try:
            messages, credentials = await self._provider.list_inbox_messages(
                self._call, credentials, self._search_query
            )
        except Exception as error:
            raise self._poll_failed(
                context, GmailPollError("Gmail message list is unavailable"), error
            ) from error
        return MessageListing(credentials, messages)

    async def deliver(
        self, context: TriggerContext, target: TriggerTarget[MessageEvent], listing: MessageListing
    ) -> None:
        """Hand target the event that failed on an earlier poll, if any, then every new matching message.

        Listed messages are delivered oldest first. Delivery stops at the first failure, so the next poll
        retries that event before any later message, even after newer mail pushes it off the page.
        """
        if self._retry is not None:
            await self._handle(context, target, self._retry.event)
        credentials = listing.credentials
        current: set[str] = set()
        for message_id, thread_id in reversed(listing.messages):
            if not message_id:
                continue
            current.add(message_id)
            if message_id in self._delivered:
                continue
            message_attributes: dict[str, TriggerLogValue] = {"thread_id": thread_id, "event_id": message_id}
            message, credentials = await self._read(context, credentials, message_id, message_attributes)
            is_reply = message.in_reply_to.strip() != "" or message.references.strip() != ""
            reason = self._ignore_reason(is_reply, message)
            if reason:
                self._log(
                    context,
                    TriggerLogLevel.DEBUG,
                    "trigger event ignored",
                    {**message_attributes, "reason": reason},
                )
                continue
            event = TriggerEvent(
                message.message_id,
                message.received_at,
                MessageEvent(
                    primary_email=credentials.primary_email,
                    message_id=message.message_id,
                    thread_id=message.thread_id,
                    from_=message.from_address,
                    subject=message.subject,
                    snippet=message.snippet,
                    received_at=message.received_at,
                    is_reply=is_reply,
                ),
            )
            try:
                await prepare_trigger_delivery(context, target, event)
            except Exception as error:
                raise self._poll_failed(
                    context,
                    GmailPollError("Gmail Trigger delivery could not be recorded"),
                    error,
                    message_attributes,
                ) from error
            # An undeliverable event is consumed; any other failure ends this scan for the next poll to retry.
            await self._handle(context, target, event)
        self._delivered = current
        self._poll_failures = 0

    async def _read(
        self,
        context: TriggerContext,
        credentials: Credentials,
        message_id: str,
        attributes: dict[str, TriggerLogValue],
    ) -> tuple[Message, Credentials]:
        try:
            result, credentials = await self._provider.read_message(
                self._call, credentials, message_id, "metadata"
            )
        except Exception as error:
            raise self._poll_failed(
                context, GmailPollError("Gmail message lookup failed"), error, attributes
            ) from error
        if result.status_code < 200 or result.status_code >= 300:
            raise self._poll_failed(context, GmailPollError("Gmail message lookup failed"), None, attributes)
        try:
            return decode_message(json.loads(result.body)), credentials
        except (ValueError, UnicodeDecodeError) as error:
            raise self._poll_failed(
                context, GmailPollError("Gmail message is invalid"), error, attributes
            ) from error

    async def _handle(
        self, context: TriggerContext, target: TriggerTarget[MessageEvent], event: TriggerEvent[MessageEvent]
    ) -> None:
        """Make one delivery attempt and log its outcome.

        It returns when target consumes the event, and otherwise keeps the event as the source's retry
        and raises. A durable inbox or a `new_trigger` runner logs the undeliverable events it consumes
        itself, so the source then logs no recovery.
        """
        failures = self._retry.failures if self._retry is not None and self._retry.event.id == event.id else 0
        thread_attributes: dict[str, TriggerLogValue] = {"thread_id": event.payload.thread_id}
        message_attributes: dict[str, TriggerLogValue] = {**thread_attributes, "event_id": event.id}
        # The thread ID also reaches the records of the inbox and the Dex targets inside the attempt.
        attempt = start_trigger_attempt(context.with_log_attributes(thread_attributes))
        try:
            await target.handle_trigger(attempt.context, event)
        except Exception as error:
            if is_trigger_undeliverable(error):
                self._log(
                    context,
                    TriggerLogLevel.WARN,
                    "trigger event skipped: undeliverable",
                    {**message_attributes, **trigger_error_attributes(error)},
                )
            elif context.cancellation.is_cancelled:
                raise
            else:
                # Gmail answered, so the poll itself did not fail.
                self._poll_failures = 0
                self._retry = _PendingMessageRetry(event, failures + 1)
                self._log(
                    context,
                    TriggerLogLevel.WARN,
                    "trigger delivery failed; retrying",
                    {
                        **message_attributes,
                        "attempt": failures + 1,
                        "delay_ms": self._provider.poll_interval // timedelta(milliseconds=1),
                        **trigger_error_attributes(error),
                    },
                )
                raise
        else:
            if failures > 0 and not attempt.was_skipped():
                self._log(
                    context,
                    TriggerLogLevel.INFO,
                    "trigger delivered after retry",
                    {**message_attributes, "attempts": failures + 1},
                )
        self._retry = None
        # A consumed retry that Gmail still lists must not be delivered again from the listing.
        self._delivered.add(event.id)

    def _ignore_reason(self, is_reply: bool, message: Message) -> str:
        """Explain why the source does not deliver a listed message, or return "" when it matches."""
        if self._requires_reply and not is_reply:
            return "not_a_reply"
        if not self._requires_reply and is_reply:
            return "not_a_root"
        if not message_matcher_matches(self._matcher, message.subject, message.snippet, message.from_address):
            return "matcher_mismatch"
        return ""

    def _poll_failed(
        self,
        context: TriggerContext,
        error: GmailPollError,
        cause: BaseException | None,
        attributes: dict[str, TriggerLogValue] | None = None,
    ) -> Exception:
        """Log a poll that ended early and return the error to raise; the next poll retries."""
        if context.cancellation.is_cancelled:
            return TriggerDeliveryAbortedError()
        error.__cause__ = cause
        self._poll_failures += 1
        cause_attributes: dict[str, TriggerLogValue] = (
            {} if cause is None else {"error_cause_type": trigger_error_attributes(cause)["error_type"]}
        )
        self._log(
            context,
            TriggerLogLevel.WARN,
            "gmail poll failed; retrying",
            {
                **(attributes or {}),
                "attempt": self._poll_failures,
                "delay_ms": self._provider.poll_interval // timedelta(milliseconds=1),
                "error": str(error),
                **cause_attributes,
            },
        )
        return error

    def _log(
        self,
        context: TriggerContext,
        level: TriggerLogLevel,
        message: str,
        attributes: dict[str, TriggerLogValue],
    ) -> None:
        log_trigger_record(self._logger, context, level, message, attributes)


@dataclass(frozen=True, slots=True)
class MessageTriggerRouteSource:
    """One poller's source and the target it delivers to."""

    source: MessagePollingTriggerSource
    target: TriggerTarget[MessageEvent]


async def run_message_trigger_polls(
    context: TriggerContext, poll: Callable[[], Awaitable[None]], interval: timedelta
) -> None:
    """Run poll, then wait interval, until the context ends; a failed poll is retried after the wait."""
    while True:
        context.cancellation.raise_if_cancelled()
        try:
            await poll()
        except Exception:  # noqa: BLE001
            # A failed poll already logged its failure; the next one retries it.
            context.cancellation.raise_if_cancelled()
        await wait_for_poll_interval(context.cancellation, interval)


async def poll_message_trigger_routes(
    context: TriggerContext,
    root_routes: Sequence[MessageTriggerRouteSource],
    reply_routes: Sequence[MessageTriggerRouteSource],
) -> None:
    """Make one ordered pass over every route.

    Replies are listed before roots: a listed reply arrived after its root, so the root listing that
    follows contains that root too, unless an earlier poll delivered it.
    """
    reply_listings = [await route.source.list(context) for route in reply_routes]
    for route in root_routes:
        await route.source.scan(context, route.target)
    for route, listing in zip(reply_routes, reply_listings, strict=True):
        await route.source.deliver(context, route.target, listing)


async def replay_message_trigger_routes(
    context: TriggerContext, routes: Sequence[MessageTriggerRouteSource]
) -> None:
    """Deliver every route's pending durable inbox events in route order."""
    for route in routes:
        if not isinstance(route.target, TriggerDeliveryReplayer):
            continue
        try:
            await route.target.replay_trigger_deliveries(context)
        except TriggerDeliveryAbortedError:
            raise
        except Exception as error:
            raise RuntimeError(f"replay Gmail Trigger deliveries: {error}") from error
