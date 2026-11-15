# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable poll cursors in project storage, byte-compatible with the Go Connector SDK."""

import re
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Final

from ._raw_json import (
    InvalidRawJSONError,
    _compact_go_raw_json,
    _decode_go_string_field,
    _encode_go_string,
    _is_go_field_key,
    _raw_json_array_elements,
    _raw_json_kind,
    _raw_json_object_members,
    _skip_whitespace,
    _well_formed,
)
from .models import Scope
from .storage import ObjectConflictError, ObjectNotFoundError, ObjectStore, OutcomeUnknownError
from .trigger_inbox import MAXIMUM_TRIGGER_INBOX_BYTES, TriggerInboxKey, _base64_url, _is_valid_inbox_name

POLL_CURSOR_SCHEMA_VERSION: Final = "connectors.dex.dev/project-poll-cursor/v1alpha1"
"""Schema of one poll cursor head object, shared with the Go Connector SDK."""

MAXIMUM_POLL_CURSOR_TOKEN_BYTES: Final = 64 << 10
"""Bound of `PollCursorState.token` in UTF-8 bytes."""

MAXIMUM_KEYED_SNAPSHOT_ENTRIES: Final = 100_000
"""Hard bound on the entries of one stored keyed snapshot."""

MAXIMUM_POLL_CURSOR_DOCUMENT_BYTES: Final = MAXIMUM_TRIGGER_INBOX_BYTES
"""Bound of one encoded poll cursor document, 8 MiB as in Go; S3 project storage refuses over 4 MiB."""

_MAXIMUM_KEYED_SNAPSHOT_KEY_BYTES = 1 << 10
_CONNECTOR_ID = re.compile(r"[a-z][a-z0-9-]{1,62}", re.ASCII)
_TRIGGER_OR_FEED_NAME = re.compile(r"[a-z][A-Za-z0-9]{1,62}", re.ASCII)
_KEYED_SNAPSHOT_HASH = re.compile(r"[0-9a-f]{16,64}", re.ASCII)


class PollCursorTooLargeError(ValueError):
    """A poll cursor state exceeds a storage bound; no retry of the same state can succeed.

    Raised for a token over `MAXIMUM_POLL_CURSOR_TOKEN_BYTES`, a snapshot over its entry bound, or an
    encoded document over 8 MiB. The connector or its configuration must produce a smaller state.
    """


class PollCursorDocumentInvalidError(ValueError):
    """A stored poll cursor document fails strict decoding or has another schema version.

    The store never overwrites such a document; an operator must inspect it.
    """


def _utf8_length(text: str) -> int:
    return len(_well_formed(text).encode("utf-8"))


def _go_order(text: str) -> bytes:
    """Return the bytes Go compares strings by, so sorting matches Go's byte order."""
    return _well_formed(text).encode("utf-8")


@dataclass(frozen=True, slots=True)
class PollCursorKey:
    """One Trigger binding's poll cursor identity: the same four parts as a `TriggerInboxKey`."""

    connector_id: str
    """Manifest connector ID."""
    connection_name: str
    """Application-declared connection the Trigger reads."""
    trigger_name: str
    """Manifest Trigger name."""
    binding_name: str
    """Application binding of that Trigger."""

    @classmethod
    def from_trigger_inbox_key(cls, key: TriggerInboxKey) -> "PollCursorKey":
        """Return the cursor key of the binding that owns an inbox key, as Go's `PollCursorKey(inboxKey)`."""
        return cls(key.connector_id, key.connection_name, key.trigger_name, key.binding_name)

    def path(self, scope: Scope) -> str:
        """Return the head key in scope, as Go writes it.

        The key is `<scope>/poll-cursors/<connector>/<b64url(connection)>/<trigger>/<b64url(binding)>/head`.
        Raises ValueError for an invalid identity.
        """
        _validate_poll_identity(self.connector_id, self.trigger_name, self.connection_name, self.binding_name)
        return (
            f"{scope.prefix}/poll-cursors/{self.connector_id}/{_base64_url(self.connection_name)}/"
            f"{self.trigger_name}/{_base64_url(self.binding_name)}/head"
        )


@dataclass(frozen=True, slots=True)
class PollFeedKey:
    """One poll cursor shared by several Trigger bindings of one connection, such as one change feed."""

    connector_id: str
    """Manifest connector ID."""
    connection_name: str
    """Application-declared connection the feed reads."""
    feed_name: str
    """The connector's stable lower camel case feed name, such as "driveChanges"."""

    def path(self, scope: Scope) -> str:
        """Return the head key `<scope>/poll-feeds/<connector>/<b64url(connection)>/<feed>/head`.

        Raises ValueError for an invalid identity.
        """
        _validate_poll_identity(self.connector_id, self.feed_name, self.connection_name)
        connection = _base64_url(self.connection_name)
        return f"{scope.prefix}/poll-feeds/{self.connector_id}/{connection}/{self.feed_name}/head"


def _validate_poll_identity(connector_id: str, trigger_or_feed_name: str, *names: str) -> None:
    if not _CONNECTOR_ID.fullmatch(connector_id) or not _TRIGGER_OR_FEED_NAME.fullmatch(trigger_or_feed_name):
        raise ValueError("poll cursor connector, trigger, or feed identity is invalid")
    if not all(_is_valid_inbox_name(name) for name in names):
        raise ValueError("poll cursor connection or binding name is invalid")


@dataclass(frozen=True, slots=True)
class KeyedSnapshotEntry:
    """One tracked provider item in a keyed snapshot."""

    key: str
    """The connector's stable item key, 1 byte to 1 KiB of UTF-8."""
    hash: str
    """The item's content hash: lowercase hex, 16 to 64 characters."""
    seen: int = 0
    """CommitCount of the commit that last added, changed, or reported this entry.

    Zero in a state being committed means "this commit"; the poll Trigger kit replaces it before writing.
    """
    value: bytes | None = field(default=None, repr=False)
    """The item's optional raw JSON value, kept only when a poller stores values; never JSON null."""


@dataclass(frozen=True, slots=True)
class KeyedSnapshot:
    """A bounded set of tracked provider items stored with a poll cursor, sorted by unique key."""

    entries: tuple[KeyedSnapshotEntry, ...] = ()
    """The tracked items, sorted ascending by key in UTF-8 byte order, as Go sorts them."""


@dataclass(frozen=True, slots=True)
class PollCursorState:
    """A committed provider position; every field belongs to the stored document contract."""

    token: str = ""
    """The connector's opaque encoded position, at most 64 KiB of UTF-8. Empty is valid."""
    snapshot: KeyedSnapshot | None = None
    """The optional keyed snapshot committed atomically with the token; None when unused."""
    seeded_at_unix_ms: int = 0
    """Time of the latest seed, in milliseconds since the Unix epoch."""
    committed_at_unix_ms: int = 0
    """Time of this commit, in milliseconds since the Unix epoch."""
    commit_count: int = 0
    """Counts every commit, seeds included; it makes each commit's bytes unique for reconciliation."""
    reset_count: int = 0
    """Counts reseeds after the provider rejected the cursor permanently."""
    last_reset_at_unix_ms: int | None = None
    """Time of the latest reseed in milliseconds, or None before the first."""


@dataclass(frozen=True, slots=True)
class PollCursorRecord:
    """One read of a poll cursor head."""

    state: PollCursorState | None
    """The committed position, or None until the cursor is seeded."""
    revision: str
    """The opaque compare-and-swap token for `replace_poll_cursor`."""


