# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WPS 365 folder metadata Query with Go-compatible provider outcomes."""

import hashlib
import hmac
import json
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_FOLDER_FILES_DEFINITION as LIST_FOLDER_FILES_DEFINITION,
)
from ._generated.definitions import (
    LIST_FOLDER_FILES_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import FileMetadata, ListFolderFilesInput, ListFolderFilesOutput


def _valid_resource_id(value: str) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and len(value) <= 256
        and value.strip() == value
        and all(33 <= ord(character) <= 126 and character not in "/?#" for character in value)
    )


def _valid_page_token(value: str) -> bool:
    return isinstance(value, str) and len(value) <= 2048 and all(33 <= ord(c) <= 126 for c in value)


def _valid_secret(value: str) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and len(value) <= 512
        and all(33 <= ord(c) <= 126 for c in value)
    )


def _invalid_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant {value}")


def _json_object(body: bytes) -> dict[str, object]:
    decoded = json.loads(body.decode("utf-8"), parse_constant=_invalid_constant)
    if not isinstance(decoded, dict):
        raise ValueError("expected JSON object")
    return decoded


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListFolderFilesOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListFolderFilesOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, LIST_FOLDER_FILES_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListFolderFilesOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, LIST_FOLDER_FILES_OPERATION_ID, message), delay)


