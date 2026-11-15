# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Project Trigger inboxes and the durable Trigger target, byte-compatible with Go and TypeScript.

Type aliases:

| Name | Meaning |
|---|---|
| `TriggerPayloadDecoder` | Decodes a replayed payload from its JSON text and a copy of its stored bytes. |
| `TriggerPayloadEncoder` | Returns a payload's stored JSON value, which `encode_go_json_value` encodes. |
| `TriggerJSONPayload` | A payload as the default decoder yields it: a JSON object, array, or scalar. |
"""

import asyncio
import base64
import re
import unicodedata
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta, timezone
from typing import Any, Final, Generic, TypeAlias, TypeVar, cast, overload

from ..timestamps import _TIMESTAMP
from ..trigger_delivery import (
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerTarget,
    bind_trigger_log_attributes,
    deliver_trigger,
    is_trigger_undeliverable,
    log_trigger_record,
    trigger_error_attributes,
)
from ._raw_json import (
    InvalidRawJSONError,
    _compact_go_raw_json,
    _decode_go_string_field,
    _decode_inbox_document,
    _encode_go_string,
    _encode_inbox_document,
    _format_go_time,
    _go_text,
    _is_go_field_key,
    _raw_json_kind,
    _raw_json_object_members,
    encode_go_json_value,
    parse_trigger_payload_json,
)
from .models import Configuration, Scope
from .storage import ObjectConflictError, ObjectNotFoundError, ObjectStore, OutcomeUnknownError

P = TypeVar("P")
C = TypeVar("C")

TRIGGER_INBOX_SCHEMA_VERSION: Final = "connectors.dex.dev/project-trigger-inbox/v1alpha1"
"""Schema of one binding's inbox object, shared with the Go and TypeScript Connector SDKs."""

MAXIMUM_PENDING_TRIGGER_EVENTS: Final = 1000
"""One binding's pending backlog bound; a source stops acknowledging new events beyond it."""

MAXIMUM_TRIGGER_INBOX_BYTES: Final = 8 << 20
"""One inbox object's size bound in bytes; S3 project storage still refuses objects over 4 MiB."""

_MAXIMUM_INBOX_WRITE_ATTEMPTS = 8
_MAXIMUM_INBOX_NAME_BYTES = 256
_INBOX_CONNECTOR_ID = re.compile(r"[a-z][a-z0-9-]{1,62}", re.ASCII)
_INBOX_TRIGGER_NAME = re.compile(r"[a-z][A-Za-z0-9]{1,62}", re.ASCII)
_GO_SPACE_CHARACTERS = frozenset("\t\n\v\f\r \x85\xa0")


class TriggerInboxFullError(Exception):
    """A binding's backlog reached its bound; the source retries once the application consumes events."""

    def __init__(self) -> None:
        """Create the error with its fixed message."""
        super().__init__("project trigger inbox is full")


def _is_go_space(character: str) -> bool:
    return character in _GO_SPACE_CHARACTERS or unicodedata.category(character).startswith("Z")


def _is_valid_inbox_name(name: str) -> bool:
    return (
        bool(name)
        and len(name.encode("utf-8", "surrogatepass")) <= _MAXIMUM_INBOX_NAME_BYTES
        and not any(character in name for character in "\0\r\n")
        and not _is_go_space(name[0])
        and not _is_go_space(name[-1])
    )


def _base64_url(value: str) -> str:
    return base64.urlsafe_b64encode(value.encode("utf-8")).rstrip(b"=").decode("ascii")


@dataclass(frozen=True, slots=True)
class TriggerInboxKey:
    """One application binding of a connector Trigger."""

    connector_id: str
    """Manifest connector ID."""
    connection_name: str
    """Application-declared connection the Trigger reads."""
    trigger_name: str
    """Manifest Trigger name."""
    binding_name: str
    """Application binding of that Trigger."""

    def validate(self) -> None:
        """Require Go-valid identities: names of at most 256 UTF-8 bytes, no edge spaces or line breaks."""
        if not _INBOX_CONNECTOR_ID.fullmatch(self.connector_id) or not _INBOX_TRIGGER_NAME.fullmatch(
            self.trigger_name
        ):
            raise ValueError("trigger inbox connector or trigger identity is invalid")
        if not _is_valid_inbox_name(self.connection_name) or not _is_valid_inbox_name(self.binding_name):
            raise ValueError("trigger inbox connection or binding name is invalid")

    def path(self, scope: Scope) -> str:
        """Return the inbox head object key in scope, as Go and TypeScript write it."""
        self.validate()
        return (
            f"{scope.prefix}/trigger-inboxes/{self.connector_id}/{_base64_url(self.connection_name)}/"
            f"{self.trigger_name}/{_base64_url(self.binding_name)}/head"
        )


@dataclass(frozen=True, slots=True)
class PendingTriggerEvent:
    """One acknowledged Trigger event that the application has not yet consumed."""

    id: str
    """The source's stable event identity, decoded as Go decodes JSON strings."""
    event: bytes = field(repr=False)
    """The complete encoded event as raw JSON, kept exactly as stored."""


class TriggerInbox:
    """Stores acknowledged Trigger events in project storage until the application consumes them.

    A restarted or replaced replica delivers them in order. Writes use compare-and-swap against
    concurrent replicas. The object key and document match the Go and TypeScript inboxes byte for byte.
    """

    def __init__(self, objects: ObjectStore, scope: Scope, key: TriggerInboxKey) -> None:
        """Bind one binding's inbox in scope without I/O; raises ValueError for an invalid identity."""
        if objects is None:
            raise ValueError("trigger inbox object store is required")
        self._objects = objects
        self._identity = key
        self._key = key.path(scope)
        self._has_head = False

    @property
    def key(self) -> str:
        """The inbox head object key, for diagnostics and fixtures."""
        return self._key

    @property
    def identity(self) -> TriggerInboxKey:
        """The four-part binding identity this inbox belongs to."""
        return self._identity

    async def pending(self) -> tuple[PendingTriggerEvent, ...]:
        """Return the pending events in acknowledgement order."""
        events, _ = await self._read()
        return tuple(events)

    async def add(self, event: PendingTriggerEvent) -> None:
        """Append an event once; an ID already pending is unchanged.

        Raises ValueError for a blank ID or invalid JSON, and TriggerInboxFullError beyond 1000 events or
        8 MiB.
        """
        if not event.id.strip() or not _is_raw_json(event.event):
            raise ValueError("trigger inbox event identity or contents are invalid")

        def append(events: list[PendingTriggerEvent]) -> list[PendingTriggerEvent]:
            if any(pending.id == event.id for pending in events):
                return events
            if len(events) >= MAXIMUM_PENDING_TRIGGER_EVENTS:
                raise TriggerInboxFullError()
            return [*events, PendingTriggerEvent(event.id, bytes(event.event))]

        await self._update(append)

    async def remove(self, event_id: str) -> None:
        """Delete a consumed event; removing an absent event succeeds."""
        await self._update(lambda events: [pending for pending in events if pending.id != event_id])

    async def _update(self, change: Callable[[list[PendingTriggerEvent]], list[PendingTriggerEvent]]) -> None:
        for _ in range(_MAXIMUM_INBOX_WRITE_ATTEMPTS):
            current, etag = await self._read()
            changed = change(current)
            if [event.id for event in changed] == [event.id for event in current]:
                return
            contents = _encode_inbox_document([(event.id, event.event) for event in changed])
            if len(contents) > MAXIMUM_TRIGGER_INBOX_BYTES:
                raise TriggerInboxFullError()
            try:
                if etag:
                    await self._objects.compare_and_swap_object(self._key, etag, contents)
                else:
                    await self._objects.create_object(self._key, contents)
                return
            except OutcomeUnknownError:
                try:
                    observed = await self._objects.read_object(self._key)
                    if observed.contents == contents:
                        return
                except Exception:  # noqa: BLE001
                    # An unreadable head is retried like a conflict.
                    pass
            except ObjectConflictError:
                continue
        raise ObjectConflictError("project trigger inbox compare-and-swap conflict")

    async def _read(self) -> tuple[list[PendingTriggerEvent], str]:
        await self._create_head()
        try:
            stored = await self._objects.read_object(self._key)
        except ObjectNotFoundError:
            return [], ""
        if not stored.etag:
            raise ValueError("trigger inbox document is invalid")
        events = [
            PendingTriggerEvent(event_id, event)
            for event_id, event in _decode_inbox_document(stored.contents)
        ]
        return events, stored.etag

    async def _create_head(self) -> None:
        # S3 refuses reading a missing key like a permission error, so the head is created first.
        if self._has_head:
            return
        try:
            await self._objects.create_object(self._key, _encode_inbox_document([]))
            self._has_head = True
        except ObjectConflictError:
            self._has_head = True
        except OutcomeUnknownError:
            return


