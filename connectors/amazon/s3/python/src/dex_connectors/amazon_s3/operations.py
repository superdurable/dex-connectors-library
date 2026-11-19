# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""S3 operation Invokes and Go-compatible provider outcome classification."""

from __future__ import annotations

import base64
import binascii
import hashlib
import re
import xml.etree.ElementTree as ET
from dataclasses import replace
from datetime import UTC, datetime
from email.utils import parsedate_to_datetime
from typing import Any
from urllib.parse import unquote_to_bytes

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
from dex_connectors.sdk.providerhttp import parse_retry_after

from ._generated.definitions import (
    GET_OBJECT_TEXT_DEFINITION,
    HEAD_OBJECT_DEFINITION,
    LIST_OBJECTS_DEFINITION,
    PUT_OBJECT_DEFINITION,
)
from .client import Client, Response, Session, validate_key
from .models import (
    GetObjectTextInput,
    HeadObjectInput,
    ListObjectsInput,
    ListObjectsOutput,
    ObjectMetadata,
    ObjectSummary,
    ObjectText,
    PutObjectInput,
    StoredObject,
)


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe S3 failure."""
    return Failure(kind, "amazon-s3", operation, message)


def query_branch(
    branch: str, value: object, receipt: Receipt | None = None, error: Failure | None = None
) -> QueryAttempt[Any]:
    """Construct a definite Query outcome."""
    return QueryAttempt.branch_result(branch, value, receipt or Receipt(), error)


def mutation_branch(
    branch: str, value: object, receipt: Receipt | None = None, error: Failure | None = None
) -> MutationAttempt[Any]:
    """Construct a definite Mutation outcome."""
    return MutationAttempt.branch_result(branch, value, receipt or Receipt(), error)


def _credential_echo(value: str, session: Session, include_id: bool = False) -> bool:
    return any(
        secret and secret in value
        for secret in (
            session.secret_access_key,
            session.session_token,
            session.access_key_id if include_id else "",
        )
    )


def _safe_header(response: Response, name: str, session: Session) -> str:
    value = response.header(name)
    return value if len(value.encode()) <= 2048 and not _credential_echo(value, session) else ""


def _error_code(response: Response, session: Session) -> str:
    if response.is_too_large:
        return ""
    try:
        root = ET.fromstring(response.body)
        _strip_xml_namespaces(root)
        if root.tag != "Error":
            return ""
        code = root.findtext("Code") or ""
    except ET.ParseError:
        return ""
    return (
        code
        if re.fullmatch(r"[A-Za-z0-9.]{1,64}", code) and not _credential_echo(code, session, True)
        else ""
    )


def _strip_xml_namespaces(root: ET.Element) -> None:
    for element in root.iter():
        element.tag = element.tag.rsplit("}", 1)[-1]


_MEDIA_TOKEN = r"[!#$%&'*+.^_`|~0-9A-Za-z-]+"
_MEDIA_TYPE = re.compile(rf"\s*({_MEDIA_TOKEN}/{_MEDIA_TOKEN})")
_MEDIA_PARAMETER = re.compile(
    rf'\s*;\s*({_MEDIA_TOKEN})\s*=\s*({_MEDIA_TOKEN}|"(?:[\x20-\x21\x23-\x5b\x5d-\x7e]|\\[\x20-\x7e])*")'
)


def _parse_media_type(value: str) -> str | None:
    match = _MEDIA_TYPE.match(value)
    if match is None:
        return None
    position = match.end()
    parameters: dict[str, str] = {}
    while value[position:].strip():
        parameter = _MEDIA_PARAMETER.match(value, position)
        if parameter is None:
            return None
        name, parameter_value = parameter.groups()
        name = name.lower()
        if name in parameters and parameters[name] != parameter_value:
            return None
        parameters[name] = parameter_value
        position = parameter.end()
    return match.group(1).lower()


def _classify(
    operation: str, subject: str, response: Response, session: Session, is_object: bool, now: datetime
) -> tuple[str, Failure, float]:
    status = response.status
    code = _error_code(response, session)
    detail = f"HTTP {status}" + (f" {code}" if code else "")
    if status == 429 or code == "SlowDown":
        outcome, kind, message = "retry", FailureKind.RATE_LIMIT, "S3 asked the connector to slow down"
    elif code == "RequestTimeout" or status == 408:
        outcome, kind, message = "retry", FailureKind.TRANSPORT, "S3 timed out the request"
    elif code in ("ConditionalRequestConflict", "OperationAborted"):
        outcome, kind, message = "retry", FailureKind.CONFLICT, "a conflicting S3 operation is in progress"
    elif status == 307 or 500 <= status < 600 and status != 501:
        outcome, kind, message = (
            "retry",
            FailureKind.AVAILABILITY,
            f"S3 is temporarily unable to serve the {subject} with {detail}",
        )
    elif status == 404 and is_object and code != "NoSuchBucket":
        outcome, kind, message = "notFound", FailureKind.NOT_FOUND, f"the {subject} was not found"
    elif status == 404:
        outcome, kind, message = "rejected", FailureKind.NOT_FOUND, f"S3 rejected the {subject} with {detail}"
    elif status == 301 or code in ("PermanentRedirect", "AuthorizationHeaderMalformed"):
        outcome, kind, message = (
            "rejected",
            FailureKind.PROVIDER_REJECTION,
            "S3 bucket needs another Region or endpoint",
        )
    elif code == "RequestTimeTooSkewed":
        outcome, kind, message = "rejected", FailureKind.AUTHENTICATION, "S3 rejected the Worker clock"
    elif (
        code
        in (
            "InvalidAccessKeyId",
            "SignatureDoesNotMatch",
            "ExpiredToken",
            "InvalidToken",
            "TokenRefreshRequired",
            "InvalidSecurity",
        )
        or status == 401
    ):
        outcome, kind, message = (
            "rejected",
            FailureKind.AUTHENTICATION,
            f"S3 rejected connection credentials with {detail}",
        )
    elif status == 403:
        outcome, kind, message = (
            "rejected",
            FailureKind.AUTHORIZATION,
            f"S3 denied the {subject} with {detail}",
        )
    elif status in (409, 412):
        outcome, kind, message = "rejected", FailureKind.CONFLICT, f"S3 rejected the {subject} with {detail}"
    else:
        outcome, kind, message = (
            "rejected",
            FailureKind.PROVIDER_REJECTION,
            f"S3 rejected the {subject} with {detail}",
        )
    delay = parse_retry_after(response.header("retry-after"), now) if outcome == "retry" else 0
    return outcome, failure(operation, kind, message), delay


def _query_failure(
    operation: str,
    subject: str,
    response: Response,
    session: Session,
    receipt: Receipt,
    identity: object,
    is_object: bool,
    now: datetime,
) -> QueryAttempt[Any]:
    outcome, error, delay = _classify(operation, subject, response, session, is_object, now)
    if outcome == "retry":
        return QueryAttempt.retry(error, delay)
    return query_branch("notFound" if outcome == "notFound" else "providerRejected", identity, receipt, error)


def _metadata(response: Response, session: Session, bucket: str, key: str) -> ObjectMetadata | None:
    length = response.header("content-length")
    try:
        size = int(length)
        if size < 0 or not re.fullmatch(r"\d+", length):
            return None
        modified = parsedate_to_datetime(response.header("last-modified")).astimezone(UTC)
    except (ValueError, TypeError, IndexError, AttributeError):
        return None
    metadata_values: dict[str, list[str]] = {}
    for name, value in response.headers.multi_items():
        if name.lower().startswith("x-amz-meta-"):
            metadata_values.setdefault(name.lower()[11:], []).append(value)
    metadata: dict[str, str] = {}
    for metadata_name, values in metadata_values.items():
        joined = ",".join(values)
        if joined and len(joined.encode()) <= 2048 and not _credential_echo(joined, session):
            metadata[metadata_name] = joined
    missing = response.header("x-amz-missing-meta")
    return ObjectMetadata(
        bucket=bucket,
        key=key,
        size_bytes=size,
        e_tag=_safe_header(response, "etag", session),
        content_type=_safe_header(response, "content-type", session),
        content_encoding=_safe_header(response, "content-encoding", session),
        content_language=_safe_header(response, "content-language", session),
        content_disposition=_safe_header(response, "content-disposition", session),
        cache_control=_safe_header(response, "cache-control", session),
        last_modified=modified,
        version_id=_safe_header(response, "x-amz-version-id", session),
        storage_class=_safe_header(response, "x-amz-storage-class", session),
        server_side_encryption=_safe_header(response, "x-amz-server-side-encryption", session),
        metadata=metadata,
        missing_metadata_count=int(missing) if missing.isdigit() and int(missing) > 0 else 0,
    )


def _list_page(body: bytes, bucket: str, prefix: str, delimiter: str) -> ListObjectsOutput:
    try:
        root = ET.fromstring(body)
        _strip_xml_namespaces(root)
        if root.tag != "ListBucketResult":
            raise ValueError("not ListBucketResult")
        encoded = root.findtext("EncodingType") == "url"
        truncated_text = (root.findtext("IsTruncated") or "false").strip()
        if truncated_text not in (
            "true",
            "false",
            "1",
            "0",
            "True",
            "False",
            "TRUE",
            "FALSE",
            "t",
            "f",
            "T",
            "F",
        ):
            raise ValueError("invalid truncated flag")
        truncated = truncated_text in ("true", "1", "True", "TRUE", "t", "T")
        token = root.findtext("NextContinuationToken") or ""
        if (
            truncated
            and not token
            or len(token.encode()) > 4096
            or any(ord(c) < 33 or ord(c) > 126 for c in token)
        ):
            raise ValueError("invalid continuation token")

        def decode(value: str) -> str:
            if not encoded:
                return value
            if re.search(r"%(?![0-9A-Fa-f]{2})", value):
                raise ValueError("invalid percent encoding")
            return unquote_to_bytes(value.replace("+", " ")).decode("utf-8")

        objects = []
        for item in root.findall("Contents"):
            key = decode(item.findtext("Key") or "")
            if not key:
                raise ValueError("missing key")
            size_text = item.findtext("Size") or ""
            if not re.fullmatch(r"\d+", size_text):
                raise ValueError("invalid size")
            modified_text = item.findtext("LastModified") or ""
            modified = (
                datetime.fromisoformat(modified_text.replace("Z", "+00:00"))
                if modified_text
                else datetime.min.replace(tzinfo=UTC)
            )
            objects.append(
                ObjectSummary(
                    key=key,
                    size_bytes=int(size_text),
                    e_tag=item.findtext("ETag") or "",
                    last_modified=modified,
                    storage_class=item.findtext("StorageClass") or "",
                )
            )
        common = tuple(decode(item.findtext("Prefix") or "") for item in root.findall("CommonPrefixes"))
        if any(not value for value in common):
            raise ValueError("invalid prefix")
        return ListObjectsOutput(bucket, prefix, delimiter, tuple(objects), common, truncated, token)
    except (ET.ParseError, UnicodeDecodeError, ValueError, OverflowError) as error:
        raise ValueError("invalid S3 list page") from error


class ListObjectsOperation:
    """List one bounded page with Go-compatible branch selection."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated list operation definition."""
        return LIST_OBJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListObjectsInput) -> QueryAttempt[ListObjectsOutput]:
        """Validate, list, and decode one S3 bucket page."""
        operation = "listObjects"
        zero = ListObjectsOutput()
        try:
            bucket = self.client.bucket(input.bucket)
            max_keys = input.max_keys or self.client.config.list_page_size
            if not 1 <= max_keys <= 1000:
                raise ValueError("maxKeys must be from 1 to 1000")
            for name, value in (
                ("prefix", input.prefix),
                ("delimiter", input.delimiter),
                ("startAfter", input.start_after),
            ):
                validate_key(value, name, True)
            token = input.continuation_token
            if len(token.encode()) > 4096 or any(ord(c) < 33 or ord(c) > 126 for c in token):
                raise ValueError("continuationToken is invalid")
        except (ValueError, UnicodeEncodeError) as error:
            return query_branch("defect", zero, error=failure(operation, FailureKind.VALIDATION, str(error)))
        session = await self.client.start_session(call)
        if session is None:
            return query_branch(
                "defect",
                zero,
                error=failure(operation, FailureKind.AUTHENTICATION, "S3 credentials unavailable"),
            )
        query = {"list-type": "2", "encoding-type": "url", "max-keys": str(max_keys)}
        query.update(
            {
                name: value
                for name, value in (
                    ("prefix", input.prefix),
                    ("delimiter", input.delimiter),
                    ("start-after", input.start_after),
                    ("continuation-token", input.continuation_token),
                )
                if value
            }
        )
        try:
            response = await self.client.exchange(call, session, "GET", bucket, query=query)
        except (httpx.HTTPError, ValueError):
            return QueryAttempt.retry(failure(operation, FailureKind.TRANSPORT, "S3 could not finish list"))
        receipt = self.client.receipt(call, response, bucket)
        identity = ListObjectsOutput(bucket=bucket)
        if not 200 <= response.status < 300:
            return _query_failure(
                operation, "list", response, session, receipt, identity, False, self.client.now()
            )
        if response.is_too_large:
            return query_branch(
                "invalidResponse",
                identity,
                receipt,
                failure(operation, FailureKind.RESPONSE_TOO_LARGE, "S3 list too large"),
            )
        if _credential_echo(response.body.decode("utf-8", "replace"), session):
            return query_branch(
                "invalidResponse",
                identity,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "S3 list contains credentials"),
            )
        try:
            output = _list_page(response.body, bucket, input.prefix, input.delimiter)
        except ValueError:
            return query_branch(
                "invalidResponse",
                identity,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "S3 list invalid"),
            )
        if not output.objects and not output.common_prefixes and not output.is_truncated:
            return query_branch(
                "notFound", output, receipt, failure(operation, FailureKind.NOT_FOUND, "no object matched")
            )
        return query_branch("found", output, receipt)


class HeadObjectOperation:
    """Read object metadata without content."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated metadata operation definition."""
        return HEAD_OBJECT_DEFINITION

    async def invoke(self, call: Call, input: HeadObjectInput) -> QueryAttempt[ObjectMetadata]:
        """Send one HEAD request and decode validated metadata."""
        operation = "headObject"
        try:
            bucket = self.client.bucket(input.bucket)
            validate_key(input.key)
        except (ValueError, UnicodeEncodeError) as error:
            return query_branch(
                "defect", ObjectMetadata(), error=failure(operation, FailureKind.VALIDATION, str(error))
            )
        session = await self.client.start_session(call)
        if session is None:
            return query_branch(
                "defect",
                ObjectMetadata(),
                error=failure(operation, FailureKind.AUTHENTICATION, "S3 credentials unavailable"),
            )
        identity = ObjectMetadata(bucket=bucket, key=input.key)
        try:
            response = await self.client.exchange(call, session, "HEAD", bucket, input.key)
        except (httpx.HTTPError, ValueError):
            return QueryAttempt.retry(
                failure(operation, FailureKind.TRANSPORT, "S3 could not finish object metadata read")
            )
        receipt = self.client.receipt(call, response, bucket, input.key)
        if not 200 <= response.status < 300:
            return _query_failure(
                operation, "object", response, session, receipt, identity, True, self.client.now()
            )
        metadata = _metadata(response, session, bucket, input.key)
        if metadata is None:
            return query_branch(
                "invalidResponse",
                identity,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "S3 metadata invalid"),
            )
        return query_branch("found", metadata, receipt)


