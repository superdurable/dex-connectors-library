# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Microsoft Graph transport and strict drive-item decoding."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, quote_plus, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    Receipt,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ._generated.timestamps import parse_go_datetime
from .models import DriveItem, ItemIdentity

if TYPE_CHECKING:
    from .operations import (
        CreateFolderOperation,
        GetFileOperation,
        ReadFileTextOperation,
        SearchFilesOperation,
        UploadFileOperation,
    )

GRAPH_ID = re.compile(r"[A-Za-z0-9!_.-]{1,256}\Z")
ITEM_FIELDS = (
    "id,name,size,webUrl,eTag,cTag,createdDateTime,lastModifiedDateTime,"
    "parentReference,file,folder,package,createdBy,lastModifiedBy"
)
SEARCH_FIELDS = "id,name,size,webUrl,lastModifiedDateTime,parentReference,file,folder,package"
TEXT_FIELDS = "id,name,size,file,folder,package"
ERROR_POINTERS = ("/error/code", "/error/innerError/code", "/error/innererror/code")
RETRYABLE_CODES = frozenset(
    ("activityLimitReached", "throttledRequest", "TooManyRequests", "serviceNotAvailable")
)
NAME_RESERVED = frozenset('/\\*<>?:|#%"')


def graph_failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Build one credential-safe Graph failure."""
    return Failure(kind, "microsoft-graph", operation, message)


def receipt(call: Call, request_id: str = "", object_id: str = "", now: datetime | None = None) -> Receipt:
    """Record the production receipt shape without provider secrets."""
    return Receipt(
        call.id, "microsoft-graph", now or datetime.now(UTC), call.idempotency_key, object_id, request_id
    )


def path_escape(value: str) -> str:
    """Encode one path segment like Go url.PathEscape."""
    return quote(value, safe="-._~$&+:=@")


def query_escape(value: str) -> str:
    """Encode one query value like Go url.QueryEscape."""
    return quote_plus(value, safe="-._~")


def odata_query(*pairs: tuple[str, str]) -> str:
    """Keep fixed OData keys literal while escaping values."""
    return "&".join(f"{key}={query_escape(value)}" for key, value in pairs)


def validate_ids(drive_id: str, item_id: str) -> None:
    """Reject malformed Graph IDs before credential or provider access."""
    if drive_id and not GRAPH_ID.fullmatch(drive_id):
        raise ValueError("driveId must be a Microsoft Graph drive ID or blank")
    if item_id and not GRAPH_ID.fullmatch(item_id):
        raise ValueError("folder ID must be a Microsoft Graph item ID, root, or blank")


def validate_name(name: str, folder: bool) -> None:
    """Apply the Go connector's item-name restrictions."""
    if not name.strip():
        raise ValueError("name is required")
    if len(name.encode("utf-8")) > 1024:
        raise ValueError("name is too long or not valid UTF-8")
    if name.strip() != name:
        raise ValueError("name cannot start or end with whitespace")
    if any(character in NAME_RESERVED for character in name):
        raise ValueError('name cannot contain / \\ * < > ? : | # % or "')
    if name.startswith("~"):
        raise ValueError("name cannot start with ~")
    if folder and name.endswith("."):
        raise ValueError("folder name cannot end with a period")
    if any(ord(character) < 32 or ord(character) == 127 for character in name):
        raise ValueError("name cannot contain control characters")


def _strict_int(value: object, field: str) -> int:
    if type(value) is not int or not -(2**63) <= value < 2**63:
        raise ValueError(f"{field} is not a Go int64")
    return value


def _string(value: object, field: str) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"{field} is not a string")
    return value