def _is_raw_json(value: bytes) -> bool:
    try:
        _compact_go_raw_json(value)
    except InvalidRawJSONError:
        return False
    return True


TriggerPayloadDecoder = Callable[[str, bytes], P]
"""Decodes a replayed payload from (JSON text with invalid UTF-8 replaced as Go does, a copy of its bytes)."""

TriggerPayloadEncoder = Callable[[P], object]
"""Returns the JSON value stored for a payload; `encode_go_json_value` encodes the result."""

TriggerJSONPayload: TypeAlias = dict[str, Any] | list[Any] | str | int | float | bool | None
"""A payload as `parse_trigger_payload_json`, the default replay decoder, yields it."""


def encode_trigger_event(
    event: TriggerEvent[P], encode_payload: TriggerPayloadEncoder[P] | None = None
) -> bytes:
    """Encode an event in Go's form `{"eventId","occurredAt","payload"}` with `encode_go_json_value`.

    encode_payload maps the payload to the JSON value to store first; None stores the payload itself,
    including a dataclass through the Dex codec. Raises InvalidRawJSONError for a naive `occurred_at` or
    a payload Go cannot encode; encoder errors propagate.
    """
    payload: object = encode_payload(event.payload) if encode_payload is not None else event.payload
    return (
        (
            f'{{"eventId":{_encode_go_string(event.id)},'
            f'"occurredAt":{_encode_go_string(_format_go_time(event.occurred_at))},"payload":'
        ).encode()
        + encode_go_json_value(payload)
        + b"}"
    )


def _parse_occurred_at(text: str) -> datetime:
    match = _TIMESTAMP.fullmatch(text)
    if match is None:
        raise ValueError("pending Trigger event time is invalid")
    year, month, day, hour, minute, second, _, fraction, offset = match.groups()
    try:
        zone = (
            UTC
            if offset == "Z"
            else timezone(
                (1 if offset[0] == "+" else -1) * timedelta(hours=int(offset[1:3]), minutes=int(offset[4:6]))
            )
        )
        # Sub-microsecond digits are truncated; the stored bytes keep Go's nanoseconds.
        microsecond = int(((fraction or "") + "000000")[:6])
        return datetime(
            int(year), int(month), int(day), int(hour), int(minute), int(second), microsecond, tzinfo=zone
        )
    except ValueError:
        raise ValueError("pending Trigger event time is invalid") from None


def _default_payload_decoder(payload_json: str, payload_bytes: bytes) -> Any:
    return parse_trigger_payload_json(payload_bytes)


@overload
def decode_trigger_event(raw: bytes, decode_payload: TriggerPayloadDecoder[P]) -> TriggerEvent[P]: ...


@overload
def decode_trigger_event(raw: bytes, decode_payload: None = None) -> TriggerEvent[TriggerJSONPayload]: ...


def decode_trigger_event(
    raw: bytes, decode_payload: TriggerPayloadDecoder[Any] | None = None
) -> TriggerEvent[Any]:
    """Decode an event that `encode_trigger_event` or Go wrote, as Go's json.Unmarshal into a struct.

    Unknown fields are ignored, keys match case-insensitively, the last duplicate wins, and a missing
    payload is null. `occurred_at` keeps microseconds; finer digits are truncated. decode_payload gets the
    payload JSON text and a copy of its bytes; None uses `parse_trigger_payload_json`. Raises ValueError
    for a non-object or an invalid time; decoder errors propagate.
    """
    raw = bytes(raw)
    try:
        if _raw_json_kind(raw) != "object":
            raise ValueError("pending Trigger event is not a JSON object")
        event_id = ""
        occurred_at = ""
        payload = b"null"
        for key, value in _raw_json_object_members(raw):
            if _is_go_field_key(key, "eventId"):
                event_id = _decode_go_string_field(value, event_id)
            elif _is_go_field_key(key, "occurredAt"):
                occurred_at = _decode_go_string_field(value, occurred_at)
            elif _is_go_field_key(key, "payload"):
                payload = value
    except InvalidRawJSONError:
        raise ValueError("pending Trigger event is not a JSON object") from None
    time = _parse_occurred_at(occurred_at)
    decoder: TriggerPayloadDecoder[Any] = (
        decode_payload if decode_payload is not None else _default_payload_decoder
    )
    return TriggerEvent(event_id, time, decoder(_go_text(payload), bytes(payload)))


def decode_trigger_configuration(
    configuration: Configuration, key: TriggerInboxKey, decode: Callable[[dict[str, Any]], C]
) -> C:
    """Decode one Trigger binding's configuration with decode, which must reject unknown fields.

    The binding is the `triggerBindings` entry whose four identity fields equal key. Raises
    ObjectNotFoundError when the project has no such binding, ValueError when its configuration is not an
    object, and whatever decode raises.
    """
    for entry in configuration.trigger_bindings:
        if (
            entry.get("connectorId") == key.connector_id
            and entry.get("connectionName") == key.connection_name
            and entry.get("triggerName") == key.trigger_name
            and entry.get("bindingName") == key.binding_name
        ):
            value = entry.get("configuration")
            if not isinstance(value, dict):
                raise ValueError("trigger binding configuration must be an object")
            return decode(value)
    raise ObjectNotFoundError("trigger binding configuration not found")


class _DurableTriggerTarget(Generic[P]):
    def __init__(
        self,
        inbox: TriggerInbox,
        target: TriggerTarget[P],
        logger: TriggerLogger,
        decode_payload: TriggerPayloadDecoder[P] | None,
        encode_payload: TriggerPayloadEncoder[P] | None,
    ) -> None:
        self._inbox = inbox
        self._target = target
        self._logger = logger
        self._decode_payload = decode_payload
        self._encode_payload = encode_payload
        self._lock = asyncio.Lock()
        self._pending_removal: dict[str, bool] = {}

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        if not event.id.strip():
            raise ValueError("Trigger event ID is required")
        encoded = encode_trigger_event(event, self._encode_payload)
        async with self._lock:
            # A source that abandoned this acknowledgement, such as a timed-out webhook, must not record late.
            context.cancellation.raise_if_cancelled()
            try:
                await self._inbox.add(PendingTriggerEvent(event.id, encoded))
            except Exception as error:
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.ERROR,
                    "trigger inbox write failed",
                    {"event_id": event.id, **trigger_error_attributes(error)},
                )
                raise

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        async with self._lock:
            await self._consume(context, event)

    async def replay_trigger_deliveries(self, context: TriggerContext) -> None:
        try:
            pending = await self._inbox.pending()
        except Exception as error:
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.ERROR,
                "trigger inbox read failed",
                trigger_error_attributes(error),
            )
            raise
        if not pending:
            return
        log_trigger_record(
            self._logger,
            context,
            TriggerLogLevel.INFO,
            "replaying pending trigger events",
            {"count": len(pending)},
        )
        delivered = skipped = 0
        for index, stored in enumerate(pending):
            try:
                decoder = cast(TriggerPayloadDecoder[P], self._decode_payload or _default_payload_decoder)
                event: TriggerEvent[P] = decode_trigger_event(stored.event, decoder)
            except Exception as error:
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.ERROR,
                    "trigger inbox event decode failed",
                    {"event_id": stored.id, **trigger_error_attributes(error)},
                )
                raise RuntimeError("decode pending Trigger event") from error
            attempt_target = _PendingReplayAttempt(self)
            try:
                await deliver_trigger(context, attempt_target, event, logger=self._logger)
            except Exception as error:
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.INFO,
                    "finished replaying pending trigger events",
                    {
                        "delivered": delivered,
                        "skipped": skipped,
                        "remaining": len(pending) - index,
                        **trigger_error_attributes(error),
                    },
                )
                raise
            if attempt_target.is_skipped:
                skipped += 1
            else:
                delivered += 1
        log_trigger_record(
            self._logger,
            context,
            TriggerLogLevel.INFO,
            "finished replaying pending trigger events",
            {"delivered": delivered, "skipped": skipped, "remaining": 0},
        )

    async def _handle_pending(self, context: TriggerContext, event: TriggerEvent[P]) -> bool:
        try:
            pending = await self._inbox.pending()
        except Exception as error:
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.ERROR,
                "trigger inbox read failed",
                {"event_id": event.id, **trigger_error_attributes(error)},
            )
            raise
        if not any(stored.id == event.id for stored in pending):
            return False
        return await self._consume(context, event)

    async def _consume(self, context: TriggerContext, event: TriggerEvent[P]) -> bool:
        is_skipped = self._pending_removal.get(event.id)
        if is_skipped is None:
            is_skipped = False
            try:
                await self._target.handle_trigger(context, event)
            except Exception as error:
                if not is_trigger_undeliverable(error):
                    raise
                is_skipped = True
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.WARN,
                    "trigger event skipped: undeliverable",
                    {"event_id": event.id, **trigger_error_attributes(error)},
                )
        # The enclosing delivery attempt must not also report this event as delivered.
        if is_skipped:
            context.record_skip()
        try:
            await self._inbox.remove(event.id)
        except Exception as error:
            self._pending_removal[event.id] = is_skipped
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.ERROR,
                "trigger inbox remove failed",
                {"event_id": event.id, **trigger_error_attributes(error)},
            )
            raise
        self._pending_removal.pop(event.id, None)
        return is_skipped


class _PendingReplayAttempt(Generic[P]):
    def __init__(self, durable: _DurableTriggerTarget[P]) -> None:
        self._durable = durable
        self.is_skipped = False

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        # The lock covers one attempt, so prepare_trigger never waits for a pending event's backoff.
        async with self._durable._lock:
            self.is_skipped = await self._durable._handle_pending(context, event)


@overload
def new_durable_trigger_target(
    inbox: TriggerInbox,
    target: TriggerTarget[P],
    *,
    decode_payload: TriggerPayloadDecoder[P],
    encode_payload: TriggerPayloadEncoder[P] | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]: ...


@overload
def new_durable_trigger_target(
    inbox: TriggerInbox,
    target: TriggerTarget[TriggerJSONPayload],
    *,
    decode_payload: None = None,
    encode_payload: TriggerPayloadEncoder[TriggerJSONPayload] | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[TriggerJSONPayload]: ...


def new_durable_trigger_target(
    inbox: TriggerInbox,
    target: TriggerTarget[Any],
    *,
    decode_payload: TriggerPayloadDecoder[Any] | None = None,
    encode_payload: TriggerPayloadEncoder[Any] | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[Any]:
    """Store acknowledged events in the project Trigger inbox until target consumes them.

    The target consumes an event by returning or by raising UndeliverableTriggerError; any other error
    keeps it pending. Replay delivers pending events in order with `deliver_trigger`, and a source must
    deliver new events only after replay returns. Inbox failures raise like target failures, so callers
    retry them; when only a removal failed, a retry removes the event without calling target again.
    `prepare_trigger` refuses a write once the context is cancelled. Records carry the binding identity
    from `inbox.identity`. decode_payload decodes replayed payloads; None uses
    `parse_trigger_payload_json`, so a target with another payload type, such as a dataclass, must pass
    one, which the overloads enforce. encode_payload maps a payload to its stored JSON value; None
    stores the payload itself, a dataclass through the Dex codec. Logs WARN for undeliverable skips,
    ERROR for inbox failures, and INFO at the start and end of a replay that finds pending events.
    """
    if inbox is None or target is None:
        raise ValueError("project trigger inbox and Trigger target are required")
    identity = inbox.identity
    logger = bind_trigger_log_attributes(
        logger,
        {
            "connector": identity.connector_id,
            "connection": identity.connection_name,
            "trigger": identity.trigger_name,
            "binding": identity.binding_name,
        },
    )
    return _DurableTriggerTarget(inbox, target, logger, decode_payload, encode_payload)


__all__ = [
    "MAXIMUM_PENDING_TRIGGER_EVENTS",
    "MAXIMUM_TRIGGER_INBOX_BYTES",
    "TRIGGER_INBOX_SCHEMA_VERSION",
    "PendingTriggerEvent",
    "TriggerInbox",
    "TriggerInboxFullError",
    "TriggerInboxKey",
    "TriggerJSONPayload",
    "TriggerPayloadDecoder",
    "TriggerPayloadEncoder",
    "decode_trigger_configuration",
    "decode_trigger_event",
    "encode_trigger_event",
    "new_durable_trigger_target",
]