class GetObjectTextOperation:
    """Read a bounded UTF-8 object as text."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated object text operation definition."""
        return GET_OBJECT_TEXT_DEFINITION

    async def invoke(self, call: Call, input: GetObjectTextInput) -> QueryAttempt[ObjectText]:
        """Classify object headers before returning text bytes."""
        operation = "getObjectText"
        try:
            bucket = self.client.bucket(input.bucket)
            validate_key(input.key)
        except (ValueError, UnicodeEncodeError) as error:
            return query_branch(
                "defect", ObjectText(), error=failure(operation, FailureKind.VALIDATION, str(error))
            )
        session = await self.client.start_session(call)
        if session is None:
            return query_branch(
                "defect",
                ObjectText(),
                error=failure(operation, FailureKind.AUTHENTICATION, "S3 credentials unavailable"),
            )
        identity = ObjectText(bucket=bucket, key=input.key)
        try:
            response = await self.client.exchange(
                call, session, "GET", bucket, input.key, limit=self.client.config.max_text_bytes
            )
        except (httpx.HTTPError, ValueError):
            return QueryAttempt.retry(
                failure(operation, FailureKind.TRANSPORT, "S3 could not finish object read")
            )
        receipt = self.client.receipt(call, response, bucket, input.key)
        if not 200 <= response.status < 300:
            return _query_failure(
                operation, "object", response, session, receipt, identity, True, self.client.now()
            )
        metadata = _metadata(response, session, bucket, input.key)
        output = ObjectText(
            bucket=bucket,
            key=input.key,
            content_type=metadata.content_type
            if metadata
            else _safe_header(response, "content-type", session),
            e_tag=metadata.e_tag if metadata else _safe_header(response, "etag", session),
            version_id=metadata.version_id if metadata else "",
            last_modified=metadata.last_modified if metadata else datetime.min.replace(tzinfo=UTC),
            size_bytes=metadata.size_bytes if metadata else len(response.body),
        )
        raw_content_type = response.header("content-type").strip()
        content_type = _parse_media_type(raw_content_type) if raw_content_type else ""
        allowed = content_type is not None and (
            not content_type
            or content_type in ("application/octet-stream", "binary/octet-stream")
            or content_type.startswith("text/")
            or content_type
            in (
                "application/json",
                "application/ld+json",
                "application/xml",
                "application/yaml",
                "application/x-yaml",
                "application/x-ndjson",
                "application/javascript",
                "application/sql",
                "application/csv",
                "application/toml",
            )
            or content_type.endswith(("+json", "+xml", "+yaml"))
        )
        if response.header("content-encoding").strip().lower() not in ("", "identity") or not allowed:
            return query_branch(
                "unsupportedContent",
                output,
                receipt,
                failure(operation, FailureKind.VALIDATION, "object is not text"),
            )
        if output.size_bytes > self.client.config.max_text_bytes or response.is_too_large:
            return query_branch(
                "tooLarge",
                output,
                receipt,
                failure(operation, FailureKind.RESPONSE_TOO_LARGE, "object too large"),
            )
        if metadata and metadata.size_bytes != len(response.body):
            return query_branch(
                "invalidResponse",
                output,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "object length differs"),
            )
        try:
            text = response.body.decode("utf-8")
        except UnicodeDecodeError:
            return query_branch(
                "unsupportedContent",
                replace(output, size_bytes=len(response.body)),
                receipt,
                failure(operation, FailureKind.VALIDATION, "invalid UTF-8"),
            )
        output = replace(output, size_bytes=len(response.body))
        if "\x00" in text:
            return query_branch(
                "unsupportedContent",
                output,
                receipt,
                failure(operation, FailureKind.VALIDATION, "invalid text"),
            )
        if _credential_echo(text, session):
            return query_branch(
                "invalidResponse",
                output,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "credential in content"),
            )
        return query_branch("read", replace(output, text=text), receipt)


