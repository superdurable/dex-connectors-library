# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed, bounded S3 REST client shared by the four connector operations."""

from __future__ import annotations

import hashlib
import hmac
import ipaddress
import re
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Mutation, Query, Receipt
from dex_connectors.sdk.providerhttp import (
    is_header_safe_credential,
    new_provider_http_client,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from .models import (
    GetObjectTextInput,
    HeadObjectInput,
    ListObjectsInput,
    ListObjectsOutput,
    ObjectMetadata,
    ObjectText,
    PutObjectInput,
    StoredObject,
)

_REGION = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$")
_BUCKET = re.compile(r"^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9+/=._-]{1,128}$")
_ENDPOINT_PATH = re.compile(r"^(/[A-Za-z0-9._~-]+)*$")
_EMPTY_HASH = hashlib.sha256(b"").hexdigest()


@dataclass(frozen=True, slots=True)
class Session:
    """Credentials resolved for one Invoke and never put into Dex state."""

    access_key_id: str
    secret_access_key: str
    session_token: str


@dataclass(frozen=True, slots=True)
class Response:
    """One bounded provider response; only successful bodies use the operation limit."""

    status: int
    headers: httpx.Headers
    body: bytes
    is_too_large: bool = False

    def header(self, name: str) -> str:
        """Read one HTTP header case insensitively."""
        return str(self.headers.get(name, ""))


class Client:
    """Sign S3 requests and resolve private credentials once per operation attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("S3 configuration and credential source are required")
        if not _REGION.fullmatch(config.region):
            raise ValueError("S3 region must be a Region code")
        suffix = ""
        if re.fullmatch(r"(?:us|eu|ap|sa|ca|me|af|il|mx)-\w+-\d+|us-gov-\w+-\d+", config.region):
            suffix = "amazonaws.com"
        elif re.fullmatch(r"cn-\w+-\d+", config.region):
            suffix = "amazonaws.com.cn"
        elif re.fullmatch(r"eusc-de-\w+-\d+", config.region):
            suffix = "amazonaws.eu"
        self.is_amazon_endpoint = not config.endpoint
        if self.is_amazon_endpoint and not suffix:
            raise ValueError("S3 region is not an Amazon S3 Region; set endpoint")
        endpoint = validate_base_url(config.endpoint or f"https://s3.{config.region}.{suffix}")
        parsed = urlsplit(endpoint)
        if not _ENDPOINT_PATH.fullmatch(parsed.path) or parsed.query or parsed.fragment:
            raise ValueError("S3 endpoint path is invalid")
        if config.addressing_style not in ("auto", "path", "virtualHosted"):
            raise ValueError("S3 addressingStyle is invalid")
        if config.default_bucket:
            validate_bucket(config.default_bucket)
        if (
            not 1 <= config.list_page_size <= 1000
            or config.max_response_bytes < 1
            or config.max_text_bytes < 1
        ):
            raise ValueError("S3 page size or response limit is invalid")
        if not 1 <= config.max_upload_bytes <= 5_242_880:
            raise ValueError("S3 maxUploadBytes is invalid")
        self.config = config
        self.endpoint = parsed
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(12)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def list_objects(self) -> Query[ListObjectsInput, ListObjectsOutput]:
        """Return the bucket listing Query."""
        from .operations import ListObjectsOperation

        return ListObjectsOperation(self)

    def head_object(self) -> Query[HeadObjectInput, ObjectMetadata]:
        """Return the object metadata Query."""
        from .operations import HeadObjectOperation

        return HeadObjectOperation(self)

    def get_object_text(self) -> Query[GetObjectTextInput, ObjectText]:
        """Return the bounded object text Query."""
        from .operations import GetObjectTextOperation

        return GetObjectTextOperation(self)

    def put_object(self) -> Mutation[PutObjectInput, StoredObject]:
        """Return the repeat-safe object write Mutation."""
        from .operations import PutObjectOperation

        return PutObjectOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this connector created it."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def bucket(self, value: str) -> str:
        """Resolve a validated input bucket against the connection default."""
        bucket = value or self.config.default_bucket
        if not bucket:
            raise ValueError("bucket is required because the connection has no defaultBucket")
        validate_bucket(bucket)
        if (
            self.config.addressing_style == "virtualHosted"
            and "." in bucket
            and self.endpoint.scheme == "https"
        ):
            raise ValueError("a bucket name with a dot cannot use virtualHosted addressing over HTTPS")
        return bucket

    async def start_session(self, call: Call) -> Session | None:
        """Resolve credentials and reject invalid values without exposing them."""
        try:
            credentials = await self.credentials.resolve(call)
            access_key_id = credentials.access_key_id.reveal()
            secret_access_key = credentials.secret_access_key.reveal()
            session_token = credentials.session_token.reveal()
            if (
                not is_header_safe_credential(access_key_id)
                or len(access_key_id) > 128
                or any(char in access_key_id for char in "/,=")
            ):
                return None
            if (
                not secret_access_key
                or len(secret_access_key.encode()) > 256
                or any(ord(char) < 32 or ord(char) == 127 for char in secret_access_key)
            ):
                return None
            if session_token and (not is_header_safe_credential(session_token) or len(session_token) > 4096):
                return None
            return Session(access_key_id, secret_access_key, session_token)
        except Exception:
            return None

    def receipt(self, call: Call, response: Response, bucket: str, key: str | None = None) -> Receipt:
        """Keep only validated provider correlation data in a Dex receipt."""
        request_id = response.header("x-amz-request-id")
        return Receipt(
            call.id,
            "amazon-s3",
            self.now(),
            call.idempotency_key,
            bucket if key is None else f"{bucket}/{key}",
            request_id if _REQUEST_ID.fullmatch(request_id) else "",
        )

    async def exchange(
        self,
        call: Call,
        session: Session,
        method: str,
        bucket: str,
        key: str | None = None,
        query: dict[str, str] | None = None,
        headers: dict[str, str] | None = None,
        body: bytes | None = None,
        limit: int | None = None,
    ) -> Response:
        """Sign and send one request; Dex decides whether to retry it."""
        path = self.endpoint.path.rstrip("/")
        use_path = self.config.addressing_style == "path" or (
            self.config.addressing_style == "auto" and (not self.is_amazon_endpoint or "." in bucket)
        )
        host = self.endpoint.netloc
        if use_path:
            path += "/" + bucket
            if key is not None:
                path += "/" + key
        else:
            host = bucket + "." + host
            path += "/" + (key or "")
        encoded_path = quote(path, safe="/-._~")
        encoded_query = "&".join(
            f"{quote(name, safe='-._~')}={quote(value, safe='-._~')}"
            for name, value in sorted((query or {}).items())
        )
        target = f"{self.endpoint.scheme}://{host}{encoded_path}"
        if encoded_query:
            target += "?" + encoded_query
        now = self.now().astimezone(UTC)
        amz_date = now.strftime("%Y%m%dT%H%M%SZ")
        payload_hash = hashlib.sha256(body).hexdigest() if body is not None else _EMPTY_HASH
        signed_headers = {name.lower(): value for name, value in (headers or {}).items()}
        if body is not None:
            signed_headers["content-length"] = str(len(body))
        signed_headers["x-amz-content-sha256"] = payload_hash
        signed_headers["x-amz-date"] = amz_date
        if session.session_token:
            signed_headers["x-amz-security-token"] = session.session_token
        canonical_headers = {"host": host.lower(), **signed_headers}
        names = sorted(canonical_headers)
        canonical_header_text = "".join(
            f"{name}:{' '.join(canonical_headers[name].strip().split())}\n" for name in names
        )
        canonical_request = "\n".join(
            (method, encoded_path, encoded_query, canonical_header_text, ";".join(names), payload_hash)
        )
        date = amz_date[:8]
        scope = f"{date}/{self.config.region}/s3/aws4_request"
        to_sign = (
            f"AWS4-HMAC-SHA256\n{amz_date}\n{scope}\n{hashlib.sha256(canonical_request.encode()).hexdigest()}"
        )
        signing_key = ("AWS4" + session.secret_access_key).encode()
        for component in (date, self.config.region, "s3", "aws4_request"):
            signing_key = hmac.new(signing_key, component.encode(), hashlib.sha256).digest()
        signature = hmac.new(signing_key, to_sign.encode(), hashlib.sha256).hexdigest()
        signed_headers["authorization"] = (
            f"AWS4-HMAC-SHA256 Credential={session.access_key_id}/{scope}, "
            f"SignedHeaders={';'.join(names)}, Signature={signature}"
        )
        signed_headers["accept-encoding"] = "identity"
        request = self.http_client.build_request(method, target, headers=signed_headers, content=body)
        response = await self.http_client.send(request, stream=True, follow_redirects=False)
        try:
            if 200 <= response.status_code < 300:
                if method in ("HEAD", "PUT"):
                    return Response(response.status_code, response.headers, b"")
                if method == "GET" and key is not None and limit is not None:
                    declared_length = response.headers.get("content-length", "")
                    if declared_length.isdigit() and int(declared_length) > limit:
                        return Response(response.status_code, response.headers, b"", True)
            response_limit = limit if limit is not None else self.config.max_response_bytes
            if response.status_code < 200 or response.status_code >= 300:
                response_limit = 65_536
            chunks: list[bytes] = []
            size = 0
            if response.is_stream_consumed:
                chunks_to_read: AsyncIterator[bytes] = _preloaded_chunks(response)
            else:
                chunks_to_read = response.aiter_raw()
            async for chunk in chunks_to_read:
                remaining = response_limit + 1 - size
                chunks.append(chunk[:remaining])
                size += min(len(chunk), remaining)
                if size > response_limit:
                    break
            return Response(response.status_code, response.headers, b"".join(chunks), size > response_limit)
        finally:
            await response.aclose()


async def _preloaded_chunks(response: httpx.Response) -> AsyncIterator[bytes]:
    yield response.content


def validate_bucket(bucket: str) -> None:
    """Apply the general purpose S3 bucket name rules."""
    if not _BUCKET.fullmatch(bucket) or ".." in bucket:
        raise ValueError("bucket must be a 3 to 63 character S3 bucket name")
    try:
        ipaddress.ip_address(bucket)
    except ValueError:
        return
    raise ValueError("bucket cannot be an IP address")


def validate_key(value: str, field: str = "key", allow_empty: bool = False) -> None:
    """Reject oversized, control, or dot-segment object names."""
    if (not allow_empty and not value) or len(value.encode()) > 1024:
        raise ValueError(f"{field} must be 1 to 1024 bytes")
    if any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value):
        raise ValueError(f"{field} must be UTF-8 text without control characters")
    if not allow_empty and any(segment in (".", "..") for segment in value.split("/")):
        raise ValueError("key cannot contain . or .. path segments")