def _mapping(value: object, field: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{field} is not an object")
    return value


def _identity(value: object) -> ItemIdentity | None:
    if value is None:
        return None
    record = _mapping(value, "identity")
    user = record.get("user")
    if user is not None:
        person = _mapping(user, "user")
        return ItemIdentity(
            _string(person.get("displayName", ""), "displayName"), _string(person.get("email", ""), "email")
        )
    app = record.get("application")
    if app is not None:
        application = _mapping(app, "application")
        return ItemIdentity(_string(application.get("displayName", ""), "displayName"), "", True)
    return None


def decode_item(raw: object) -> DriveItem:
    """Reject malformed or lossy Graph metadata and omit download URLs."""
    value = _mapping(raw, "driveItem")
    item_id = _string(value.get("id", ""), "id")
    name = _string(value.get("name", ""), "name")
    if not GRAPH_ID.fullmatch(item_id) or not name:
        raise ValueError("item resource lacks a valid ID or name")
    raw_size = value.get("size")
    size = _strict_int(0 if raw_size is None else raw_size, "size")
    if size < 0:
        raise ValueError("item size is invalid")
    parent = (
        _mapping(value["parentReference"], "parentReference")
        if value.get("parentReference") is not None
        else {}
    )
    file = _mapping(value["file"], "file") if value.get("file") is not None else {}
    folder = _mapping(value["folder"], "folder") if value.get("folder") is not None else {}
    hashes = _mapping(file["hashes"], "hashes") if file.get("hashes") is not None else {}
    created = value.get("createdDateTime")
    modified = value.get("lastModifiedDateTime")
    return DriveItem(
        id=item_id,
        name=name,
        drive_id=_string(parent.get("driveId", ""), "driveId"),
        drive_type=_string(parent.get("driveType", ""), "driveType"),
        site_id=_string(parent.get("siteId", ""), "siteId"),
        parent_folder_id=_string(parent.get("id", ""), "parentReference.id"),
        parent_path=_string(parent.get("path", ""), "path"),
        is_folder=value.get("folder") is not None or value.get("root") is not None,
        is_package=value.get("package") is not None,
        child_count=_strict_int(
            0 if folder.get("childCount") is None else folder["childCount"], "childCount"
        ),
        mime_type=_string(file.get("mimeType", ""), "mimeType"),
        size_bytes=size,
        quick_xor_hash=_string(hashes.get("quickXorHash", ""), "quickXorHash"),
        e_tag=_string(value.get("eTag", ""), "eTag"),
        c_tag=_string(value.get("cTag", ""), "cTag"),
        created_date_time=parse_go_datetime(created, "createdDateTime")
        if created is not None
        else datetime.min.replace(tzinfo=UTC),
        last_modified_date_time=parse_go_datetime(modified, "lastModifiedDateTime")
        if modified is not None
        else datetime.min.replace(tzinfo=UTC),
        web_url=_string(value.get("webUrl", ""), "webUrl"),
        created_by=_identity(value.get("createdBy")),
        last_modified_by=_identity(value.get("lastModifiedBy")),
    )


def decode_json(content: bytes) -> object:
    """Decode Graph JSON while rejecting nonfinite literals and lossy numbers."""

    def reject_constant(value: str) -> object:
        raise ValueError(f"nonfinite JSON number {value}")

    return json.loads(content, parse_constant=reject_constant)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded Graph exchange, including status available before a body-read error."""

    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    error: str = ""
    request_id: str = ""
    credentials: Credentials | None = None


class Client:
    """Bind the five Graph operations to one credential source and HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Microsoft OneDrive configuration and credentials are required")
        endpoint = validate_base_url(config.endpoint)
        if config.max_response_bytes < 1 or config.max_text_bytes < 1:
            raise ValueError("Microsoft OneDrive response and text limits must be positive")
        if not 1 <= config.max_upload_bytes <= 5 * 1024 * 1024:
            raise ValueError("Microsoft OneDrive upload limit must be from 1 to 5242880 bytes")
        if not 1 <= config.search_page_size <= 200:
            raise ValueError("Microsoft OneDrive search page size must be from 1 to 200")
        self.endpoint = endpoint
        self.base_url = endpoint + "/v1.0"
        self.max_response_bytes = config.max_response_bytes
        self.max_text_bytes = config.max_text_bytes
        self.max_upload_bytes = config.max_upload_bytes
        self.search_page_size = config.search_page_size
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Microsoft OneDrive and SharePoint", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client
        )

    async def aclose(self) -> None:
        """Close only a transport constructed by this client."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def search_files(self) -> "SearchFilesOperation":
        """Return the searchFiles Query."""
        from .operations import SearchFilesOperation

        return SearchFilesOperation(self)

    def get_file(self) -> "GetFileOperation":
        """Return the getFile Query."""
        from .operations import GetFileOperation

        return GetFileOperation(self)

    def read_file_text(self) -> "ReadFileTextOperation":
        """Return the readFileText Query."""
        from .operations import ReadFileTextOperation

        return ReadFileTextOperation(self)

    def upload_file(self) -> "UploadFileOperation":
        """Return the uploadFile Mutation."""
        from .operations import UploadFileOperation

        return UploadFileOperation(self)

    def create_folder(self) -> "CreateFolderOperation":
        """Return the createFolder Mutation."""
        from .operations import CreateFolderOperation

        return CreateFolderOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str, drive_id: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve current token with generated refresh and enforce app-only drive selection."""
        try:
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
            token = credentials.access_token.reveal() if credentials.access_token else ""
            if not is_header_safe_credential(token) or credentials.auth_method_id not in (
                "",
                "microsoft-oauth",
                "microsoft-app-only",
            ):
                raise ValueError("invalid token")
        except Exception:
            return None, graph_failure(
                FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable"
            )
        if not drive_id and credentials.auth_method_id == "microsoft-app-only":
            return None, graph_failure(
                FailureKind.VALIDATION,
                operation,
                "driveId is required with app-only authorization, which has no signed-in user",
            )
        return credentials, None

    def drive_url(self, drive_id: str) -> str:
        """Address a selected drive or the signed-in user's drive."""
        return self.base_url + ("/drives/" + path_escape(drive_id) if drive_id else "/me/drive")

    def item_url(self, drive_id: str, item_id: str) -> str:
        """Address a drive item, treating blank and root as the root."""
        return self.drive_url(drive_id) + (
            "/items/" + path_escape(item_id) if item_id and item_id != "root" else "/root"
        )

    def child_url(self, drive_id: str, parent_id: str, name: str) -> str:
        """Address one child by parent and escaped path name."""
        return self.item_url(drive_id, parent_id) + ":/" + path_escape(name)

    def valid_next_link(self, link: str) -> bool:
        """Accept only a configured Graph-origin v1.0 page URL."""
        try:
            parsed = urlsplit(link)
            origin = urlsplit(self.endpoint)
            return (
                parsed.scheme == origin.scheme
                and parsed.netloc == origin.netloc
                and not parsed.fragment
                and not parsed.username
                and parsed.path.startswith(origin.path + "/v1.0/")
            )
        except ValueError:
            return False

    async def send(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        target: str,
        *,
        body: bytes | None = None,
        content_type: str = "",
        limit: int | None = None,
        raw: bool = False,
        preauthenticated: bool = False,
    ) -> Exchange:
        """Send once, retrying one 401 only through a coordinated credential refresh."""
        for attempt in range(2):
            headers = (
                {}
                if preauthenticated
                else {
                    "Authorization": "Bearer "
                    + (credentials.access_token.reveal() if credentials.access_token else "")
                }
            )
            if not raw:
                headers["Accept"] = "application/json"
            if content_type:
                headers["Content-Type"] = content_type
            try:
                request = self.http_client.build_request(method, target, content=body, headers=headers)
            except (ValueError, TypeError, httpx.HTTPError):
                return Exchange(error="defect")
            try:
                response, _ = await send_traced(self.http_client, request)
            except ProviderTransportError:
                return Exchange(error="transport")
            status = response.status_code
            response_headers = response.headers
            request_id = response_headers.get("request-id", "")
            maximum = max(limit if limit is not None else self.max_response_bytes, 65536)
            try:
                content = await read_bounded_body(response, maximum)
                if 200 <= status < 300 and len(content) > (
                    limit if limit is not None else self.max_response_bytes
                ):
                    return Exchange(status, response_headers, error="tooLarge", request_id=request_id)
            except BodyTooLargeError:
                if 200 <= status < 300:
                    return Exchange(status, response_headers, error="tooLarge", request_id=request_id)
                content = b""
            except ProviderTransportError:
                return Exchange(status, response_headers, error="read", request_id=request_id)
            if (
                status != 401
                or attempt
                or preauthenticated
                or not isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                return Exchange(
                    status, response_headers, content, request_id=request_id, credentials=credentials
                )
            try:
                credentials = await resolve_credential_after_rejection(
                    self.credentials, call, self.refresh_driver
                )
            except Exception:
                return Exchange(status, response_headers, content, request_id=request_id)
        return Exchange(error="defect")

    def failed_read(
        self,
        call: Call,
        operation: str,
        exchange: Exchange,
        *,
        not_found_branch: str,
        provider_rejected_branch: str,
        invalid_branch: str,
        defect_branch: str,
    ) -> tuple[str, Failure, float, Receipt] | None:
        """Map Graph read status and body errors to Retry or a business branch."""
        received = receipt(call, exchange.request_id, now=self.now()) if exchange.status else Receipt()
        if exchange.error == "defect":
            return (
                defect_branch,
                graph_failure(FailureKind.LOCAL_DEFECT, operation, "provider request could not be built"),
                0,
                Receipt(),
            )
        if exchange.error == "tooLarge":
            return (
                invalid_branch,
                graph_failure(
                    FailureKind.RESPONSE_TOO_LARGE,
                    operation,
                    "provider response exceeds the configured limit",
                ),
                0,
                received,
            )
        if exchange.error:
            return (
                "",
                graph_failure(FailureKind.TRANSPORT, operation, "provider is unavailable"),
                0,
                Receipt(),
            )
        if 200 <= exchange.status < 300:
            return None
        codes = read_error_tokens(exchange.body, ERROR_POINTERS)
        kind = status_failure_kind(exchange.status, codes)
        if retryable_status(exchange.status, codes):
            delay = parse_retry_after(
                (exchange.headers or httpx.Headers()).get("Retry-After", ""), self.now()
            )
            return (
                "",
                graph_failure(kind, operation, "provider temporarily rejected the request"),
                delay,
                Receipt(),
            )
        if exchange.status == 404:
            return (
                not_found_branch,
                graph_failure(
                    FailureKind.NOT_FOUND, operation, "item was not found or is not visible to the connection"
                ),
                0,
                received,
            )
        return (
            provider_rejected_branch,
            graph_failure(kind, operation, "provider rejected the request"),
            0,
            received,
        )


def retryable_status(status: int, codes: tuple[str, ...]) -> bool:
    """Classify Graph throttling and availability without retrying 501 or 507."""
    return status not in (501, 507) and (
        status in (408, 429) or status >= 500 or any(code in RETRYABLE_CODES for code in codes)
    )


def status_failure_kind(status: int, codes: tuple[str, ...]) -> FailureKind:
    """Match the Go Graph status classification order."""
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 429 or any(code in RETRYABLE_CODES for code in codes):
        return FailureKind.RATE_LIMIT
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 404:
        return FailureKind.NOT_FOUND
    if status in (409, 423, 412):
        return FailureKind.CONFLICT
    if status == 507:
        return FailureKind.QUOTA_EXHAUSTED
    if status >= 500:
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION
