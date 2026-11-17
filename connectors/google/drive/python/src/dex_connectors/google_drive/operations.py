# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Drive operation invocation and Go-compatible response classification."""

import base64
import binascii
import json
import re
import secrets
from datetime import UTC, datetime, timedelta, timezone
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
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    GET_FILE_DEFINITION,
    READ_FILE_TEXT_DEFINITION,
    SEARCH_FILES_DEFINITION,
    UPLOAD_FILE_DEFINITION,
)
from .client import (
    FULL_FILE_FIELDS,
    Client,
    Exchange,
    error_reasons,
    failure,
    query_string,
    status_failure_kind,
)
from .json_values import strict_json_loads
from .models import (
    File,
    FileSummary,
    FileUser,
    GetFileInput,
    ReadFileTextInput,
    ReadFileTextOutput,
    SearchFilesInput,
    SearchFilesOutput,
    ShortcutTarget,
    UploadFileInput,
    UploadFileOutput,
)

_ID = re.compile(r"^[A-Za-z0-9_-]{1,256}$", re.ASCII)
_MEDIA_TYPE = re.compile(r"^[a-z0-9!#$%&'*+.^_`|~-]+/[a-z0-9!#$%&'*+.^_`|~-]+$", re.ASCII)
_TIMESTAMP = re.compile(
    r"^([0-9]{4}-[0-9]{2}-[0-9]{2})T([0-9]{2}:[0-9]{2}:[0-9]{2})(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})$"
)
_TEXT_EXPORTS = {
    "application/vnd.google-apps.document": "text/plain",
    "application/vnd.google-apps.spreadsheet": "text/csv",
    "application/vnd.google-apps.presentation": "text/plain",
}
_TEXT_DOWNLOADS = {
    "application/json",
    "application/ld+json",
    "application/xml",
    "application/yaml",
    "application/x-yaml",
    "application/x-ndjson",
    "application/javascript",
    "application/sql",
}


def _drive_id(value: str) -> bool:
    return isinstance(value, str) and _ID.fullmatch(value) is not None


def _plain_media_type(value: str) -> bool:
    return isinstance(value, str) and _MEDIA_TYPE.fullmatch(value) is not None


class _DriveTimestamp(datetime):
    """Preserve Go's offset and nanoseconds in Dex's datetime JSON codec."""

    __slots__ = ("_wire_seconds", "_nanoseconds")
    _wire_seconds: str
    _nanoseconds: int

    @property
    def microsecond(self) -> int:
        """Expose up to nine digits to the RFC 3339 encoder."""
        return self._nanoseconds

    def isoformat(self, sep: str = "T", timespec: str = "auto") -> str:
        """Return the provider's canonical seconds and offset."""
        return self._wire_seconds


