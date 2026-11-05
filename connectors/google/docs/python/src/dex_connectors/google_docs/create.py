# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Duplicate-aware Google Drive document creation."""

import json
from datetime import UTC, datetime, timedelta
from email.message import EmailMessage
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import Call, FailureKind, MutationAttempt, Receipt
from dex_connectors.sdk.providerhttp import parse_retry_after
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from .client import (
    CREATED_FIELDS,
    DRIVE_ID,
    GO_PATH_SAFE,
    GOOGLE_DOCUMENT_MIME,
    Client,
    Credentials,
    Exchange,
    failure,
    go_query,
    google_error_tokens,
    retryable,
    status_kind,
)
from .document import validate_write_text
from .models import CreatedDocument, CreateDocumentInput, CreateDocumentOutput
from .operations import _read_result


def _validate_create(input: CreateDocumentInput, max_text_bytes: int) -> str:
    if (
        not input.title.strip()
        or len(input.title.encode()) > 1024
        or "\n" in input.title
        or "\v" in input.title
    ):
        raise ValueError("title must be 1 to 1024 bytes on one line")
    validate_write_text("title", input.title)
    if input.parent_folder_id and not DRIVE_ID.fullmatch(input.parent_folder_id):
        raise ValueError("parentFolderId must be a Drive folder ID or root")
    format_name = input.initial_text_format or "plainText"
    if format_name not in ("plainText", "markdown"):
        raise ValueError("initialTextFormat must be plainText or markdown")
    if len(input.initial_text.encode()) > max_text_bytes:
        raise ValueError("initialText exceeds the configured text limit")
    validate_write_text("initialText", input.initial_text)
    return format_name


def _created(resource: Any) -> CreatedDocument:
    if (
        not isinstance(resource, dict)
        or not isinstance(resource.get("id"), str)
        or not DRIVE_ID.fullmatch(resource["id"])
    ):
        raise ValueError("file resource is not a Google Doc with a valid ID")
    if resource.get("mimeType") != GOOGLE_DOCUMENT_MIME:
        raise ValueError("file resource is not a Google Doc with a valid ID")
    title, parents, link, created_time, trashed = (
        resource.get("name", ""),
        resource.get("parents") or [],
        resource.get("webViewLink", ""),
        resource.get("createdTime", ""),
        resource.get("trashed", False),
    )
    if (
        not isinstance(title, str)
        or not isinstance(parents, list)
        or any(not isinstance(item, str) for item in parents)
    ):
        raise ValueError("invalid file metadata")
    if not isinstance(link, str) or not isinstance(created_time, str) or not isinstance(trashed, bool):
        raise ValueError("invalid file metadata")
    timestamp = datetime.min.replace(tzinfo=UTC)
    if created_time:
        timestamp = datetime.fromisoformat(created_time.replace("Z", "+00:00"))
        if timestamp.utcoffset() is None:
            raise ValueError("invalid file timestamp")
    return CreatedDocument(resource["id"], title, tuple(parents), link, timestamp, trashed)


def _lookup_result(body: bytes) -> tuple[CreatedDocument | None, bool]:
    value: Any = json.loads(body)
    if (
        not isinstance(value, dict)
        or not isinstance(value.get("files", []), list)
        or not isinstance(value.get("incompleteSearch", False), bool)
    ):
        raise ValueError("invalid duplicate lookup")
    files = [_created(resource) for resource in value.get("files", [])]
    return min(files, key=lambda document: document.created_time) if files else None, value.get(
        "incompleteSearch", False
    )


def _lookup_url(client: Client, key: str) -> str:
    escaped_key = key.replace("\\", "\\\\").replace("'", "\\'")
    query = "appProperties has { key='dexIdempotencyKey' and value='" + escaped_key + "' }"
    return (
        client.drive_endpoint
        + "/files?"
        + go_query(
            {
                "q": query,
                "fields": "incompleteSearch,files(" + CREATED_FIELDS + ")",
                "pageSize": "10",
                "spaces": "drive",
                "corpora": "allDrives",
                "supportsAllDrives": "true",
                "includeItemsFromAllDrives": "true",
            }
        )
    )


async def _lookup(
    client: Client, call: Call, credential: Credentials, key: str
) -> tuple[Exchange, str, Any, Receipt | None, float, CreatedDocument | None, bool]:
    exchange = await client.exchange(call, credential, "GET", _lookup_url(client, key))
    branch, problem, receipt, delay = _read_result(
        client,
        call,
        "createDocument",
        exchange,
        "",
        too_large_branch="invalidResponse",
        not_found_branch="providerRejected",
    )
    if branch != "success":
        return exchange, branch, problem, receipt, delay, None, False
    try:
        document, incomplete = _lookup_result(exchange.body)
    except (ValueError, TypeError, KeyError):
        return (
            exchange,
            "invalidResponse",
            failure(
                "createDocument",
                FailureKind.PROTOCOL,
                "provider returned an invalid duplicate-lookup response",
            ),
            client.receipt(call, exchange),
            0,
            None,
            False,
        )
    return exchange, "success", None, client.receipt(call, exchange), 0, document, incomplete


def _create_request(
    client: Client, input: CreateDocumentInput, format_name: str, key: str
) -> tuple[str, bytes, str]:
    metadata: dict[str, Any] = {
        "name": input.title,
        "mimeType": GOOGLE_DOCUMENT_MIME,
        "appProperties": {"dexIdempotencyKey": key},
    }
    if input.parent_folder_id:
        metadata["parents"] = [input.parent_folder_id]
    metadata_body = json.dumps(metadata, separators=(",", ":"), ensure_ascii=False).encode()
    if not input.initial_text:
        url = (
            client.drive_endpoint
            + "/files?"
            + go_query({"fields": CREATED_FIELDS, "supportsAllDrives": "true"})
        )
        return url, metadata_body, "application/json; charset=UTF-8"
    media_type = "text/markdown; charset=UTF-8" if format_name == "markdown" else "text/plain; charset=UTF-8"
    # A MIME writer gives each invocation a fresh boundary and preserves the two ordered parts.
    message = EmailMessage()
    message.set_type("multipart/related")
    metadata_part = EmailMessage()
    metadata_part["Content-Type"] = "application/json; charset=UTF-8"
    metadata_part.set_payload(metadata_body.decode())
    media_part = EmailMessage()
    media_part["Content-Type"] = media_type
    media_part.set_payload(input.initial_text)
    message.attach(metadata_part)
    message.attach(media_part)
    boundary = message.get_boundary()
    body = message.as_bytes().split(b"\n\n", 1)[-1]
    url = (
        client.drive_upload_endpoint
        + "/files?"
        + go_query(
            {
                "uploadType": "multipart",
                "fields": CREATED_FIELDS,
                "supportsAllDrives": "true",
            }
        )
    )
    return url, body, "multipart/related; boundary=" + str(boundary)


async def create_document(
    client: Client, call: Call, input: CreateDocumentInput
) -> MutationAttempt[CreateDocumentOutput]:
    """Fence the POST, read back by a private property, and never repeat an ambiguous create."""
    empty = CreateDocumentOutput()
    try:
        format_name = _validate_create(input, client.max_text_bytes)
    except ValueError as error:
        return MutationAttempt.branch_result(
            "defect", empty, Receipt(), failure("createDocument", FailureKind.VALIDATION, str(error))
        )
    key = str(call.idempotency_key)
    if not key or len(key.encode()) > 100:
        return MutationAttempt.branch_result(
            "defect",
            empty,
            Receipt(),
            failure("createDocument", FailureKind.LOCAL_DEFECT, "idempotency key is missing or too long"),
        )
    try:
        fence = Fence(call, timedelta(seconds=30))
        decision = await fence.decide()
    except ValueError:
        return MutationAttempt.branch_result(
            "defect",
            empty,
            Receipt(),
            failure(
                "createDocument", FailureKind.LOCAL_DEFECT, "single-dispatch fence could not be constructed"
            ),
        )
    credential = await client.credential(call)
    if credential is None:
        if decision.kind == DecisionKind.SEND_ALLOWED:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # The retry remains safe because no provider request was sent.
            return MutationAttempt.retry(
                failure(
                    "createDocument", FailureKind.AUTHENTICATION, "connection credentials are unavailable"
                )
            )
        return MutationAttempt.retry(
            failure(
                "createDocument",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable for duplicate lookup",
            )
        )
    if (
        decision.kind == DecisionKind.READ_BACK_REQUIRED
        and decision.marker
        and decision.marker.provider_id
        and DRIVE_ID.fullmatch(decision.marker.provider_id)
    ):
        provider_id = decision.marker.provider_id
        direct_url = (
            client.drive_endpoint
            + "/files/"
            + quote(provider_id, safe=GO_PATH_SAFE)
            + "?"
            + go_query({"fields": CREATED_FIELDS, "supportsAllDrives": "true"})
        )
        direct = await client.exchange(call, credential, "GET", direct_url)
        branch, problem, _, delay = _read_result(
            client,
            call,
            "createDocument",
            direct,
            "",
            too_large_branch="invalidResponse",
            not_found_branch="providerRejected",
        )
        if branch == "retry":
            return MutationAttempt.retry(problem, delay)
        if branch == "success":
            try:
                document = _created(json.loads(direct.body))
                if document.document_id == provider_id:
                    return MutationAttempt.branch_result(
                        "created",
                        CreateDocumentOutput(document, True),
                        client.receipt(call, direct, document.document_id),
                    )
            except (ValueError, TypeError):
                pass
    lookup, branch, problem, receipt, delay, earlier, incomplete = await _lookup(
        client, call, credential, key
    )
    if branch != "success":
        if decision.kind == DecisionKind.SEND_ALLOWED and branch == "retry":
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # The provider create was not sent.
            return MutationAttempt.retry(
                failure("createDocument", FailureKind.AVAILABILITY, "duplicate lookup is unavailable")
            )
        if decision.kind != DecisionKind.SEND_ALLOWED and branch != "retry":
            return MutationAttempt.uncertain(
                empty,
                failure(
                    "createDocument",
                    problem.kind,
                    "a prior create cannot be confirmed because duplicate lookup was rejected",
                ),
                receipt or Receipt(),
            )
        if branch == "retry":
            return MutationAttempt.retry(problem, delay)
        return MutationAttempt.branch_result(branch, empty, receipt or Receipt(), problem)
    if earlier:
        return MutationAttempt.branch_result(
            "created", CreateDocumentOutput(earlier, True), client.receipt(call, lookup, earlier.document_id)
        )
    if incomplete:
        if decision.kind == DecisionKind.SEND_ALLOWED:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # The incomplete lookup happened before the create.
        return MutationAttempt.retry(
            failure(
                "createDocument", FailureKind.AVAILABILITY, "provider did not complete the duplicate lookup"
            )
        )
    if decision.kind != DecisionKind.SEND_ALLOWED:
        return MutationAttempt.uncertain(
            empty,
            failure(
                "createDocument",
                FailureKind.TRANSPORT,
                "an earlier attempt of this Step may have created the document, "
                "and the duplicate lookup does not show it yet",
            ),
            receipt or Receipt(),
        )
    try:
        url, body, content_type = _create_request(client, input, format_name, key)
    except (TypeError, ValueError):
        return MutationAttempt.branch_result(
            "defect",
            empty,
            Receipt(),
            failure("createDocument", FailureKind.LOCAL_DEFECT, "create request could not be encoded"),
        )
    try:
        response = await fence.dispatch(
            lambda _deadline: client.exchange(
                call, credential, "POST", url, body, content_type, single_dispatch=True
            )
        )
    except (CheckpointError, InsufficientAttemptTimeError):
        return MutationAttempt.retry(
            failure(
                "createDocument",
                FailureKind.AVAILABILITY,
                "Dex could not record a dispatch claim or the attempt has too little time",
            )
        )
    except Exception:
        response = Exchange(error="afterSend")
    return await _after_create(client, call, fence, credential, key, response)


async def _after_create(
    client: Client, call: Call, fence: Fence, credential: Credentials, key: str, response: Exchange
) -> MutationAttempt[CreateDocumentOutput]:
    empty = CreateDocumentOutput()
    receipt = client.receipt(call, response)
    if response.error == "notBuilt":
        return MutationAttempt.branch_result(
            "defect",
            empty,
            receipt,
            failure("createDocument", FailureKind.LOCAL_DEFECT, "provider request could not be built"),
        )
    tokens = google_error_tokens(response.body)
    if 200 <= response.status < 300 and not response.error:
        try:
            document = _created(json.loads(response.body))
            try:
                await fence.record_provider_id(document.document_id)
            except Exception:
                pass  # The accepted provider response is already conclusive.
            return MutationAttempt.branch_result(
                "created",
                CreateDocumentOutput(document),
                client.receipt(call, response, document.document_id),
            )
        except (ValueError, TypeError):
            return await _reconcile(
                client,
                call,
                fence,
                credential,
                key,
                response,
                FailureKind.PROTOCOL,
                "provider returned an unusable accepted create response",
            )
    if response.status == 429 or response.status == 403 and retryable(403, tokens) or response.status == 503:
        if found_document := await _find_after_send(client, call, credential, key):
            try:
                await fence.record_provider_id(found_document.document_id)
            except Exception:
                pass  # The duplicate lookup confirmed this document.
            return MutationAttempt.branch_result(
                "created",
                CreateDocumentOutput(found_document),
                client.receipt(call, response, found_document.document_id),
            )
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure(
                    "createDocument",
                    FailureKind.AVAILABILITY,
                    "Dex could not record safe resend after provider rejection",
                )
            )
        return MutationAttempt.retry(
            failure(
                "createDocument",
                status_kind(response.status, tokens),
                "provider temporarily rejected the create",
            ),
            parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC)),
        )
    if response.status == 401:
        try:
            await getattr(client._credentials, "resolve_after_rejection")(call)
        except Exception:
            return MutationAttempt.branch_result(
                "providerRejected",
                empty,
                receipt,
                failure(
                    "createDocument", FailureKind.AUTHENTICATION, "provider rejected the create credential"
                ),
            )
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure(
                    "createDocument",
                    FailureKind.AUTHENTICATION,
                    "Dex could not record safe resend after credential refresh",
                )
            )
        return MutationAttempt.retry(
            failure(
                "createDocument",
                FailureKind.AUTHENTICATION,
                "provider rejected the create credential; refreshed for the next attempt",
            )
        )
    if response.error == "beforeSend":
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure("createDocument", FailureKind.TRANSPORT, "Dex could not record a proven no-send")
            )
        return MutationAttempt.retry(
            failure(
                "createDocument",
                FailureKind.TRANSPORT,
                "provider connection failed before the create was sent",
            )
        )
    if response.error:
        return await _reconcile(
            client,
            call,
            fence,
            credential,
            key,
            response,
            FailureKind.TRANSPORT,
            "provider create outcome is unknown",
        )
    if response.status >= 500 or response.status in (408, 409):
        return await _reconcile(
            client,
            call,
            fence,
            credential,
            key,
            response,
            FailureKind.AVAILABILITY,
            "provider create outcome is unknown",
        )
    return MutationAttempt.branch_result(
        "providerRejected",
        empty,
        receipt,
        failure("createDocument", status_kind(response.status, tokens), "provider rejected the create"),
    )


async def _find_after_send(
    client: Client, call: Call, credential: Credentials, key: str
) -> CreatedDocument | None:
    _, branch, _, _, _, document, incomplete = await _lookup(client, call, credential, key)
    return document if branch == "success" and not incomplete else None


async def _reconcile(
    client: Client,
    call: Call,
    fence: Fence,
    credential: Credentials,
    key: str,
    response: Exchange,
    kind: FailureKind,
    message: str,
) -> MutationAttempt[CreateDocumentOutput]:
    if document := await _find_after_send(client, call, credential, key):
        try:
            await fence.record_provider_id(document.document_id)
        except Exception:
            pass  # The duplicate lookup confirmed this document.
        return MutationAttempt.branch_result(
            "created", CreateDocumentOutput(document), client.receipt(call, response, document.document_id)
        )
    return MutationAttempt.uncertain(
        CreateDocumentOutput(), failure("createDocument", kind, message), client.receipt(call, response)
    )
