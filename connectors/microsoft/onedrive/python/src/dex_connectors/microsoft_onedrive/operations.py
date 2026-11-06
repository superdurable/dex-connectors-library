# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OneDrive operation validation, Graph decisions, and retry-safe writes."""

import base64
import binascii
import json
import re
from typing import Any, TypeVar
from urllib.parse import urlsplit

import httpx
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
from dex_connectors.sdk.providerhttp import parse_retry_after, read_error_tokens

from ._generated import definitions as defs
from .client import (
    ERROR_POINTERS,
    ITEM_FIELDS,
    SEARCH_FIELDS,
    TEXT_FIELDS,
    Client,
    Exchange,
    decode_item,
    decode_json,
    graph_failure,
    odata_query,
    path_escape,
    receipt,
    retryable_status,
    status_failure_kind,
    validate_ids,
    validate_name,
)
from .models import (
    CreateFolderInput,
    CreateFolderOutput,
    DriveItem,
    FileSummary,
    GetFileInput,
    ReadFileTextInput,
    ReadFileTextOutput,
    SearchFilesInput,
    SearchFilesOutput,
    UploadFileInput,
    UploadFileOutput,
)

T = TypeVar("T")
TEXT_MIME_TYPES = frozenset(
    (
        "application/json",
        "application/ld+json",
        "application/xml",
        "application/yaml",
        "application/x-yaml",
        "application/x-ndjson",
        "application/javascript",
        "application/sql",
        "application/x-sh",
        "application/toml",
    )
)
TEXT_EXTENSIONS = frozenset(
    (
        ".txt",
        ".md",
        ".markdown",
        ".csv",
        ".tsv",
        ".json",
        ".ndjson",
        ".xml",
        ".yaml",
        ".yml",
        ".toml",
        ".ini",
        ".cfg",
        ".conf",
        ".log",
        ".sql",
        ".html",
        ".htm",
        ".css",
        ".js",
        ".ts",
        ".py",
        ".go",
        ".sh",
    )
)


def _go_equal_fold(left: str, right: str) -> bool:
    return len(left) == len(right) and left.casefold() == right.casefold()


def _go_path_extension(path: str) -> str:
    name = path.rsplit("/", 1)[-1]
    dot = name.rfind(".")
    return name[dot:] if dot >= 0 else ""


def _query_failure(
    client: Client, call: Call, operation: str, exchange: Exchange, zero: T, *, not_found: str
) -> QueryAttempt[T] | None:
    result = client.failed_read(
        call,
        operation,
        exchange,
        not_found_branch=not_found,
        provider_rejected_branch=defs.PROVIDER_REJECTED,
        invalid_branch=defs.INVALID_RESPONSE,
        defect_branch=defs.DEFECT,
    )
    if result is None:
        return None
    branch, failure, delay, received = result
    return (
        QueryAttempt.branch_result(branch, zero, received, failure)
        if branch
        else QueryAttempt.retry(failure, delay)
    )


def _mutation_read_failure(
    client: Client, call: Call, operation: str, exchange: Exchange, zero: T
) -> MutationAttempt[T] | None:
    result = client.failed_read(
        call,
        operation,
        exchange,
        not_found_branch="",
        provider_rejected_branch=defs.PROVIDER_REJECTED,
        invalid_branch=defs.INVALID_RESPONSE,
        defect_branch=defs.DEFECT,
    )
    if result is None:
        return None
    branch, failure, delay, received = result
    return (
        MutationAttempt.branch_result(branch, zero, received, failure)
        if branch
        else MutationAttempt.retry(failure, delay)
    )


def _response_item(exchange: Exchange) -> DriveItem:
    return decode_item(decode_json(exchange.body))


def _plain_media_type(value: str) -> bool:
    token = r"[a-z0-9!#$%&'*+.^_`|~-]+"
    return bool(re.fullmatch(token + "/" + token, value))


def _summary(item: DriveItem) -> FileSummary:
    return FileSummary(
        item.id,
        item.name,
        item.drive_id,
        item.parent_folder_id,
        item.is_folder,
        item.is_package,
        item.mime_type,
        item.size_bytes,
        item.last_modified_date_time,
        item.web_url,
    )


class SearchFilesOperation:
    """Search drive-wide indexed items or one folder's immediate children."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable searchFiles identity and branches."""
        return defs.SEARCH_FILES_DEFINITION

    async def invoke(self, call: Call, input: SearchFilesInput) -> QueryAttempt[SearchFilesOutput]:
        """Run a bounded Graph search page with Go-compatible filtering."""
        operation = "searchFiles"
        zero = SearchFilesOutput()
        try:
            validate_ids(input.drive_id, input.parent_folder_id)
            if input.name_match not in (None, "", "exact", "contains"):
                raise ValueError("nameMatch must be exact or contains")
            if input.name_match and not input.name:
                raise ValueError("nameMatch requires a name")
            if input.name:
                if not input.name.strip():
                    raise ValueError("name cannot be only whitespace")
                if len(input.name.encode("utf-8")) > 255:
                    raise ValueError("name must be valid UTF-8 of at most 255 bytes")
                if any(
                    character in '/\\*<>?:|#%"' or ord(character) < 32 or ord(character) == 127
                    for character in input.name
                ):
                    raise ValueError('name cannot contain / \\ * < > ? : | # % or "')
            if input.mime_type and not _plain_media_type(input.mime_type):
                raise ValueError("mimeType must be one lowercase type/subtype without parameters")
            if input.page_token and (
                len(input.page_token.encode("utf-8")) > 8192
                or any(character in " \r\n\t" for character in input.page_token)
                or not self.client.valid_next_link(input.page_token)
            ):
                raise ValueError(
                    "pageToken must be a Microsoft Graph next-page link from an earlier searchFiles result"
                )
            page_size = input.page_size or self.client.search_page_size
            if not 1 <= page_size <= 200:
                raise ValueError("pageSize must be from 1 to 200")
            if not input.parent_folder_id and not input.name:
                raise ValueError(
                    "name is required when parentFolderId is blank, because drive search needs search text"
                )
        except ValueError as error:
            return QueryAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), graph_failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, failure = await self.client.resolve_credentials(call, operation, input.drive_id)
        if failure is not None or credentials is None:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), failure)
        is_path = bool(
            input.parent_folder_id and input.name and input.name_match != "contains" and not input.page_token
        )
        is_indexed = not input.parent_folder_id
        if is_path:
            target = (
                self.client.child_url(input.drive_id, input.parent_folder_id, input.name)
                + "?"
                + odata_query(("$select", SEARCH_FIELDS))
            )
            exchange = await self.client.send(call, credentials, "GET", target)
            failed = _query_failure(self.client, call, operation, exchange, zero, not_found=defs.NOT_FOUND)
            if failed is not None:
                if failed.kind == "branch" and failed.branch == defs.NOT_FOUND:
                    return QueryAttempt.branch_result(
                        defs.NOT_FOUND,
                        zero,
                        failed.receipt or Receipt(),
                        graph_failure(FailureKind.NOT_FOUND, operation, "no item matched the search"),
                    )
                return failed
            received = receipt(call, exchange.request_id, now=self.client.now())
            try:
                item = _response_item(exchange)
                if not _go_equal_fold(item.name, input.name):
                    raise ValueError("name mismatch")
            except (ValueError, TypeError, UnicodeError):
                return QueryAttempt.branch_result(
                    defs.INVALID_RESPONSE,
                    zero,
                    received,
                    graph_failure(
                        FailureKind.PROTOCOL, operation, "provider returned an invalid item response"
                    ),
                )
            received = receipt(call, exchange.request_id, item.id, self.client.now())
            files: tuple[FileSummary, ...] = (_summary(item),) if _matches_mime(item, input.mime_type) else ()
            output = SearchFilesOutput(files)
            if not files:
                return QueryAttempt.branch_result(
                    defs.NOT_FOUND,
                    output,
                    received,
                    graph_failure(FailureKind.NOT_FOUND, operation, "no item matched the search"),
                )
            return QueryAttempt.branch_result(defs.FOUND, output, received)
        if input.page_token:
            target = input.page_token
        else:
            query = "?" + odata_query(("$select", SEARCH_FIELDS), ("$top", str(page_size)))
            if is_indexed:
                search_text = path_escape(input.name.replace("'", "''"))
                target = (
                    self.client.drive_url(input.drive_id) + "/root/search(q='" + search_text + "')" + query
                )
            else:
                target = self.client.item_url(input.drive_id, input.parent_folder_id) + "/children" + query
        exchange = await self.client.send(call, credentials, "GET", target)
        failed = _query_failure(
            self.client, call, operation, exchange, zero, not_found=defs.PROVIDER_REJECTED
        )
        if failed is not None:
            return failed
        received = receipt(call, exchange.request_id, now=self.client.now())
        try:
            page = decode_json(exchange.body)
            if not isinstance(page, dict) or not isinstance(page.get("value"), list):
                raise ValueError("invalid collection")
            resources = page["value"]
            if len(resources) > 200:
                raise ValueError("too many items")
            next_link = page.get("@odata.nextLink", "")
            if not isinstance(next_link, str) or (
                next_link
                and (len(next_link.encode("utf-8")) > 8192 or not self.client.valid_next_link(next_link))
            ):
                raise ValueError("invalid next link")
            files = tuple(
                _summary(item)
                for resource in resources
                if _matches_item(item := decode_item(resource), input)
            )
            output = SearchFilesOutput(files, next_link, is_indexed)
        except (ValueError, TypeError, UnicodeError):
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                received,
                graph_failure(
                    FailureKind.PROTOCOL, operation, "provider returned an invalid search response"
                ),
            )
        if not output.files and not output.next_page_token:
            return QueryAttempt.branch_result(
                defs.NOT_FOUND,
                output,
                received,
                graph_failure(FailureKind.NOT_FOUND, operation, "no item matched the search"),
            )
        return QueryAttempt.branch_result(defs.FOUND, output, received)


