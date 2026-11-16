# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Drive changes.list feed of the file Triggers: classification, the cursor, and the fetcher.

Drive change records carry no change kind, so each usable file change is classified against the
checkpoint of the previous complete pass, at behavior parity with the Go connector. The cursor
token is the Go connector's JSON document byte for byte, so Go and Python replicas share a feed.
"""

import json
import re
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, TriggerContext, TriggerEvent, TriggerRef
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshotEntry
from dex_connectors.sdk.providerhttp import read_error_tokens
from dex_connectors.sdk.timestamps import format_rfc3339_nano

from ._generated.definitions import CONNECTOR_ID
from .client import Client, query_string
from .json_values import strict_json_loads
from .models import FileEvent, FileTriggerConfiguration
from .operations import _drive_id, _DriveTimestamp, _timestamp

FILE_CREATED_TRIGGER_NAME: Final = "fileCreated"
FILE_UPDATED_TRIGGER_NAME: Final = "fileUpdated"
FILE_TRASHED_TRIGGER_NAME: Final = "fileTrashed"
FILE_CHANGES_FEED_NAME: Final = "fileChanges"
"""The stable storage identity of the shared changes.list cursor."""

# Drive does not promise change records in timestamp order; stable event IDs absorb the repeats.
_CLASSIFICATION_LOOKBACK: Final = timedelta(minutes=5)
# Separates an edit from the write that created the file.
_MODIFIED_AFTER_CREATION_THRESHOLD: Final = timedelta(seconds=1)
_CHANGE_FILE_FIELDS: Final = (
    "nextPageToken,newStartPageToken,changes(changeType,removed,fileId,time,"
    "file(id,name,mimeType,parents,driveId,createdTime,modifiedTime,trashed,trashedTime,size,md5Checksum,webViewLink))"
)
_GOOGLE_ERROR_TOKEN_POINTERS: Final = ("/error/errors/0/reason", "/error/status")
_GOOGLE_RATE_LIMIT_REASONS: Final = frozenset({"rateLimitExceeded", "userRateLimitExceeded"})
_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_GO_INT64 = re.compile(r"[+-]?[0-9]+", re.ASCII)
_LONE_SURROGATE = re.compile("[\ud800-\udfff]")
# mime.ParseMediaType token characters, lowercase because the parsed type must equal the value.
_PLAIN_MEDIA_TYPE = re.compile(r"[a-z0-9!#$%&'*+\-.^_`{|}~]+/[a-z0-9!#$%&'*+\-.^_`{|}~]+", re.ASCII)


def validate_file_trigger_configuration(configuration: FileTriggerConfiguration) -> None:
    """Check that folder_id is a Drive ID and mime_type a plain lowercase type/subtype when set.

    Raises ValueError otherwise, as the Go connector's FileTriggerConfiguration.Validate does.
    """
    if configuration is None:
        raise ValueError("Google Drive file Trigger configuration is required")
    if configuration.folder_id and not _drive_id(configuration.folder_id):
        raise ValueError("Google Drive file Trigger folderId must be a Drive folder ID")
    if configuration.mime_type and _PLAIN_MEDIA_TYPE.fullmatch(configuration.mime_type) is None:
        raise ValueError(
            "Google Drive file Trigger mimeType must be one lowercase type/subtype without parameters"
        )


def _configuration_matches(configuration: FileTriggerConfiguration, file: FileEvent) -> bool:
    if configuration.folder_id and configuration.folder_id not in file.parents:
        return False
    return not configuration.mime_type or configuration.mime_type == file.mime_type


@dataclass(frozen=True, slots=True)
class FileChange:
    """One usable changes.list entry with the Trigger events it produces.

    The fetcher classifies it against the cursor checkpoint, so a refetch yields the same events.
    """

    file: FileEvent
    created: TriggerEvent[FileEvent] | None = None
    updated: TriggerEvent[FileEvent] | None = None
    trashed: TriggerEvent[FileEvent] | None = None

    def event_for(self, trigger_name: str) -> TriggerEvent[FileEvent] | None:
        """Return this change's event of one file Trigger, or None."""
        if trigger_name == FILE_CREATED_TRIGGER_NAME:
            return self.created
        if trigger_name == FILE_UPDATED_TRIGGER_NAME:
            return self.updated
        if trigger_name == FILE_TRASHED_TRIGGER_NAME:
            return self.trashed
        return None


def _is_zero_time(value: datetime) -> bool:
    return value == _ZERO_TIME


def _unix_milliseconds(value: datetime) -> int:
    return (value - _UNIX_EPOCH) // timedelta(milliseconds=1)


def _file_trigger_event(file: FileEvent, kind: str, occurred_at: datetime) -> TriggerEvent[FileEvent]:
    # One ID per file, kind, and provider time, so each edit or trash is one event however often refetched.
    event_id = f"{file.file_id}@{kind}@{_unix_milliseconds(occurred_at)}"
    return TriggerEvent(event_id, occurred_at, file)


def classify_file_change(file: FileEvent, checkpoint: datetime) -> FileChange:
    """Derive events from file state alone, because Drive changes carry no change kind.

    A trashed file produces only fileTrashed. Otherwise a file created after the lookback produces
    fileCreated, and one modified after the lookback and after its creation write produces fileUpdated.
    """
    lookback = checkpoint - _CLASSIFICATION_LOOKBACK
    if file.is_trashed:
        trashed_at = file.change_time if _is_zero_time(file.trashed_time) else file.trashed_time
        if trashed_at > lookback:
            return FileChange(file, trashed=_file_trigger_event(file, "trashed", trashed_at))
        return FileChange(file)
    created = None
    if file.created_time > lookback:
        created = TriggerEvent(f"{file.file_id}@created", file.created_time, file)
    updated = None
    if (
        file.modified_time > lookback
        and file.modified_time - file.created_time >= _MODIFIED_AFTER_CREATION_THRESHOLD
    ):
        updated = _file_trigger_event(file, "modified", file.modified_time)
    return FileChange(file, created=created, updated=updated)


FileChangeSelector = Callable[[FileChange, KeyedSnapshotEntry | None], Sequence[TriggerEvent[FileEvent]]]


def file_change_selector(trigger_name: str, configuration: FileTriggerConfiguration) -> FileChangeSelector:
    """Return the feed selector of one binding of trigger_name."""

    def select(change: FileChange, previous: KeyedSnapshotEntry | None) -> Sequence[TriggerEvent[FileEvent]]:
        event = change.event_for(trigger_name)
        if event is None or not _configuration_matches(configuration, change.file):
            return ()
        return (event,)

    return select


def _go_json_string(text: str) -> str:
    # Go's encoder with SetEscapeHTML(false): lone surrogates become U+FFFD; U+2028 and U+2029 stay escaped.
    encoded = json.dumps(_LONE_SURROGATE.sub("\ufffd", text), ensure_ascii=False)
    return encoded.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


def _go_time_text(value: datetime) -> str:
    """Format a time as Go's time.Time MarshalJSON does, keeping a provider time's offset and nanoseconds."""
    if isinstance(value, _DriveTimestamp):
        fraction = f"{value._nanoseconds:09d}".rstrip("0")
        seconds, zone = value._wire_seconds[:19], value._wire_seconds[19:]
        return seconds + (f".{fraction}" if fraction else "") + zone
    return format_rfc3339_nano(value)


@dataclass(frozen=True, slots=True)
class _FileChangeCursor:
    """The stored feed position, the Go connector's fileChangeCursor.

    checkpoint is the latest change time of the previous complete pass; round_latest accumulates the
    current pass while Drive reports more pages.
    """

    page_token: str
    checkpoint: datetime
    round_latest: datetime = _ZERO_TIME


def _encode_file_change_cursor(position: _FileChangeCursor) -> Cursor:
    token = (
        '{"pageToken":'
        + _go_json_string(position.page_token)
        + ',"checkpoint":'
        + _go_json_string(_go_time_text(position.checkpoint))
    )
    if not _is_zero_time(position.round_latest):
        token += ',"roundLatest":' + _go_json_string(_go_time_text(position.round_latest))
    return Cursor(token + "}")


def _reject_json_constant(value: str) -> Any:
    raise ValueError(f"invalid JSON constant {value}")


def _go_fields(value: object, names: tuple[str, ...], *, is_unknown_rejected: bool = False) -> dict[str, Any]:
    """Select an object's members as encoding/json matches struct fields: exact or case-folded, last wins."""
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError("JSON value is not an object")
    selected: dict[str, Any] = {}
    for key, member in value.items():
        name = next((name for name in names if key == name or key.casefold() == name.casefold()), None)
        if name is not None:
            selected[name] = member
        elif is_unknown_rejected:
            raise ValueError(f"JSON object has unknown field {key!r}")
    return selected


def _go_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("JSON value is not a string")
    return value


def _go_bool(value: object) -> bool:
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError("JSON value is not a boolean")
    return value


def _go_time(value: object) -> datetime:
    if value is None:
        return _ZERO_TIME
    if not isinstance(value, str):
        raise ValueError("JSON value is not a time")
    return _timestamp(value)


def _go_strings(value: object) -> tuple[str, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError("JSON value is not an array")
    return tuple(_go_string(item) for item in value)


def _go_int64(value: object) -> int:
    if value is None:
        return 0
    if isinstance(value, bool) or not isinstance(value, int) or not -(1 << 63) <= value < 1 << 63:
        raise ValueError("JSON value is not an int64")
    return value


def _decode_file_change_cursor(token: str) -> _FileChangeCursor:
    """Decode a stored cursor as Go's json.Decoder with DisallowUnknownFields does, or raise ValueError."""
    try:
        text = token.lstrip(" \t\r\n")
        document, _ = json.JSONDecoder(parse_constant=_reject_json_constant).raw_decode(text)
        fields = _go_fields(document, ("pageToken", "checkpoint", "roundLatest"), is_unknown_rejected=True)
        position = _FileChangeCursor(
            _go_string(fields.get("pageToken")),
            _go_time(fields.get("checkpoint")),
            _go_time(fields.get("roundLatest")),
        )
    except (ValueError, OverflowError) as error:
        raise ValueError("stored Google Drive change cursor is invalid") from error
    if not position.page_token or _is_zero_time(position.checkpoint):
        raise ValueError("stored Google Drive change cursor is invalid")
    return position


def _parse_optional_int64(value: str) -> int:
    if value == "":
        return 0
    if _GO_INT64.fullmatch(value) is None:
        raise ValueError("value is not a non-negative integer")
    parsed = int(value)
    if parsed < 0 or parsed >= 1 << 63:
        raise ValueError("value is not a non-negative integer")
    return parsed


_FILE_EVENT_FIELDS: Final = (
    "fileId", "name", "mimeType", "parents", "driveId", "createdTime", "modifiedTime", "trashedTime",
    "changeTime", "isTrashed", "sizeBytes", "md5Checksum", "webViewLink",
)  # fmt: skip


def decode_file_event_payload(payload_json: str, payload_bytes: bytes) -> FileEvent:
    """Decode a pending inbox payload that the Go or Python connector stored, as Go's json.Unmarshal does.

    Unknown members are ignored and provider times keep their offset and nanoseconds. Raises ValueError
    for a payload that is not a FileEvent object.
    """
    try:
        fields = _go_fields(
            strict_json_loads(payload_bytes if payload_bytes else payload_json), _FILE_EVENT_FIELDS
        )
        return FileEvent(
            file_id=_go_string(fields.get("fileId")),
            name=_go_string(fields.get("name")),
            mime_type=_go_string(fields.get("mimeType")),
            parents=_go_strings(fields.get("parents")),
            drive_id=_go_string(fields.get("driveId")),
            created_time=_go_time(fields.get("createdTime")),
            modified_time=_go_time(fields.get("modifiedTime")),
            trashed_time=_go_time(fields.get("trashedTime")),
            change_time=_go_time(fields.get("changeTime")),
            is_trashed=_go_bool(fields.get("isTrashed")),
            size_bytes=_go_int64(fields.get("sizeBytes")),
            md5_checksum=_go_string(fields.get("md5Checksum")),
            web_view_link=_go_string(fields.get("webViewLink")),
        )
    except (ValueError, OverflowError, UnicodeDecodeError) as error:
        raise ValueError("pending Google Drive file event is invalid") from error


@dataclass(frozen=True, slots=True)
class _DriveChangeList:
    next_page_token: str
    new_start_page_token: str
    change_times: tuple[datetime, ...]
    files: tuple[FileEvent, ...]


_CHANGE_FIELDS: Final = ("changeType", "removed", "fileId", "time", "file")
_CHANGE_FILE_RESOURCE_FIELDS: Final = (
    "id", "name", "mimeType", "parents", "driveId", "createdTime", "modifiedTime", "trashed", "trashedTime",
    "size", "md5Checksum", "webViewLink",
)  # fmt: skip


def _decode_drive_change_list(document: object) -> _DriveChangeList:
    """Decode a changes.list response as Go decodes driveChangeList, keeping only usable file changes."""
    listed = _go_fields(document, ("nextPageToken", "newStartPageToken", "changes"))
    resources = listed.get("changes")
    if resources is not None and not isinstance(resources, list):
        raise ValueError("changes is not an array")
    change_times: list[datetime] = []
    files: list[FileEvent] = []
    for resource in resources or ():
        change = _go_fields(resource, _CHANGE_FIELDS)
        change_time = _go_time(change.get("time"))
        change_times.append(change_time)
        file = (
            None if change.get("file") is None else _go_fields(change["file"], _CHANGE_FILE_RESOURCE_FIELDS)
        )
        decoded = _file_event_from_change(
            _go_string(change.get("changeType")),
            _go_bool(change.get("removed")),
            _go_string(change.get("fileId")),
            change_time,
            file,
        )
        if decoded is not None:
            files.append(decoded)
    if len(change_times) > MAXIMUM_CHANGES_PER_PAGE:
        raise _ChangePageTooLargeError(len(change_times))
    return _DriveChangeList(
        _go_string(listed.get("nextPageToken")),
        _go_string(listed.get("newStartPageToken")),
        tuple(change_times),
        tuple(files),
    )


class _ChangePageTooLargeError(Exception):
    def __init__(self, count: int) -> None:
        super().__init__(f"Google Drive returned {count} changes, more than the requested page size")


def _file_event_from_change(
    change_type: str, is_removed: bool, file_id: str, change_time: datetime, file: dict[str, Any] | None
) -> FileEvent | None:
    """Skip drive changes, removals, and entries without valid file metadata."""
    if file is None:
        return None
    # Every member decodes first, so a mistyped member fails the page as Go's json.Unmarshal does.
    identifier, name, mime_type = (_go_string(file.get(key)) for key in ("id", "name", "mimeType"))
    parents, drive_id = _go_strings(file.get("parents")), _go_string(file.get("driveId"))
    created_time, modified_time = _go_time(file.get("createdTime")), _go_time(file.get("modifiedTime"))
    is_trashed, trashed_time = _go_bool(file.get("trashed")), _go_time(file.get("trashedTime"))
    size, md5_checksum = _go_string(file.get("size")), _go_string(file.get("md5Checksum"))
    web_view_link = _go_string(file.get("webViewLink"))
    if (
        change_type != "file"
        or is_removed
        or identifier != file_id
        or not _drive_id(identifier)
        or not name
        or not mime_type
        or _is_zero_time(created_time)
        or _is_zero_time(modified_time)
    ):
        return None
    try:
        size_bytes = _parse_optional_int64(size)
    except ValueError:
        return None
    return FileEvent(
        file_id=identifier,
        name=name,
        mime_type=mime_type,
        parents=parents,
        drive_id=drive_id,
        created_time=created_time,
        modified_time=modified_time,
        trashed_time=trashed_time,
        change_time=change_time,
        is_trashed=is_trashed,
        size_bytes=size_bytes,
        md5_checksum=md5_checksum,
        web_view_link=web_view_link,
    )


class FileChangeFetcher:
    """Reads one connection's Drive change log for every file Trigger binding; a poll kit ChangeFetcher."""

    def __init__(self, client: Client, connection: ConnectionRef, trigger_name: str) -> None:
        """Bind the client and the stable credential Call of trigger_name, or of the shared feed name."""
        if client is None:
            raise ValueError("Google Drive client is required")
        self._client = client
        self._call = new_trigger_credential_call(connection, TriggerRef(CONNECTOR_ID, trigger_name))

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Store Drive's current start page token, so existing files are never reported."""
        document = await self._get_json(
            context, "/changes/startPageToken?" + query_string({"supportsAllDrives": "true"}), False
        )
        try:
            start_page_token = _go_string(_go_fields(document, ("startPageToken",)).get("startPageToken"))
        except ValueError as error:
            raise RuntimeError("Google Drive returned an invalid change response") from error
        if not start_page_token:
            raise RuntimeError("Google Drive returned no start page token")
        return _encode_file_change_cursor(
            _FileChangeCursor(start_page_token, self._client.now().astimezone(UTC))
        )

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[FileChange]:
        """Read one changes.list page after cursor and classify every usable file change."""
        try:
            position = _decode_file_change_cursor(cursor.token)
        except ValueError as error:
            raise mark_cursor_invalid(error) from error
        query = query_string(
            {
                "pageToken": position.page_token,
                "pageSize": str(self._client.poll_page_size),
                "fields": _CHANGE_FILE_FIELDS,
                "spaces": "drive",
                "includeRemoved": "false",
                "includeItemsFromAllDrives": "true",
                "supportsAllDrives": "true",
            }
        )
        document = await self._get_json(context, "/changes?" + query, True)
        try:
            listed = _decode_drive_change_list(document)
        except _ChangePageTooLargeError as error:
            raise RuntimeError(str(error)) from None
        except (ValueError, OverflowError) as error:
            raise RuntimeError("Google Drive returned an invalid change response") from error
        latest = position.round_latest
        for change_time in listed.change_times:
            if change_time > latest:
                latest = change_time
        changes = [classify_file_change(file, position.checkpoint) for file in listed.files]
        if listed.next_page_token:
            following = _FileChangeCursor(listed.next_page_token, position.checkpoint, latest)
            return ChangePage(changes, _encode_file_change_cursor(following), True)
        if listed.new_start_page_token:
            checkpoint = latest if latest > position.checkpoint else position.checkpoint
            following = _FileChangeCursor(listed.new_start_page_token, checkpoint)
            return ChangePage(changes, _encode_file_change_cursor(following), False)
        raise RuntimeError("Google Drive returned neither a next page token nor a new start page token")

    async def _get_json(
        self, context: TriggerContext, path_and_query: str, is_cursor_request: bool
    ) -> object:
        """Send one authenticated read and map failures to the poll kit's signals.

        Only a rejected stored page token is a cursor fault; a rejected seed request is retried.
        """
        await wait_for_poll_request(context)
        credentials, failure = await self._client.resolve_credentials(self._call, FILE_CHANGES_FEED_NAME)
        if credentials is None or failure is not None:
            raise RuntimeError("Google Drive Trigger credentials are unavailable")
        exchange = await self._client.exchange(
            self._call, credentials, "GET", self._client.api_base_url + path_and_query
        )
        if exchange.is_body_too_large:
            raise RuntimeError("Google Drive changes page exceeds maxResponseBytes; lower pollPageSize")
        if exchange.error:
            raise RuntimeError(f"Google Drive change request failed: {exchange.error}")
        if 200 <= exchange.status < 300:
            try:
                return strict_json_loads(exchange.body)
            except (ValueError, UnicodeDecodeError) as error:
                raise RuntimeError("Google Drive returned an invalid change response") from error
        reasons = read_error_tokens(exchange.body, _GOOGLE_ERROR_TOKEN_POINTERS)
        rejection = RuntimeError(f"Google Drive rejected the change request with HTTP {exchange.status}")
        if exchange.status == 429 or (
            exchange.status == 403 and reasons and reasons[0] in _GOOGLE_RATE_LIMIT_REASONS
        ):
            raise mark_rate_limited(rejection, timedelta(seconds=self._client.retry_after(exchange)))
        if is_cursor_request and _is_rejected_page_token(exchange.status, exchange.body):
            raise mark_cursor_invalid(rejection)
        raise rejection


def _is_rejected_page_token(status: int, body: bytes) -> bool:
    """Report a changes.list rejection of the stored page token.

    A 400 for another parameter, such as fields, would recur after every reseed, so it is retried.
    """
    if status in (404, 410):
        return True
    if status == 400:
        location = read_error_tokens(body, ("/error/errors/0/location",))
        return not location or location[0] == "pageToken"
    return False


class BindingFileEventFetcher:
    """Turns the shared change feed into one binding's event feed, for the per-Trigger factories."""

    def __init__(self, changes: FileChangeFetcher, select_events: FileChangeSelector) -> None:
        """Bind the change fetcher and the binding's selector."""
        self._changes = changes
        self._select_events = select_events

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Return Drive's current change position."""
        return await self._changes.seed_cursor(context)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[FileEvent]]:
        """Return the binding's events from one changes.list page."""
        page = await self._changes.fetch_changes(context, cursor)
        events = [event for change in page.changes for event in self._select_events(change, None)]
        return ChangePage(events, page.next_cursor, page.has_more_pages)