class PollCursorStore:
    """Reads and conditionally replaces one poll cursor head in project storage, beside the Trigger inboxes.

    Concurrent replicas are ordered by compare-and-swap. The object key and document bytes match the Go
    Connector SDK, so Go and Python replicas can share one cursor.
    """

    def __init__(self, objects: ObjectStore, scope: Scope, key: PollCursorKey | PollFeedKey) -> None:
        """Bind one binding's or feed's cursor in scope without I/O; raises ValueError for a bad identity."""
        if objects is None:
            raise ValueError("poll cursor object store is required")
        self._objects = objects
        self._key = key.path(scope)
        self._has_head = False

    @property
    def key(self) -> str:
        """The cursor head object key, for diagnostics and fixtures."""
        return self._key

    async def read_poll_cursor(self) -> PollCursorRecord:
        """Return the current record; its state is None until the cursor is seeded.

        Before the first read the store creates the unseeded head with a create-only write, so it never
        reads a missing key and a restart learns whether the cursor was seeded without a provider call.
        Raises PollCursorDocumentInvalidError when the stored document fails strict decoding.
        """
        await self._create_head()
        try:
            stored = await self._objects.read_object(self._key)
        except ObjectNotFoundError:
            return PollCursorRecord(None, "")
        state = decode_poll_cursor_document(stored.contents)
        if not stored.etag:
            raise PollCursorDocumentInvalidError("project poll cursor document is invalid: missing revision")
        self._has_head = True
        return PollCursorRecord(state, stored.etag)

    async def replace_poll_cursor(self, expected_revision: str, state: PollCursorState) -> PollCursorRecord:
        """Write state only while the head is still at expected_revision.

        Raises ObjectConflictError when another writer changed the head first, PollCursorTooLargeError for
        a state over a storage bound, and ValueError for another invalid state. An unknown write outcome is
        reconciled by reading the head back and comparing bytes; a mismatch is ObjectConflictError. It
        never retries with another precondition: the caller re-reads and decides again.
        """
        contents = encode_poll_cursor_document(state)
        try:
            if expected_revision:
                written = await self._objects.compare_and_swap_object(self._key, expected_revision, contents)
            else:
                written = await self._objects.create_object(self._key, contents)
        except OutcomeUnknownError as unknown_outcome:
            try:
                observed = await self._objects.read_object(self._key)
            except Exception:  # noqa: BLE001
                # An unreadable head leaves the outcome unknown, as Go reports it.
                raise unknown_outcome from None
            if observed.contents != contents:
                raise ObjectConflictError("project poll cursor compare-and-swap conflict") from None
            written = observed
        self._has_head = True
        return PollCursorRecord(state, written.etag)

    async def _create_head(self) -> None:
        # S3 refuses reading a missing key like a permission error, so the head is created first.
        if self._has_head:
            return
        try:
            await self._objects.create_object(self._key, encode_poll_cursor_document(None))
            self._has_head = True
        except ObjectConflictError:
            self._has_head = True
        except OutcomeUnknownError:
            return


def validate_poll_cursor_state(state: PollCursorState) -> None:
    """Check a state's storage bounds and snapshot ordering.

    Raises PollCursorTooLargeError for an oversize token or snapshot, and ValueError for an unsorted or
    duplicate snapshot key, a malformed hash, a null value, or a negative counter.
    """
    token_bytes = _utf8_length(state.token)
    if token_bytes > MAXIMUM_POLL_CURSOR_TOKEN_BYTES:
        raise PollCursorTooLargeError(
            f"project poll cursor state exceeds its bound: token is {token_bytes} bytes"
        )
    if state.commit_count < 0 or state.reset_count < 0:
        raise ValueError("poll cursor counters must not be negative")
    if state.snapshot is not None:
        validate_keyed_snapshot(state.snapshot)


def validate_keyed_snapshot(snapshot: KeyedSnapshot) -> None:
    """Check that entries are sorted by unique key, keys are 1 byte to 1 KiB, hashes are lowercase hex of
    16 to 64 characters, values are JSON other than null, and at most 100 000 entries exist.

    Raises PollCursorTooLargeError over the entry bound and ValueError otherwise.
    """
    entries = snapshot.entries
    if len(entries) > MAXIMUM_KEYED_SNAPSHOT_ENTRIES:
        raise PollCursorTooLargeError(
            f"project poll cursor state exceeds its bound: snapshot has {len(entries)} entries"
        )
    previous_key: bytes | None = None
    for entry in entries:
        key = _go_order(entry.key)
        if not key or len(key) > _MAXIMUM_KEYED_SNAPSHOT_KEY_BYTES:
            raise ValueError("keyed snapshot key must be 1 byte to 1 KiB")
        if previous_key is not None and previous_key >= key:
            raise ValueError("keyed snapshot entries must be sorted by unique key")
        previous_key = key
        if not _KEYED_SNAPSHOT_HASH.fullmatch(entry.hash):
            raise ValueError("keyed snapshot hash must be lowercase hex of 16 to 64 characters")
        if entry.seen < 0:
            raise ValueError("keyed snapshot seen counter must not be negative")
        if entry.value is not None and not _is_json_other_than_null(entry.value):
            raise ValueError("keyed snapshot value must be JSON other than null")


def _is_json_other_than_null(value: bytes) -> bool:
    try:
        return _compact_go_raw_json(value) != b"null"
    except InvalidRawJSONError:
        return False


def encode_poll_cursor_document(state: PollCursorState | None) -> bytes:
    """Encode a cursor document exactly as Go marshals it: field order, compact, HTML-safe escapes.

    None encodes the unseeded head. Snapshot values are compacted as Go writes a json.RawMessage.
    Raises PollCursorTooLargeError over a bound, including an encoded document over 8 MiB, and
    ValueError for another invalid state.
    """
    prefix = f'{{"schemaVersion":{_encode_go_string(POLL_CURSOR_SCHEMA_VERSION)},"state":'.encode()
    if state is None:
        return prefix + b"null}"
    validate_poll_cursor_state(state)
    parts = [prefix, f'{{"token":{_encode_go_string(state.token)},"snapshot":'.encode()]
    if state.snapshot is None:
        parts.append(b"null")
    else:
        parts.append(b'{"entries":[')
        for index, entry in enumerate(state.snapshot.entries):
            parts.append(
                f'{"," if index else ""}{{"key":{_encode_go_string(entry.key)},'
                f'"hash":{_encode_go_string(entry.hash)},"seen":{entry.seen}'.encode()
            )
            if entry.value is not None:
                parts.append(b',"value":' + _compact_go_raw_json(entry.value))
            parts.append(b"}")
        parts.append(b"]}")
    last_reset = "null" if state.last_reset_at_unix_ms is None else str(state.last_reset_at_unix_ms)
    parts.append(
        f',"seededAtUnixMs":{state.seeded_at_unix_ms},"committedAtUnixMs":{state.committed_at_unix_ms},'
        f'"commitCount":{state.commit_count},"resetCount":{state.reset_count},'
        f'"lastResetAtUnixMs":{last_reset}}}}}'.encode()
    )
    contents = b"".join(parts)
    if len(contents) > MAXIMUM_POLL_CURSOR_DOCUMENT_BYTES:
        raise PollCursorTooLargeError(
            f"project poll cursor state exceeds its bound: encoded document is {len(contents)} bytes"
        )
    return contents


_INT64_LITERAL = re.compile(rb"-?[0-9]+")


@dataclass(slots=True)
class _DecodedEntry:
    key: str = ""
    hash: str = ""
    seen: int = 0
    value: bytes | None = None


@dataclass(slots=True)
class _DecodedSnapshot:
    entries: list[_DecodedEntry] | None = None
    # Go reuses a slice's earlier elements when a duplicate key decodes another array into it.
    backing: list[_DecodedEntry] = field(default_factory=list)


@dataclass(slots=True)
class _DecodedState:
    token: str = ""
    snapshot: _DecodedSnapshot | None = None
    seeded_at_unix_ms: int = 0
    committed_at_unix_ms: int = 0
    commit_count: int = 0
    reset_count: int = 0
    last_reset_at_unix_ms: int | None = None


def decode_poll_cursor_document(contents: bytes) -> PollCursorState | None:
    """Decode a cursor document strictly, as Go's json.Decoder with DisallowUnknownFields does.

    Keys match case-insensitively, the last duplicate wins, a duplicate object merges into the earlier
    one, null leaves a field unchanged, and invalid UTF-8 in strings becomes U+FFFD. Returns None for the
    unseeded head. Raises PollCursorDocumentInvalidError for any other schema, an unknown field, a
    malformed value, trailing data, or a state that fails `validate_poll_cursor_state`.
    """
    try:
        contents = bytes(contents)
        if _raw_json_kind(contents) != "object":
            raise InvalidRawJSONError()
        schema_version = ""
        state: _DecodedState | None = None
        for key, value in _raw_json_object_members(contents):
            if _is_go_field_key(key, "schemaVersion"):
                schema_version = _decode_go_string_field(value, schema_version)
            elif _is_go_field_key(key, "state"):
                state = _decode_state(value, state)
            else:
                raise InvalidRawJSONError()
        if schema_version != POLL_CURSOR_SCHEMA_VERSION:
            raise InvalidRawJSONError()
    except (InvalidRawJSONError, IndexError, KeyError, ValueError):
        raise PollCursorDocumentInvalidError("project poll cursor document is invalid") from None
    if state is None:
        return None
    decoded = _freeze_state(state)
    try:
        validate_poll_cursor_state(decoded)
    except ValueError as error:
        raise PollCursorDocumentInvalidError(f"project poll cursor document is invalid: {error}") from None
    return decoded


def _decode_struct_fields(raw: bytes, fields: dict[str, Callable[[bytes], None]]) -> bool:
    """Apply each member to its field handler; return False for null, which leaves a Go struct unchanged."""
    kind = _raw_json_kind(raw)
    if kind == "null":
        return False
    if kind != "object":
        raise InvalidRawJSONError()
    for key, value in _raw_json_object_members(raw):
        handler = fields.get(key) or next(
            (candidate for name, candidate in fields.items() if _is_go_field_key(key, name)), None
        )
        if handler is None:
            raise InvalidRawJSONError()
        handler(value)
    return True