def _timestamp(value: object) -> datetime:
    if value is None:
        return datetime.min.replace(tzinfo=UTC)
    if not isinstance(value, str):
        raise ValueError("timestamp has an invalid type")
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise ValueError("timestamp has an invalid format")
    date, clock, fraction, zone = match.groups()
    if int(clock[:2]) > 23:
        raise ValueError("timestamp has an invalid hour")
    parsed = datetime.fromisoformat(f"{date}T{clock}")
    nanoseconds = int(((fraction or "") + "000000000")[:9])
    parsed = parsed.replace(microsecond=nanoseconds // 1000)
    canonical_zone = zone
    if zone == "Z":
        parsed = parsed.replace(tzinfo=UTC)
    else:
        hours = int(zone[1:3])
        minutes = int(zone[4:6])
        # Go's time.Time JSON decoder rejects offsets beyond 23 hours.
        if minutes > 59 or hours > 23:
            raise ValueError("timestamp has an invalid offset")
        offset = timedelta(hours=hours, minutes=minutes)
        parsed = parsed.replace(tzinfo=timezone(offset if zone[0] == "+" else -offset))
        if offset == timedelta(0):
            canonical_zone = "Z"
    timestamp = datetime.__new__(
        _DriveTimestamp,
        parsed.year,
        parsed.month,
        parsed.day,
        parsed.hour,
        parsed.minute,
        parsed.second,
        parsed.microsecond,
        tzinfo=parsed.tzinfo,
    )
    timestamp._wire_seconds = f"{date}T{clock}{canonical_zone}"
    timestamp._nanoseconds = nanoseconds
    return timestamp


def _string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("provider field is not a string")
    return value


def _bool(value: object) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("provider field is not a boolean")
    return value


def _string_list(value: object) -> tuple[str, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError("provider field is not a list")
    return tuple(_string(item) if item is not None else "" for item in value)


def _resource(value: object) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("file resource is not an object")
    return value


def _user(value: object) -> FileUser:
    data = _resource(value)
    return FileUser(_string(data.get("displayName")), _string(data.get("emailAddress")))


def _nonnegative_number(value: object) -> int:
    raw = _string(value)
    if raw == "":
        return 0
    if not re.fullmatch(r"[+]?[0-9]+", raw, re.ASCII):
        raise ValueError("file number is invalid")
    number = int(raw)
    if number > 2**63 - 1:
        raise ValueError("file number is too large")
    return number


def _file(value: object) -> File:
    resource = _resource(value)
    identifier = _string(resource.get("id"))
    name = _string(resource.get("name"))
    mime_type = _string(resource.get("mimeType"))
    if not _drive_id(identifier) or not name or not mime_type:
        raise ValueError("file resource lacks a valid ID, name, or MIME type")
    owners = resource.get("owners")
    if owners is not None and not isinstance(owners, list):
        raise ValueError("owners must be an array")
    last_user = resource.get("lastModifyingUser")
    shortcut = resource.get("shortcutDetails")
    if shortcut is not None:
        shortcut = _resource(shortcut)
    return File(
        id=identifier,
        name=name,
        mime_type=mime_type,
        description=_string(resource.get("description")),
        parents=_string_list(resource.get("parents")),
        drive_id=_string(resource.get("driveId")),
        created_time=_timestamp(resource.get("createdTime")),
        modified_time=_timestamp(resource.get("modifiedTime")),
        size_bytes=_nonnegative_number(resource.get("size")),
        md5_checksum=_string(resource.get("md5Checksum")),
        version=_nonnegative_number(resource.get("version")),
        web_view_link=_string(resource.get("webViewLink")),
        is_trashed=_bool(resource.get("trashed")),
        owners=tuple(_user(owner) for owner in owners or ()),
        last_modifying_user=_user(last_user) if last_user is not None else None,
        shortcut=ShortcutTarget(_string(shortcut.get("targetId")), _string(shortcut.get("targetMimeType")))
        if shortcut is not None
        else None,
    )


def _json_object(exchange: Exchange) -> dict[str, Any]:
    value = strict_json_loads(exchange.body)
    return _resource(value)


# Only these statuses prove Drive refused an upload; every other status is an unknown outcome.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


def _has_file_id(exchange: Exchange) -> bool:
    """Report a response body that names a created file, which proves Drive accepted the upload."""
    try:
        value = strict_json_loads(exchange.body)
    except (ValueError, TypeError, OverflowError):
        return False
    return isinstance(value, dict) and isinstance(value.get("id"), str) and value["id"] != ""


def _quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "\\'") + "'"


def _read_outcome(
    client: Client,
    operation: str,
    exchange: Exchange,
    *,
    not_found: str = "notFound",
    too_large: str = "invalidResponse",
) -> tuple[str, Failure | None, float]:
    if exchange.error == "notBuilt":
        return (
            "defect",
            failure(operation, FailureKind.LOCAL_DEFECT, "provider request could not be built"),
            0,
        )
    if exchange.error:
        return "retry", failure(operation, FailureKind.TRANSPORT, "provider is unavailable"), 0
    if exchange.is_body_too_large:
        return (
            too_large,
            failure(
                operation, FailureKind.RESPONSE_TOO_LARGE, "provider response exceeds the configured limit"
            ),
            0,
        )
    if 200 <= exchange.status < 300:
        return "success", None, 0
    reasons = error_reasons(exchange)
    kind = status_failure_kind(exchange.status, reasons)
    if exchange.status >= 500 or kind == FailureKind.RATE_LIMIT:
        return (
            "retry",
            failure(operation, kind, "provider temporarily rejected the request"),
            client.retry_after(exchange),
        )
    if exchange.status == 404:
        return (
            not_found,
            failure(
                operation, FailureKind.NOT_FOUND, "file was not found or is not visible to the connection"
            ),
            0,
        )
    if reasons and reasons[0] == "exportSizeLimitExceeded":
        return (
            too_large,
            failure(operation, FailureKind.RESPONSE_TOO_LARGE, "Google export size limit was exceeded"),
            0,
        )
    return "providerRejected", failure(operation, kind, "provider rejected the request"), 0


def _query_failure(
    client: Client,
    call: Call,
    empty: Any,
    operation: str,
    exchange: Exchange,
    route: str,
    problem: Failure,
    delay: float,
) -> QueryAttempt[Any]:
    if route == "retry":
        return QueryAttempt.retry(problem, delay)
    return QueryAttempt.branch_result(
        route, empty, client.receipt(call, exchange) if exchange.was_sent else Receipt(), problem
    )


class SearchFilesOperation:
    """Search one bounded page of Drive files."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the search Query definition."""
        return SEARCH_FILES_DEFINITION

    async def invoke(self, call: Call, input: SearchFilesInput) -> QueryAttempt[SearchFilesOutput]:
        """Validate filters, request one page, and classify its contents."""
        empty = SearchFilesOutput()
        page_size = input.page_size or self.client.search_page_size
        if (
            input.name
            and not input.name.strip()
            or input.name_match not in (None, "", "exact", "contains")
            or (input.name_match and not input.name)
            or input.mime_type
            and not _plain_media_type(input.mime_type)
            or (input.parent_folder_id and not _drive_id(input.parent_folder_id))
            or len(input.page_token.encode("utf-8")) > 4096
            or any(char in input.page_token for char in " \r\n\t")
            or not 1 <= page_size <= 100
        ):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("searchFiles", FailureKind.VALIDATION, "search input is invalid"),
            )
        credentials, problem = await self.client.resolve_credentials(call, "searchFiles")
        if problem:
            return QueryAttempt.branch_result("defect", empty, Receipt(), problem)
        assert credentials is not None
        terms = []
        if input.name:
            terms.append(f"name {'contains' if input.name_match == 'contains' else '='} {_quote(input.name)}")
        if input.mime_type:
            terms.append("mimeType = " + _quote(input.mime_type))
        if input.parent_folder_id:
            terms.append(_quote(input.parent_folder_id) + " in parents")
        terms.append("trashed = false")
        query = {
            "q": " and ".join(terms),
            "pageSize": str(page_size),
            "orderBy": "modifiedTime desc,name",
            "fields": (
                "nextPageToken,incompleteSearch,files(id,name,mimeType,parents,modifiedTime,webViewLink)"
            ),
            "spaces": "drive",
        }
        if input.page_token:
            query["pageToken"] = input.page_token
        exchange = await self.client.exchange(call, credentials, "GET", self.client.files_list_url(query))
        route, problem, delay = _read_outcome(
            self.client, "searchFiles", exchange, not_found="providerRejected"
        )
        if problem:
            return _query_failure(self.client, call, empty, "searchFiles", exchange, route, problem, delay)
        receipt = self.client.receipt(call, exchange)
        try:
            payload = _json_object(exchange)
            rows = payload.get("files")
            if rows is not None and not isinstance(rows, list):
                raise ValueError("files must be an array")
            if len(rows or ()) > page_size:
                raise ValueError("too many files")
            files = []
            for row in rows or ():
                resource = _resource(row)
                identifier = _string(resource.get("id"))
                name = _string(resource.get("name"))
                mime_type = _string(resource.get("mimeType"))
                if not _drive_id(identifier) or not name or not mime_type:
                    raise ValueError("invalid file summary")
                files.append(
                    FileSummary(
                        identifier,
                        name,
                        mime_type,
                        _string_list(resource.get("parents")),
                        _timestamp(resource.get("modifiedTime")),
                        _string(resource.get("webViewLink")),
                    )
                )
            output = SearchFilesOutput(
                tuple(files), _string(payload.get("nextPageToken")), _bool(payload.get("incompleteSearch"))
            )
        except (ValueError, TypeError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("searchFiles", FailureKind.PROTOCOL, "provider returned an invalid search response"),
            )
        if not output.files and not output.next_page_token:
            return QueryAttempt.branch_result(
                "notFound",
                output,
                receipt,
                failure("searchFiles", FailureKind.NOT_FOUND, "no file matched the search"),
            )
        return QueryAttempt.branch_result("found", output, receipt)


class GetFileOperation:
    """Read one Drive file's metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the metadata Query definition."""
        return GET_FILE_DEFINITION

    async def invoke(self, call: Call, input: GetFileInput) -> QueryAttempt[File]:
        """Read and validate full metadata for one Drive ID."""
        empty = File()
        if not _drive_id(input.file_id):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("getFile", FailureKind.VALIDATION, "fileId must be a Drive file ID"),
            )
        credentials, problem = await self.client.resolve_credentials(call, "getFile")
        if problem:
            return QueryAttempt.branch_result("defect", empty, Receipt(), problem)
        assert credentials is not None
        exchange = await self.client.exchange(
            call, credentials, "GET", self.client.file_metadata_url(input.file_id, FULL_FILE_FIELDS)
        )
        route, problem, delay = _read_outcome(self.client, "getFile", exchange)
        if problem:
            return _query_failure(self.client, call, empty, "getFile", exchange, route, problem, delay)
        receipt = self.client.receipt(call, exchange, input.file_id)
        try:
            file = _file(_json_object(exchange))
            if file.id != input.file_id and input.file_id != "root":
                raise ValueError("returned a different file")
        except (ValueError, TypeError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("getFile", FailureKind.PROTOCOL, "provider returned an invalid file response"),
            )
        return QueryAttempt.branch_result("found", file, receipt)


class ReadFileTextOperation:
    """Export or download one bounded UTF-8 Drive file."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the text Query definition."""
        return READ_FILE_TEXT_DEFINITION

    async def invoke(self, call: Call, input: ReadFileTextInput) -> QueryAttempt[ReadFileTextOutput]:
        """Read metadata before deciding whether and how to fetch content."""
        empty = ReadFileTextOutput()
        if not _drive_id(input.file_id):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("readFileText", FailureKind.VALIDATION, "fileId must be a Drive file ID"),
            )
        credentials, problem = await self.client.resolve_credentials(call, "readFileText")
        if problem:
            return QueryAttempt.branch_result("defect", empty, Receipt(), problem)
        assert credentials is not None
        metadata = await self.client.exchange(
            call, credentials, "GET", self.client.file_metadata_url(input.file_id, "id,name,mimeType,size")
        )
        route, problem, delay = _read_outcome(self.client, "readFileText", metadata)
        if problem:
            return _query_failure(self.client, call, empty, "readFileText", metadata, route, problem, delay)
        metadata_receipt = self.client.receipt(call, metadata, input.file_id)
        try:
            file = _file(_json_object(metadata))
        except (ValueError, TypeError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                metadata_receipt,
                failure("readFileText", FailureKind.PROTOCOL, "provider returned an invalid file response"),
            )
        output = ReadFileTextOutput(file.id, file.name, file.mime_type)
        if file.mime_type in _TEXT_EXPORTS:
            text_mime_type = _TEXT_EXPORTS[file.mime_type]
            target = self.client.file_export_url(file.id, text_mime_type)
        elif file.mime_type.startswith("text/") or file.mime_type in _TEXT_DOWNLOADS:
            if file.size_bytes > self.client.max_text_bytes:
                return QueryAttempt.branch_result(
                    "tooLarge",
                    output,
                    metadata_receipt,
                    failure(
                        "readFileText",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "file size exceeds the configured text limit",
                    ),
                )
            text_mime_type = file.mime_type
            target = self.client.file_content_url(file.id)
        else:
            if file.mime_type == "application/vnd.google-apps.folder":
                message = "file is a folder"
            elif file.mime_type == "application/vnd.google-apps.shortcut":
                message = "file is a shortcut; read its target file ID from getFile"
            elif file.mime_type.startswith("application/vnd.google-apps."):
                message = "Google Workspace file type has no text export"
            else:
                message = "file type is not text"
            return QueryAttempt.branch_result(
                "unsupportedContent",
                output,
                metadata_receipt,
                failure("readFileText", FailureKind.VALIDATION, message),
            )
        content = await self.client.exchange(
            call, credentials, "GET", target, limit=self.client.max_text_bytes
        )
        route, problem, delay = _read_outcome(
            self.client,
            "readFileText",
            content,
            too_large="tooLarge",
        )
        if problem:
            if route == "retry":
                return QueryAttempt.retry(problem, delay)
            return QueryAttempt.branch_result(
                route, output, self.client.receipt(call, content) if content.was_sent else Receipt(), problem
            )
        receipt = self.client.receipt(call, content, file.id)
        text_bytes = content.body[3:] if content.body.startswith(b"\xef\xbb\xbf") else content.body
        try:
            text = text_bytes.decode("utf-8")
            if "\x00" in text:
                raise UnicodeError("embedded NUL")
        except UnicodeError:
            return QueryAttempt.branch_result(
                "unsupportedContent",
                output,
                receipt,
                failure("readFileText", FailureKind.VALIDATION, "file content is not valid UTF-8 text"),
            )
        return QueryAttempt.branch_result(
            "read",
            ReadFileTextOutput(file.id, file.name, file.mime_type, text_mime_type, text, len(text_bytes)),
            receipt,
        )


def _upload_content(input: UploadFileInput, max_bytes: int) -> bytes:
    if not input.name.strip():
        raise ValueError("name is required")
    if input.parent_folder_id and not _drive_id(input.parent_folder_id):
        raise ValueError("parentFolderId must be a Drive folder ID or root")
    if not _plain_media_type(input.mime_type):
        raise ValueError("mimeType must be one lowercase type/subtype without parameters")
    if input.mime_type.startswith("application/vnd.google-apps."):
        raise ValueError("mimeType cannot be a Google Workspace type")
    try:
        content = base64.b64decode(input.byte_content, validate=True) if input.byte_content else b""
    except (ValueError, binascii.Error):
        raise ValueError("byteContent must be base64") from None
    if input.text_content and content:
        raise ValueError("set textContent or byteContent, not both")
    if input.text_content:
        content = input.text_content.encode("utf-8")
    if len(content) > max_bytes:
        raise ValueError(f"content exceeds the configured upload limit of {max_bytes} bytes")
    return content


def _earlier_upload_query(key: str) -> str:
    return "appProperties has { key=" + _quote("dexIdempotencyKey") + " and value=" + _quote(key) + " }"


def _earlier_file(exchange: Exchange) -> tuple[File | None, bool]:
    payload = _json_object(exchange)
    rows = payload.get("files")
    if rows is not None and not isinstance(rows, list):
        raise ValueError("files must be an array")
    files = tuple(_file(row) for row in rows or ())
    return (
        min(files, key=lambda file: file.created_time) if files else None,
        _bool(payload.get("incompleteSearch")),
    )