def _http_failure(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status in (400, 404, 422):
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


class Client:
    """Bounded WPS HTTP client with an in-memory application token cache."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("WPS configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0 or not 1 <= config.page_size <= 500:
            raise ValueError("WPS timeout, response limit, or page size is invalid")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("WPS API URL must be a host root")
        self.page_size = config.page_size
        self.max_response_bytes = config.max_response_bytes
        self.request_signing_enabled = config.request_signing_enabled
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._token_fingerprint = b""
        self._token = ""
        self._token_expiry = datetime.min.replace(tzinfo=UTC)

    def list_folder_files(self) -> "ListFolderFilesOperation":
        """Bind the file-list Query to this client."""
        return ListFolderFilesOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()

    async def _access_token(
        self, app_id: str, app_key: str
    ) -> tuple[str, QueryAttempt[ListFolderFilesOutput] | None]:
        fingerprint = hashlib.sha256(f"{app_id}:{app_key}".encode()).digest()
        if (
            self._token
            and fingerprint == self._token_fingerprint
            and datetime.now(UTC) + timedelta(minutes=5) < self._token_expiry
        ):
            return self._token, None
        form = urlencode({"grant_type": "client_credentials", "client_id": app_id, "client_secret": app_key})
        request = self.http_client.build_request(
            "POST",
            self.base_url + "/oauth2/token",
            headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
            content=form.encode(),
        )
        try:
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return "", _retry(FailureKind.AVAILABILITY, "WPS token request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return "", _retry(FailureKind.RATE_LIMIT, "WPS token request was rate limited", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return "", _retry(FailureKind.AVAILABILITY, "WPS token service is unavailable", delay)
            if response.status_code != 200:
                return "", _branch(
                    PROVIDER_REJECTED,
                    _http_failure(response.status_code),
                    "WPS rejected application credentials",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return "", _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "WPS token response exceeded limit"
                )
            except ProviderTransportError:
                return "", _retry(FailureKind.AVAILABILITY, "WPS token response was interrupted")
            try:
                document = _json_object(body)
                code = document.get("code", 0)
                if type(code) is not int:
                    raise ValueError("invalid code")
                if code != 0:
                    return "", _branch(
                        PROVIDER_REJECTED, FailureKind.AUTHENTICATION, "WPS rejected application credentials"
                    )
                token = document.get("access_token")
                expiry = document.get("expires_in")
                token_type = document.get("token_type")
                if (
                    not isinstance(token, str)
                    or not token
                    or any(character.isspace() for character in token)
                    or type(expiry) is not int
                    or not 360 <= expiry <= 86400
                    or not isinstance(token_type, str)
                    or token_type.lower() != "bearer"
                ):
                    raise ValueError("invalid token")
            except (ValueError, TypeError, UnicodeDecodeError):
                return "", _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "WPS token response was invalid")
            self._token_fingerprint = fingerprint
            self._token = token
            self._token_expiry = datetime.now(UTC) + timedelta(seconds=expiry)
            return token, None
        finally:
            await response.aclose()


class ListFolderFilesOperation:
    """Fetch one bounded page of enterprise-drive file metadata."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WPS client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_FOLDER_FILES_DEFINITION

    async def invoke(self, call: Call, input: ListFolderFilesInput) -> QueryAttempt[ListFolderFilesOutput]:
        """Fetch a token and one file page, classifying every provider outcome."""
        if (
            not _valid_resource_id(input.drive_id)
            or not _valid_resource_id(input.parent_id)
            or not _valid_page_token(input.page_token)
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "drive, folder, or page token is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id = credentials.app_id
            app_key = credentials.app_key.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "WPS credentials unavailable")
        if not _valid_resource_id(app_id) or not _valid_secret(app_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "WPS credentials invalid")
        token, failure = await self.client._access_token(app_id, app_key)
        if failure is not None:
            return failure
        path = f"/v7/drives/{quote(input.drive_id, safe='')}/files/{quote(input.parent_id, safe='')}/children"
        query = {"page_size": str(self.client.page_size)}
        if input.page_token:
            query["page_token"] = input.page_token
        uri = path + "?" + urlencode(query)
        headers = {"Accept": "application/json", "Authorization": "Bearer " + token}
        if self.client.request_signing_enabled:
            kso_date = format_datetime(datetime.now(UTC), usegmt=True)
            signed = "KSO-1" + "GET" + uri + "application/json" + kso_date
            signature = hmac.new(app_key.encode(), signed.encode(), hashlib.sha256).hexdigest()
            headers.update(
                {
                    "Content-Type": "application/json",
                    "X-Kso-Date": kso_date,
                    "X-Kso-Authorization": f"KSO-1 {app_id}:{signature}",
                }
            )
        request = self.client.http_client.build_request("GET", self.client.base_url + uri, headers=headers)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "WPS file-list request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "WPS rate limited file listing", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "WPS file listing is temporarily unavailable", delay)
            if response.status_code != 200:
                return _branch(
                    PROVIDER_REJECTED, _http_failure(response.status_code), "WPS rejected file listing"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "WPS file-list response exceeded limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "WPS file-list response was interrupted")
            try:
                document = _json_object(body)
                code = document.get("code")
                if type(code) is not int:
                    raise ValueError("missing status code")
                if code == 429000001:
                    return _retry(FailureKind.RATE_LIMIT, "WPS rate limited file listing", delay)
                if code != 0:
                    kind = {
                        40100011: FailureKind.AUTHENTICATION,
                        40100008: FailureKind.AUTHENTICATION,
                        403000001: FailureKind.AUTHORIZATION,
                        40300010: FailureKind.AUTHORIZATION,
                        400000001: FailureKind.QUOTA_EXHAUSTED,
                    }.get(code, FailureKind.PROVIDER_REJECTION)
                    return _branch(PROVIDER_REJECTED, kind, "WPS rejected file listing")
                data = document.get("data")
                if not isinstance(data, dict):
                    raise ValueError("missing page")
                raw_items = data.get("items")
                next_page_token = data.get("next_page_token", "")
                if not isinstance(raw_items, list) or len(raw_items) > self.client.page_size:
                    raise ValueError("invalid items")
                if not isinstance(next_page_token, str) or len(next_page_token) > 2048:
                    raise ValueError("invalid page token")
                items: list[FileMetadata] = []
                for raw_item in raw_items:
                    if not isinstance(raw_item, dict):
                        raise ValueError("invalid file item")
                    file_id, name, file_type, size = (
                        raw_item.get(key) for key in ("id", "name", "type", "size")
                    )
                    if (
                        not all(isinstance(value, str) and value for value in (file_id, name, file_type))
                        or type(size) is not int
                        or size < 0
                    ):
                        raise ValueError("invalid file metadata")
                    assert isinstance(file_id, str) and isinstance(name, str) and isinstance(file_type, str)
                    items.append(FileMetadata(file_id, name, file_type, size))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "WPS returned an invalid file-list page"
                )
            return QueryAttempt.branch_result(
                LISTED, ListFolderFilesOutput(tuple(items), next_page_token), Receipt()
            )
        finally:
            await response.aclose()


Config.__doc__ = "WPS endpoint, request limits, page size, and optional KSO-1 request signing."
Credentials.__doc__ = "Application identifier and private APPKEY resolved for each Query."