def _decode_int64(raw: bytes, current: int) -> int:
    kind = _raw_json_kind(raw)
    if kind == "null":
        return current
    literal = raw[_skip_whitespace(raw, 0) :]
    if kind != "number" or not _INT64_LITERAL.fullmatch(literal):
        raise InvalidRawJSONError()
    value = int(literal)
    if not -(1 << 63) <= value < 1 << 63:
        raise InvalidRawJSONError()
    return value


def _decode_state(raw: bytes, current: _DecodedState | None) -> _DecodedState | None:
    if _raw_json_kind(raw) == "null":
        return None
    state = current if current is not None else _DecodedState()

    def set_snapshot(value: bytes) -> None:
        state.snapshot = _decode_snapshot(value, state.snapshot)

    def set_last_reset(value: bytes) -> None:
        state.last_reset_at_unix_ms = None if _raw_json_kind(value) == "null" else _decode_int64(value, 0)

    def integer_setter(name: str) -> Callable[[bytes], None]:
        return lambda value: setattr(state, name, _decode_int64(value, getattr(state, name)))

    _decode_struct_fields(
        raw,
        {
            "token": lambda value: setattr(state, "token", _decode_go_string_field(value, state.token)),
            "snapshot": set_snapshot,
            "seededAtUnixMs": integer_setter("seeded_at_unix_ms"),
            "committedAtUnixMs": integer_setter("committed_at_unix_ms"),
            "commitCount": integer_setter("commit_count"),
            "resetCount": integer_setter("reset_count"),
            "lastResetAtUnixMs": set_last_reset,
        },
    )
    return state


def _decode_snapshot(raw: bytes, current: _DecodedSnapshot | None) -> _DecodedSnapshot | None:
    if _raw_json_kind(raw) == "null":
        return None
    snapshot = current if current is not None else _DecodedSnapshot()

    def set_entries(value: bytes) -> None:
        kind = _raw_json_kind(value)
        if kind == "null":
            snapshot.entries, snapshot.backing = None, []
            return
        if kind != "array":
            raise InvalidRawJSONError()
        elements = _raw_json_array_elements(value)
        while len(snapshot.backing) < len(elements):
            snapshot.backing.append(_DecodedEntry())
        for element, entry in zip(elements, snapshot.backing, strict=False):
            _decode_entry(element, entry)
        snapshot.entries = snapshot.backing[: len(elements)]

    _decode_struct_fields(raw, {"entries": set_entries})
    return snapshot


def _decode_entry(raw: bytes, entry: _DecodedEntry) -> None:
    def set_value(value: bytes) -> None:
        entry.value = bytes(value)

    _decode_struct_fields(
        raw,
        {
            "key": lambda value: setattr(entry, "key", _decode_go_string_field(value, entry.key)),
            "hash": lambda value: setattr(entry, "hash", _decode_go_string_field(value, entry.hash)),
            "seen": lambda value: setattr(entry, "seen", _decode_int64(value, entry.seen)),
            "value": set_value,
        },
    )


def _freeze_state(state: _DecodedState) -> PollCursorState:
    snapshot = None
    if state.snapshot is not None:
        snapshot = KeyedSnapshot(
            tuple(
                KeyedSnapshotEntry(entry.key, entry.hash, entry.seen, entry.value)
                for entry in state.snapshot.entries or ()
            )
        )
    return PollCursorState(
        state.token,
        snapshot,
        state.seeded_at_unix_ms,
        state.committed_at_unix_ms,
        state.commit_count,
        state.reset_count,
        state.last_reset_at_unix_ms,
    )


__all__ = [
    "MAXIMUM_KEYED_SNAPSHOT_ENTRIES",
    "MAXIMUM_POLL_CURSOR_DOCUMENT_BYTES",
    "MAXIMUM_POLL_CURSOR_TOKEN_BYTES",
    "POLL_CURSOR_SCHEMA_VERSION",
    "KeyedSnapshot",
    "KeyedSnapshotEntry",
    "PollCursorDocumentInvalidError",
    "PollCursorKey",
    "PollCursorRecord",
    "PollCursorState",
    "PollCursorStore",
    "PollCursorTooLargeError",
    "PollFeedKey",
    "decode_poll_cursor_document",
    "encode_poll_cursor_document",
    "validate_keyed_snapshot",
    "validate_poll_cursor_state",
]