def _matches_mime(item: DriveItem, mime_type: str) -> bool:
    return not mime_type or (not item.is_folder and _go_equal_fold(item.mime_type, mime_type))


def _matches_item(item: DriveItem, input: SearchFilesInput) -> bool:
    if input.name:
        if input.name_match == "contains":
            is_match = input.name.lower() in item.name.lower()
        else:
            is_match = _go_equal_fold(item.name, input.name)
        if not is_match:
            return False
    return _matches_mime(item, input.mime_type)


class GetFileOperation:
    """Read a single OneDrive or SharePoint drive item."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable getFile identity and branches."""
        return defs.GET_FILE_DEFINITION

    async def invoke(self, call: Call, input: GetFileInput) -> QueryAttempt[DriveItem]:
        """Read and strictly decode one item metadata response."""
        operation = "getFile"
        zero = DriveItem()
        try:
            validate_ids(input.drive_id, input.item_id)
            if not input.item_id:
                raise ValueError("missing item")
        except ValueError:
            return QueryAttempt.branch_result(
                defs.DEFECT,
                zero,
                Receipt(),
                graph_failure(
                    FailureKind.VALIDATION,
                    operation,
                    "itemId must be a Microsoft Graph item ID or root, and driveId a drive ID or blank",
                ),
            )
        credentials, failure = await self.client.resolve_credentials(call, operation, input.drive_id)
        if failure is not None or credentials is None:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), failure)
        target = (
            self.client.item_url(input.drive_id, input.item_id) + "?" + odata_query(("$select", ITEM_FIELDS))
        )
        exchange = await self.client.send(call, credentials, "GET", target)
        failed = _query_failure(self.client, call, operation, exchange, zero, not_found=defs.NOT_FOUND)
        if failed is not None:
            return failed
        received = receipt(call, exchange.request_id, input.item_id, self.client.now())
        try:
            item = _response_item(exchange)
        except (ValueError, TypeError, UnicodeError):
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                received,
                graph_failure(FailureKind.PROTOCOL, operation, "provider returned an invalid item response"),
            )
        return QueryAttempt.branch_result(
            defs.FOUND, item, receipt(call, exchange.request_id, item.id, self.client.now())
        )


class ReadFileTextOperation:
    """Read bounded UTF-8 file content, following one safe download redirect."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable readFileText identity and branches."""
        return defs.READ_FILE_TEXT_DEFINITION

    async def invoke(self, call: Call, input: ReadFileTextInput) -> QueryAttempt[ReadFileTextOutput]:
        """Read metadata, then content only for a small supported text file."""
        operation = "readFileText"
        zero = ReadFileTextOutput()
        try:
            validate_ids(input.drive_id, input.item_id)
            if not input.item_id or input.item_id == "root":
                raise ValueError("missing file")
        except ValueError:
            return QueryAttempt.branch_result(
                defs.DEFECT,
                zero,
                Receipt(),
                graph_failure(
                    FailureKind.VALIDATION,
                    operation,
                    "itemId must be a Microsoft Graph file item ID, and driveId a drive ID or blank",
                ),
            )
        credentials, failure = await self.client.resolve_credentials(call, operation, input.drive_id)
        if failure is not None or credentials is None:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), failure)
        item_url = self.client.item_url(input.drive_id, input.item_id)
        exchange = await self.client.send(
            call, credentials, "GET", item_url + "?" + odata_query(("$select", TEXT_FIELDS))
        )
        credentials = exchange.credentials or credentials
        failed = _query_failure(self.client, call, operation, exchange, zero, not_found=defs.NOT_FOUND)
        if failed is not None:
            return failed
        metadata_receipt = receipt(call, exchange.request_id, input.item_id, self.client.now())
        try:
            item = _response_item(exchange)
        except (ValueError, TypeError, UnicodeError):
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                metadata_receipt,
                graph_failure(FailureKind.PROTOCOL, operation, "provider returned an invalid item response"),
            )
        output = ReadFileTextOutput(item.id, item.name, item.mime_type, item.size_bytes)
        reason = _unsupported_reason(item)
        if reason:
            return QueryAttempt.branch_result(
                defs.UNSUPPORTED_CONTENT,
                output,
                metadata_receipt,
                graph_failure(FailureKind.VALIDATION, operation, reason),
            )
        if item.size_bytes > self.client.max_text_bytes:
            return QueryAttempt.branch_result(
                defs.TOO_LARGE,
                output,
                metadata_receipt,
                graph_failure(
                    FailureKind.RESPONSE_TOO_LARGE, operation, "file size exceeds the configured text limit"
                ),
            )
        content = await self.client.send(
            call, credentials, "GET", item_url + "/content", limit=self.client.max_text_bytes, raw=True
        )
        classified = self._classify_content(call, output, content, False)
        if classified is not None:
            return classified
        if content.status in (301, 302, 303, 307, 308):
            location = (content.headers or httpx.Headers()).get("Location", "")
            if not self._valid_download_url(location):
                return QueryAttempt.branch_result(
                    defs.INVALID_RESPONSE,
                    output,
                    receipt(call, content.request_id, output.item_id, self.client.now()),
                    graph_failure(
                        FailureKind.PROTOCOL, operation, "provider returned an unusable download redirect"
                    ),
                )
            downloaded = await self.client.send(
                call,
                credentials,
                "GET",
                location,
                limit=self.client.max_text_bytes,
                raw=True,
                preauthenticated=True,
            )
            downloaded = Exchange(
                downloaded.status, downloaded.headers, downloaded.body, downloaded.error, content.request_id
            )
            classified = self._classify_content(call, output, downloaded, True)
            if classified is not None:
                return classified
            if downloaded.status in (301, 302, 303, 307, 308):
                return QueryAttempt.branch_result(
                    defs.INVALID_RESPONSE,
                    output,
                    receipt(call, content.request_id, output.item_id, self.client.now()),
                    graph_failure(FailureKind.PROTOCOL, operation, "file download redirected more than once"),
                )
            content = downloaded
        try:
            text = content.body.decode("utf-8")
            if "\x00" in text:
                raise ValueError("NUL byte")
        except (UnicodeDecodeError, ValueError):
            return QueryAttempt.branch_result(
                defs.UNSUPPORTED_CONTENT,
                output,
                receipt(call, content.request_id, item.id, self.client.now()),
                graph_failure(FailureKind.VALIDATION, operation, "file content is not valid UTF-8 text"),
            )
        output = ReadFileTextOutput(
            item.id, item.name, item.mime_type, item.size_bytes, text, len(content.body)
        )
        return QueryAttempt.branch_result(
            defs.READ, output, receipt(call, content.request_id, item.id, self.client.now())
        )

    def _valid_download_url(self, location: str) -> bool:
        try:
            parsed = urlsplit(location)
            if not parsed.netloc or parsed.username or parsed.password or parsed.fragment:
                return False
            if parsed.scheme == "https":
                return True
            return (
                parsed.scheme == "http"
                and urlsplit(self.client.endpoint).scheme == "http"
                and parsed.hostname in ("localhost", "127.0.0.1", "::1")
            )
        except ValueError:
            return False

    def _classify_content(
        self, call: Call, output: ReadFileTextOutput, exchange: Exchange, download: bool
    ) -> QueryAttempt[ReadFileTextOutput] | None:
        operation = "readFileText"
        received = receipt(call, exchange.request_id, output.item_id, self.client.now())
        if exchange.error == "tooLarge":
            return QueryAttempt.branch_result(
                defs.TOO_LARGE,
                output,
                received,
                graph_failure(
                    FailureKind.RESPONSE_TOO_LARGE,
                    operation,
                    "file content exceeds the configured text limit",
                ),
            )
        if exchange.error == "defect":
            return QueryAttempt.branch_result(
                defs.DEFECT,
                output,
                received,
                graph_failure(FailureKind.LOCAL_DEFECT, operation, "provider request could not be built"),
            )
        if exchange.error:
            return QueryAttempt.retry(graph_failure(FailureKind.TRANSPORT, operation, "file download failed"))
        if 200 <= exchange.status < 300 or exchange.status in (301, 302, 303, 307, 308):
            return None
        if download and exchange.status in (401, 403):
            return QueryAttempt.retry(
                graph_failure(FailureKind.AUTHORIZATION, operation, "file download link expired")
            )
        return _query_failure(self.client, call, operation, exchange, output, not_found=defs.NOT_FOUND)


def _unsupported_reason(item: DriveItem) -> str:
    if item.is_folder:
        return "item is a folder"
    if item.is_package:
        return "item is a package, such as a OneNote notebook"
    media_type = item.mime_type.split(";", 1)[0].strip().lower()
    if (
        media_type.startswith("text/")
        or media_type in TEXT_MIME_TYPES
        or media_type.endswith(("+json", "+xml"))
    ):
        return ""
    if (
        item.mime_type in ("", "application/octet-stream")
        and _go_path_extension(item.name).lower() in TEXT_EXTENSIONS
    ):
        return ""
    return "file type is not text"


class UploadFileOperation:
    """PUT one file by stable path with conflict read-back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable uploadFile identity and branches."""
        return defs.UPLOAD_FILE_DEFINITION

    def idempotency_key(self, call_id: CallID, _: UploadFileInput) -> IdempotencyKey:
        """Use the stable connector call ID as the receipt key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UploadFileInput) -> MutationAttempt[UploadFileOutput]:
        """Upload one file or classify the existing item after a conflict."""
        operation = "uploadFile"
        zero = UploadFileOutput()
        try:
            validate_ids(input.drive_id, input.parent_folder_id)
            validate_name(input.name, False)
            if input.conflict_behavior not in ("fail", "replace"):
                raise ValueError("conflictBehavior must be fail or replace")
            if not _plain_media_type(input.mime_type):
                raise ValueError("mimeType must be one lowercase type/subtype without parameters")
            if input.text_content and input.byte_content:
                raise ValueError("set textContent or byteContent, not both")
            binary = base64.b64decode(input.byte_content, validate=True) if input.byte_content else b""
            content = input.text_content.encode("utf-8") if input.text_content else binary
            if len(content) > self.client.max_upload_bytes:
                raise ValueError(
                    f"content exceeds the configured upload limit of {self.client.max_upload_bytes} bytes"
                )
        except (ValueError, binascii.Error) as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), graph_failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, failure = await self.client.resolve_credentials(call, operation, input.drive_id)
        if failure is not None or credentials is None:
            return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), failure)
        target = (
            self.client.child_url(input.drive_id, input.parent_folder_id, input.name)
            + ":/content?"
            + odata_query(("@microsoft.graph.conflictBehavior", input.conflict_behavior))
        )
        exchange = await self.client.send(
            call, credentials, "PUT", target, body=content, content_type=input.mime_type
        )
        credentials = exchange.credentials or credentials
        received = receipt(call, exchange.request_id, now=self.client.now())
        if exchange.error == "defect":
            return MutationAttempt.branch_result(
                defs.DEFECT,
                zero,
                received,
                graph_failure(FailureKind.LOCAL_DEFECT, operation, "provider request could not be built"),
            )
        if exchange.error:
            return MutationAttempt.retry(
                graph_failure(FailureKind.TRANSPORT, operation, "provider upload outcome is unknown")
            )
        codes = read_error_tokens(exchange.body, ERROR_POINTERS)
        if 200 <= exchange.status < 300:
            try:
                item = _response_item(exchange)
                if item.is_folder:
                    raise ValueError("folder upload")
            except (ValueError, TypeError, UnicodeError):
                return MutationAttempt.retry(
                    graph_failure(
                        FailureKind.PROTOCOL, operation, "provider returned an invalid upload response"
                    )
                )
            return MutationAttempt.branch_result(
                defs.UPLOADED,
                UploadFileOutput(item),
                receipt(call, exchange.request_id, item.id, self.client.now()),
            )
        if exchange.status in (409, 423):
            return await self._read_back(call, credentials, input, content, codes)
        if retryable_status(exchange.status, codes):
            delay = parse_retry_after(
                (exchange.headers or httpx.Headers()).get("Retry-After", ""), self.client.now()
            )
            return MutationAttempt.retry(
                graph_failure(
                    status_failure_kind(exchange.status, codes),
                    operation,
                    "provider temporarily rejected the upload",
                ),
                delay,
            )
        return MutationAttempt.branch_result(
            defs.PROVIDER_REJECTED,
            zero,
            received,
            graph_failure(
                status_failure_kind(exchange.status, codes), operation, "provider rejected the upload"
            ),
        )

    async def _read_back(
        self, call: Call, credentials: Any, input: UploadFileInput, content: bytes, codes: tuple[str, ...]
    ) -> MutationAttempt[UploadFileOutput]:
        operation = "uploadFile"
        zero = UploadFileOutput()
        target = (
            self.client.child_url(input.drive_id, input.parent_folder_id, input.name)
            + "?"
            + odata_query(("$select", ITEM_FIELDS))
        )
        exchange = await self.client.send(call, credentials, "GET", target)
        failed = _mutation_read_failure(self.client, call, operation, exchange, zero)
        if failed is not None:
            if exchange.status == 404 and failed.kind == "retry":
                return MutationAttempt.retry(
                    graph_failure(FailureKind.CONFLICT, operation, "conflicting item is not readable yet")
                )
            return failed
        received = receipt(call, exchange.request_id, now=self.client.now())
        try:
            item = _response_item(exchange)
        except (ValueError, TypeError, UnicodeError):
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                received,
                graph_failure(FailureKind.PROTOCOL, operation, "provider returned an invalid item response"),
            )
        received = receipt(call, exchange.request_id, item.id, self.client.now())
        if not item.is_folder and not item.is_package and not item.quick_xor_hash:
            return MutationAttempt.retry(
                graph_failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "provider has not reported the existing file's content hash yet",
                )
            )
        from .quick_xor_hash import compute_quick_xor_hash

        identical = (
            not item.is_folder
            and not item.is_package
            and _go_equal_fold(item.name, input.name)
            and item.size_bytes == len(content)
            and item.quick_xor_hash == compute_quick_xor_hash(content)
        )
        if identical:
            return MutationAttempt.branch_result(defs.UPLOADED, UploadFileOutput(item, True), received)
        if input.conflict_behavior == "fail" and "nameAlreadyExists" in codes:
            return MutationAttempt.branch_result(
                defs.ALREADY_EXISTS,
                UploadFileOutput(item),
                received,
                graph_failure(FailureKind.CONFLICT, operation, "an item with this name already exists"),
            )
        return MutationAttempt.branch_result(
            defs.PROVIDER_REJECTED,
            zero,
            received,
            graph_failure(
                FailureKind.CONFLICT,
                operation,
                "provider rejected the upload because the item is locked or conflicts",
            ),
        )


class CreateFolderOperation:
    """POST one folder with fail-on-conflict and read-back reconciliation."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable createFolder identity and branches."""
        return defs.CREATE_FOLDER_DEFINITION

    def idempotency_key(self, call_id: CallID, _: CreateFolderInput) -> IdempotencyKey:
        """Use the stable connector call ID as the receipt key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateFolderInput) -> MutationAttempt[CreateFolderOutput]:
        """Create a folder or read the existing item after a name conflict."""
        operation = "createFolder"
        zero = CreateFolderOutput()
        try:
            validate_ids(input.drive_id, input.parent_folder_id)
            validate_name(input.name, True)
        except ValueError as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), graph_failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, failure = await self.client.resolve_credentials(call, operation, input.drive_id)
        if failure is not None or credentials is None:
            return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), failure)
        body = json.dumps(
            {"@microsoft.graph.conflictBehavior": "fail", "folder": {}, "name": input.name},
            separators=(",", ":"),
            ensure_ascii=False,
        ).encode()
        target = (
            self.client.item_url(input.drive_id, input.parent_folder_id)
            + "/children?"
            + odata_query(("@microsoft.graph.conflictBehavior", "fail"))
        )
        exchange = await self.client.send(
            call, credentials, "POST", target, body=body, content_type="application/json"
        )
        credentials = exchange.credentials or credentials
        received = receipt(call, exchange.request_id, now=self.client.now())
        if exchange.error == "defect":
            return MutationAttempt.branch_result(
                defs.DEFECT,
                zero,
                received,
                graph_failure(FailureKind.LOCAL_DEFECT, operation, "provider request could not be built"),
            )
        if exchange.error:
            return MutationAttempt.retry(
                graph_failure(FailureKind.TRANSPORT, operation, "provider folder outcome is unknown")
            )
        codes = read_error_tokens(exchange.body, ERROR_POINTERS)
        if 200 <= exchange.status < 300:
            try:
                item = _response_item(exchange)
                if not item.is_folder:
                    raise ValueError("not folder")
            except (ValueError, TypeError, UnicodeError):
                return MutationAttempt.retry(
                    graph_failure(
                        FailureKind.PROTOCOL, operation, "provider returned an invalid folder response"
                    )
                )
            return MutationAttempt.branch_result(
                defs.CREATED,
                CreateFolderOutput(item),
                receipt(call, exchange.request_id, item.id, self.client.now()),
            )
        if exchange.status == 409 and "nameAlreadyExists" in codes:
            return await self._read_back(call, credentials, input)
        if retryable_status(exchange.status, codes):
            delay = parse_retry_after(
                (exchange.headers or httpx.Headers()).get("Retry-After", ""), self.client.now()
            )
            return MutationAttempt.retry(
                graph_failure(
                    status_failure_kind(exchange.status, codes),
                    operation,
                    "provider temporarily rejected the folder creation",
                ),
                delay,
            )
        return MutationAttempt.branch_result(
            defs.PROVIDER_REJECTED,
            zero,
            received,
            graph_failure(
                status_failure_kind(exchange.status, codes),
                operation,
                "provider rejected the folder creation",
            ),
        )

    async def _read_back(
        self, call: Call, credentials: Any, input: CreateFolderInput
    ) -> MutationAttempt[CreateFolderOutput]:
        operation = "createFolder"
        zero = CreateFolderOutput()
        target = (
            self.client.child_url(input.drive_id, input.parent_folder_id, input.name)
            + "?"
            + odata_query(("$select", ITEM_FIELDS))
        )
        exchange = await self.client.send(call, credentials, "GET", target)
        failed = _mutation_read_failure(self.client, call, operation, exchange, zero)
        if failed is not None:
            if exchange.status == 404 and failed.kind == "retry":
                return MutationAttempt.retry(
                    graph_failure(FailureKind.CONFLICT, operation, "conflicting item is not readable yet")
                )
            return failed
        received = receipt(call, exchange.request_id, now=self.client.now())
        try:
            item = _response_item(exchange)
        except (ValueError, TypeError, UnicodeError):
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                received,
                graph_failure(FailureKind.PROTOCOL, operation, "provider returned an invalid item response"),
            )
        received = receipt(call, exchange.request_id, item.id, self.client.now())
        if item.is_folder:
            return MutationAttempt.branch_result(defs.CREATED, CreateFolderOutput(item, True), received)
        return MutationAttempt.branch_result(
            defs.NAME_CONFLICT,
            CreateFolderOutput(item),
            received,
            graph_failure(FailureKind.CONFLICT, operation, "a file already has this name"),
        )