class PutObjectOperation:
    """Store a repeat-safe object and reconcile a create-only conflict."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated object write definition."""
        return PUT_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PutObjectInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as S3's metadata marker."""
        from dex_connectors.sdk import IdempotencyKey

        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PutObjectInput) -> MutationAttempt[StoredObject]:
        """Send a signed PUT, or identify the writer after a precondition failure."""
        operation = "putObject"
        zero = StoredObject()
        try:
            bucket = self.client.bucket(input.bucket)
            validate_key(input.key)
            if input.text_content and input.byte_content:
                raise ValueError("set at most one of textContent and byteContent")
            content = (
                input.text_content.encode()
                if input.text_content
                else base64.b64decode(input.byte_content, validate=True)
            )
            if len(content) > self.client.config.max_upload_bytes:
                raise ValueError("content exceeds maxUploadBytes")
            if (
                not input.content_type
                or len(input.content_type) > 256
                or not _printable(input.content_type)
                or _parse_media_type(input.content_type) is None
            ):
                raise ValueError("contentType is invalid")
            metadata_size = len("x-amz-meta-dex-idempotency-key") + 128
            for name, value in input.metadata.items():
                if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,127}", name) or name == "dex-idempotency-key":
                    raise ValueError("metadata name is invalid")
                if not value or not _printable(value) or value.strip() != value:
                    raise ValueError("metadata value is invalid")
                metadata_size += len("x-amz-meta-") + len(name) + len(value)
            if metadata_size > 2048:
                raise ValueError("metadata exceeds S3 limit")
            if not re.fullmatch(r"[!-~]{1,128}", str(call.idempotency_key)):
                raise ValueError("idempotency key is missing or not header-safe")
        except (ValueError, binascii.Error, UnicodeEncodeError) as error:
            return mutation_branch(
                "defect", zero, error=failure(operation, FailureKind.VALIDATION, str(error))
            )
        session = await self.client.start_session(call)
        if session is None:
            return mutation_branch(
                "defect",
                zero,
                error=failure(operation, FailureKind.AUTHENTICATION, "S3 credentials unavailable"),
            )
        requested = StoredObject(
            bucket=bucket, key=input.key, size_bytes=len(content), content_type=input.content_type
        )
        headers = {
            "content-type": input.content_type,
            "content-md5": base64.b64encode(hashlib.md5(content).digest()).decode(),
            "x-amz-meta-dex-idempotency-key": str(call.idempotency_key),
        }
        headers.update({"x-amz-meta-" + name: value for name, value in input.metadata.items()})
        if input.is_create_only:
            headers["if-none-match"] = "*"
        try:
            response = await self.client.exchange(
                call, session, "PUT", bucket, input.key, headers=headers, body=content
            )
        except (httpx.HTTPError, ValueError):
            return MutationAttempt.retry(
                failure(operation, FailureKind.TRANSPORT, "S3 could not finish object write")
            )
        receipt = self.client.receipt(call, response, bucket, input.key)
        if 200 <= response.status < 300:
            return mutation_branch(
                "stored",
                replace(
                    requested,
                    e_tag=_safe_header(response, "etag", session),
                    version_id=_safe_header(response, "x-amz-version-id", session),
                ),
                receipt,
            )
        if input.is_create_only and response.status == 412:
            return await self._read_existing(call, session, requested)
        outcome, classified_failure, delay = _classify(
            operation, "object write", response, session, False, self.client.now()
        )
        if outcome == "retry":
            return MutationAttempt.retry(classified_failure, delay)
        return mutation_branch("providerRejected", requested, receipt, classified_failure)

    async def _read_existing(
        self, call: Call, session: Session, requested: StoredObject
    ) -> MutationAttempt[StoredObject]:
        try:
            response = await self.client.exchange(call, session, "HEAD", requested.bucket, requested.key)
        except (httpx.HTTPError, ValueError):
            return MutationAttempt.retry(
                failure("putObject", FailureKind.TRANSPORT, "S3 could not finish metadata read")
            )
        receipt = self.client.receipt(call, response, requested.bucket, requested.key)
        if response.status == 404:
            return MutationAttempt.retry(
                failure("putObject", FailureKind.CONFLICT, "object removed before read")
            )
        if not 200 <= response.status < 300:
            outcome, error, delay = _classify(
                "putObject", "object", response, session, True, self.client.now()
            )
            if outcome == "retry":
                return MutationAttempt.retry(error, delay)
            return mutation_branch("providerRejected", requested, receipt, error)
        metadata = _metadata(response, session, requested.bucket, requested.key)
        if metadata is None:
            return mutation_branch(
                "providerRejected",
                requested,
                receipt,
                failure("putObject", FailureKind.PROTOCOL, "S3 returned invalid metadata"),
            )
        stored = StoredObject(
            bucket=requested.bucket,
            key=requested.key,
            e_tag=metadata.e_tag,
            version_id=metadata.version_id,
            size_bytes=metadata.size_bytes,
            content_type=metadata.content_type,
            last_modified=metadata.last_modified,
        )
        if metadata.metadata.get("dex-idempotency-key") == str(call.idempotency_key):
            return mutation_branch("stored", replace(stored, is_from_earlier_attempt=True), receipt)
        return mutation_branch(
            "alreadyExists",
            stored,
            receipt,
            failure("putObject", FailureKind.CONFLICT, "another object exists"),
        )


def _printable(value: str) -> bool:
    return all(32 <= ord(char) <= 126 for char in value)