def _multipart_body(input: UploadFileInput, content: bytes, key: str) -> tuple[bytes, str]:
    boundary = secrets.token_hex(24)
    metadata: dict[str, Any] = {
        "name": input.name,
        "mimeType": input.mime_type,
        "appProperties": {"dexIdempotencyKey": key},
    }
    if input.parent_folder_id:
        metadata["parents"] = [input.parent_folder_id]
    encoded = json.dumps(metadata, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    separator = ("--" + boundary + "\r\n").encode()
    body = (
        separator
        + b"Content-Type: application/json; charset=UTF-8\r\n\r\n"
        + encoded
        + b"\r\n"
        + separator
        + ("Content-Type: " + input.mime_type + "\r\n\r\n").encode()
        + content
        + b"\r\n"
        + ("--" + boundary + "--\r\n").encode()
    )
    return body, "multipart/related; boundary=" + boundary


class UploadFileOperation:
    """Create one Drive file behind a durable single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the upload Mutation definition."""
        return UPLOAD_FILE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UploadFileInput) -> IdempotencyKey:
        """Derive the stable Drive app property from the Call ID."""
        return IdempotencyKey(call_id)

    async def _lookup(
        self,
        call: Call,
        credentials: Any,
        key: str,
    ) -> tuple[Exchange, str, Failure | None, float, File | None, bool]:
        fields = "incompleteSearch,files(" + FULL_FILE_FIELDS + ")"
        exchange = await self.client.exchange(
            call,
            credentials,
            "GET",
            self.client.files_list_url(
                {
                    "q": _earlier_upload_query(key),
                    "fields": fields,
                    "pageSize": "10",
                    "spaces": "drive",
                }
            ),
        )
        route, problem, delay = _read_outcome(
            self.client,
            "uploadFile",
            exchange,
            not_found="providerRejected",
        )
        if problem:
            return exchange, route, problem, delay, None, False
        try:
            file, incomplete = _earlier_file(exchange)
        except (ValueError, TypeError, OverflowError):
            return (
                exchange,
                "invalidResponse",
                failure(
                    "uploadFile",
                    FailureKind.PROTOCOL,
                    "provider returned an invalid duplicate-lookup response",
                ),
                0,
                None,
                False,
            )
        return exchange, "success", None, 0, file, incomplete

    async def _read_back_id(
        self,
        call: Call,
        credentials: Any,
        file_id: str,
        key: str,
    ) -> tuple[Exchange, str, Failure | None, float, File | None]:
        exchange = await self.client.exchange(
            call,
            credentials,
            "GET",
            self.client.file_metadata_url(file_id, FULL_FILE_FIELDS + ",appProperties"),
        )
        route, problem, delay = _read_outcome(
            self.client,
            "uploadFile",
            exchange,
            not_found="providerRejected",
        )
        if problem:
            return exchange, route, problem, delay, None
        try:
            payload = _json_object(exchange)
            properties = payload.get("appProperties")
            if properties is not None and (
                not isinstance(properties, dict)
                or not all(isinstance(k, str) and isinstance(v, str) for k, v in properties.items())
            ):
                raise ValueError("appProperties invalid")
            if _string(payload.get("id")) != file_id or (properties or {}).get("dexIdempotencyKey") != key:
                return exchange, "success", None, 0, None
            file = _file(payload)
        except (ValueError, TypeError, OverflowError):
            return (
                exchange,
                "invalidResponse",
                failure(
                    "uploadFile",
                    FailureKind.PROTOCOL,
                    "provider returned an invalid upload ID read-back response",
                ),
                0,
                None,
            )
        return exchange, "success", None, 0, file

    async def _retry_before_send(
        self,
        fence: Fence,
        problem: Failure,
        delay: float,
    ) -> MutationAttempt[UploadFileOutput]:
        try:
            await fence.record_not_sent()
        except Exception:
            problem = failure(
                "uploadFile", FailureKind.AVAILABILITY, "upload was not sent, but its checkpoint failed"
            )
        return MutationAttempt.retry(problem, delay)

    async def _retry_after_rejection(
        self,
        fence: Fence,
        kind: FailureKind,
        message: str,
        delay: float,
    ) -> MutationAttempt[UploadFileOutput]:
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure(
                    "uploadFile",
                    FailureKind.AVAILABILITY,
                    "provider returned a retryable status, but its retry checkpoint failed",
                ),
                delay,
            )
        return MutationAttempt.retry(failure("uploadFile", kind, message), delay)

    def _uncertain(self, call: Call, message: str) -> MutationAttempt[UploadFileOutput]:
        return MutationAttempt.uncertain(
            UploadFileOutput(),
            failure("uploadFile", FailureKind.AVAILABILITY, message),
            self.client.receipt(call),
        )

    async def _reconcile(
        self,
        call: Call,
        fence: Fence,
        credentials: Any,
        key: str,
        request_id: str,
        can_resend_unknown_outcome: bool,
    ) -> MutationAttempt[UploadFileOutput]:
        """Read back an upload with an unknown outcome, then resend while the cap allows."""
        exchange, route, problem, delay, file, incomplete = await self._lookup(call, credentials, key)
        if problem:
            if route == "retry":
                return MutationAttempt.retry(problem, delay)
            return self._resend_or_select_uncertain(
                call, can_resend_unknown_outcome, "upload read-back could not confirm the outcome", request_id
            )
        if file:
            try:
                await fence.record_provider_id(file.id)
            except Exception:
                pass  # A later attempt still finds the file by its app property.
            return MutationAttempt.branch_result(
                "uploaded",
                UploadFileOutput(file),
                self.client.receipt(call, exchange, file.id),
            )
        if incomplete:
            return MutationAttempt.retry(
                failure("uploadFile", FailureKind.AVAILABILITY, "provider did not complete upload read-back")
            )
        return self._resend_or_select_uncertain(
            call,
            can_resend_unknown_outcome,
            "upload was sent but read-back found no matching file",
            request_id,
        )

    def _resend_or_select_uncertain(
        self, call: Call, can_resend_unknown_outcome: bool, message: str, request_id: str
    ) -> MutationAttempt[UploadFileOutput]:
        """Retry a possibly accepted upload while the send cap allows another send."""
        if can_resend_unknown_outcome:
            # The claim stays the last marker, so the next attempt reads back again and counts this send.
            return MutationAttempt.retry(failure("uploadFile", FailureKind.AVAILABILITY, message))
        return MutationAttempt.uncertain(
            UploadFileOutput(),
            failure("uploadFile", FailureKind.AVAILABILITY, message + "; the send cap is reached"),
            Receipt(
                call.id,
                "google-drive",
                self.client._now().astimezone(UTC),
                call.idempotency_key,
                "",
                request_id,
            ),
        )

    async def _reject_or_select_uncertain(
        self,
        call: Call,
        fence: Fence,
        has_possible_earlier_send: bool,
        kind: FailureKind,
        message: str,
        receipt: Receipt,
    ) -> MutationAttempt[UploadFileOutput]:
        """Select providerRejected only when no earlier send may have uploaded."""
        if has_possible_earlier_send:
            return self._uncertain(call, message + " after an earlier unconfirmed upload")
        try:
            await fence.record_can_resend()
        except Exception:
            pass  # The refusal ends the Step, so a lost checkpoint cannot cause a resend.
        return MutationAttempt.branch_result(
            "providerRejected", UploadFileOutput(), receipt, failure("uploadFile", kind, message)
        )

    async def invoke(self, call: Call, input: UploadFileInput) -> MutationAttempt[UploadFileOutput]:
        """Read back an earlier upload, resend an unknown outcome below the cap, and preserve provider IDs.

        A definite refusal is never resent, and uncertain is selected only at
        the send cap or when an earlier send may have uploaded the file.
        """
        empty = UploadFileOutput()
        try:
            content = _upload_content(input, self.client.max_upload_bytes)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure("uploadFile", FailureKind.VALIDATION, str(error))
            )
        key = str(call.idempotency_key)
        if not key or len(key.encode()) > 100:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("uploadFile", FailureKind.LOCAL_DEFECT, "idempotency key is missing or too long"),
            )
        try:
            fence = Fence(call, timedelta(seconds=30), max_sends=call.max_sends)
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("uploadFile", FailureKind.LOCAL_DEFECT, "single-dispatch fence could not be created"),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    "uploadFile",
                    FailureKind.AVAILABILITY,
                    "upload checkpoint is unreadable; nothing was uploaded in this attempt",
                )
            )
        # This attempt's send is the last one the cap allows when no further send remains.
        can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        credentials, problem = await self.client.resolve_credentials(call, "uploadFile")
        if problem:
            if decision.kind == DecisionKind.SEND_ALLOWED:
                return await self._retry_before_send(fence, problem, 0)
            return MutationAttempt.retry(problem)
        assert credentials is not None
        marker_id = decision.marker.provider_id if decision.marker else ""
        # Drive returns a file ID only for an accepted upload, so a saved one is read back and never resent.
        if decision.kind != DecisionKind.SEND_ALLOWED and marker_id:
            exchange, route, problem, delay, file = await self._read_back_id(
                call, credentials, marker_id, key
            )
            if problem:
                if route == "retry":
                    return MutationAttempt.retry(problem, delay)
                return self._uncertain(call, "upload ID read-back could not confirm the outcome")
            if file is None:
                return self._uncertain(call, "upload ID read-back found no matching file")
            return MutationAttempt.branch_result(
                "uploaded",
                UploadFileOutput(file, True),
                self.client.receipt(call, exchange, file.id),
            )
        lookup, route, problem, delay, earlier, incomplete = await self._lookup(call, credentials, key)
        if problem:
            if decision.kind == DecisionKind.SEND_ALLOWED and route == "retry":
                return await self._retry_before_send(fence, problem, delay)
            if route == "retry":
                return MutationAttempt.retry(problem, delay)
            if decision.kind == DecisionKind.READ_BACK_REQUIRED:
                return self._uncertain(call, "upload read-back could not confirm the outcome")
            if not has_possible_earlier_send:
                return MutationAttempt.branch_result(
                    route, empty, self.client.receipt(call, lookup) if lookup.was_sent else Receipt(), problem
                )
            # An inconclusive read-back after a possible send permits this attempt's resend.
        if earlier:
            return MutationAttempt.branch_result(
                "uploaded",
                UploadFileOutput(earlier, True),
                self.client.receipt(call, lookup, earlier.id),
            )
        if incomplete:
            problem = failure(
                "uploadFile", FailureKind.AVAILABILITY, "provider did not complete the duplicate lookup"
            )
            if decision.kind == DecisionKind.SEND_ALLOWED:
                return await self._retry_before_send(fence, problem, 0)
            return MutationAttempt.retry(problem)
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return self._uncertain(call, "prior upload cannot be confirmed by read-back")
        body, content_type = _multipart_body(input, content, key)
        try:
            response = await fence.dispatch(
                lambda _deadline: self.client.exchange(
                    call,
                    credentials,
                    "POST",
                    self.client.upload_base_url
                    + "/files?"
                    + query_string(
                        {
                            "uploadType": "multipart",
                            "fields": FULL_FILE_FIELDS,
                            "supportsAllDrives": "true",
                        }
                    ),
                    body=body,
                    content_type=content_type,
                    retry_unauthorized=False,
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    "uploadFile", FailureKind.AVAILABILITY, "upload was not sent before the attempt deadline"
                )
            )
        except (DispatchNotAllowedError, TimeoutError):
            return await self._reconcile(call, fence, credentials, key, "", can_resend_unknown_outcome)
        if response.error == "beforeSend":
            return await self._retry_after_rejection(
                fence,
                FailureKind.TRANSPORT,
                "upload failed before a connection was obtained",
                0,
            )
        if response.status in (429, 503):
            return await self._retry_after_rejection(
                fence,
                status_failure_kind(response.status, []),
                "provider returned a retryable upload status",
                self.client.retry_after(response),
            )
        if response.error or (200 <= response.status < 300 and response.is_body_too_large):
            return await self._reconcile(
                call, fence, credentials, key, response.request_id, can_resend_unknown_outcome
            )
        receipt = self.client.receipt(call, response)
        reasons = error_reasons(response)
        has_accepted_file = _has_file_id(response)
        if response.status == 401 and not has_accepted_file:
            refresh = getattr(self.client._credentials, "resolve_after_rejection", None)
            if refresh is not None:
                try:
                    await refresh(call)
                    return await self._retry_after_rejection(
                        fence,
                        FailureKind.AUTHENTICATION,
                        "provider rejected the upload credential",
                        0,
                    )
                except Exception:
                    pass
            return await self._reject_or_select_uncertain(
                call,
                fence,
                has_possible_earlier_send,
                FailureKind.AUTHENTICATION,
                "provider rejected the upload credential",
                receipt,
            )
        kind = status_failure_kind(response.status, reasons)
        if kind == FailureKind.RATE_LIMIT and not has_accepted_file:
            return await self._retry_after_rejection(
                fence, kind, "provider returned a retryable upload status", self.client.retry_after(response)
            )
        if 200 <= response.status < 300:
            try:
                if not has_accepted_file:
                    raise ValueError("upload response has no file ID")
                file = _file(_json_object(response))
            except (ValueError, TypeError, OverflowError):
                return await self._reconcile(
                    call, fence, credentials, key, response.request_id, can_resend_unknown_outcome
                )
            try:
                await fence.record_provider_id(file.id)
            except Exception:
                pass  # The accepted upload result remains known even when its extra checkpoint is lost.
            return MutationAttempt.branch_result(
                "uploaded",
                UploadFileOutput(file),
                self.client.receipt(call, response, file.id),
            )
        if response.status in _DEFINITE_REFUSAL_STATUSES and not has_accepted_file:
            return await self._reject_or_select_uncertain(
                call, fence, has_possible_earlier_send, kind, "provider rejected the upload", receipt
            )
        return await self._reconcile(
            call, fence, credentials, key, response.request_id, can_resend_unknown_outcome
        )
