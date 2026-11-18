# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Duplicate-aware Google Drive document creation."""

import json
import secrets
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import Call, FailureKind, MutationAttempt, Receipt
from dex_connectors.sdk.providerhttp import parse_retry_after
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    Decision,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.timestamps import canonical_go_timestamp
from .client import (
    CREATED_FIELDS,
    DEFINITE_REFUSAL_STATUSES,
    DRIVE_ID,
    GO_PATH_SAFE,
    GOOGLE_DOCUMENT_MIME,
    Client,
    Credentials,
    Exchange,
    failure,
    go_query,
    google_error_tokens,
    is_terminal_credential_error,
    retryable,
    status_kind,
)
from .document import validate_write_text
from .json_values import strict_json_loads
from .models import CreatedDocument, CreateDocumentInput, CreateDocumentOutput
from .operations import _read_result

# _INVALID_LOOKUP marks a duplicate lookup whose 2xx body could not be decoded.
_INVALID_LOOKUP = "invalidLookup"
# The checkpoint provider ID of a create Drive accepted without a usable document ID; it is never a
# Drive ID, so it only permits lookups, never a resend.
ACCEPTED_CREATE_WITHOUT_DOCUMENT_ID = "dex:acceptedWithoutDocumentId"


@dataclass(frozen=True)
class _CreateSend:
    """The Fence decision and send-cap facts of one createDocument attempt."""

    fence: Fence
    decision: Decision
    # False when this attempt's send would be the last one the cap allows.
    can_resend_unknown_outcome: bool
    # True when a refusal or local failure cannot prove that nothing was created.
    has_possible_earlier_send: bool


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
        canonical_time = canonical_go_timestamp(created_time, "created_time")
        timestamp = datetime.fromisoformat(canonical_time.replace("Z", "+00:00"))
        if timestamp.utcoffset() is None:
            raise ValueError("invalid file timestamp")
    return CreatedDocument(resource["id"], title, tuple(parents), link, timestamp, trashed)


def _lookup_result(body: bytes) -> tuple[CreatedDocument | None, bool]:
    value: Any = strict_json_loads(body)
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
            _INVALID_LOOKUP,
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
    metadata_body = json.dumps(metadata, separators=(",", ":"), ensure_ascii=False, sort_keys=True).encode()
    if not input.initial_text:
        url = (
            client.drive_endpoint
            + "/files?"
            + go_query({"fields": CREATED_FIELDS, "supportsAllDrives": "true"})
        )
        return url, metadata_body, "application/json; charset=UTF-8"
    media_type = "text/markdown; charset=UTF-8" if format_name == "markdown" else "text/plain; charset=UTF-8"
    boundary = secrets.token_hex(30)
    delimiter = ("--" + boundary + "\r\n").encode()
    body = (
        delimiter + b"Content-Type: application/json; charset=UTF-8\r\n\r\n"
        + metadata_body + b"\r\n"
        + delimiter + ("Content-Type: " + media_type + "\r\n\r\n").encode()
        + input.initial_text.encode() + b"\r\n"
        + ("--" + boundary + "--\r\n").encode()
    )
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
    return url, body, "multipart/related; boundary=" + boundary


async def create_document(
    client: Client, call: Call, input: CreateDocumentInput
) -> MutationAttempt[CreateDocumentOutput]:
    """Fence the POST, read back by a private property, and resend an unknown create below the send cap."""
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
        fence = Fence(call, timedelta(seconds=30), max_sends=call.max_sends)
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
    send = _CreateSend(
        fence,
        decision,
        can_resend_unknown_outcome=decision.prior_send_upper_bound + 1 < call.max_sends,
        has_possible_earlier_send=decision.prior_send_upper_bound > 0,
    )
    credential, is_credential_failure_retryable = await client.write_credential(call)
    if credential is None:
        is_first_attempt_send = (
            decision.kind == DecisionKind.SEND_ALLOWED and decision.prior_send_upper_bound == 0
        )
        if is_credential_failure_retryable and is_first_attempt_send:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # The retry remains safe because no provider request was sent.
            return MutationAttempt.retry(
                failure(
                    "createDocument",
                    FailureKind.AUTHENTICATION,
                    "connection credentials are temporarily unavailable",
                )
            )
        # An unreadable checkpoint may hide an earlier send, so even a terminal failure retries it.
        if is_credential_failure_retryable or decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    "createDocument",
                    FailureKind.AUTHENTICATION,
                    "connection credentials are unavailable for duplicate lookup",
                )
            )
        return _local_failure(
            _CreateSend(fence, decision, False, not is_first_attempt_send),
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable; reconnect the connection",
            Receipt(),
        )
    if decision.kind == DecisionKind.FAIL_CLOSED:
        # An unreadable checkpoint never permits a send, but the app property can still confirm a create.
        if found := await _find_after_send(client, call, credential, key):
            return MutationAttempt.branch_result(
                "created",
                CreateDocumentOutput(found, True),
                client.receipt(call, Exchange(), found.document_id),
            )
        return MutationAttempt.retry(
            failure(
                "createDocument",
                FailureKind.AVAILABILITY,
                "dispatch checkpoint is unreadable; no document was created in this attempt",
            )
        )
    # Drive returns a file ID only for a document it created, so a saved one is never resent.
    has_saved_document_id = decision.marker is not None and bool(decision.marker.provider_id)
    is_at_send_cap = decision.kind == DecisionKind.READ_BACK_REQUIRED
    # Only a first send with no saved ID may record notSent before a lookup Retry.
    is_first_send = (
        decision.kind == DecisionKind.SEND_ALLOWED
        and not send.has_possible_earlier_send
        and not has_saved_document_id
    )
    if (
        has_saved_document_id
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
                document = _created(strict_json_loads(direct.body))
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
    has_possible_create = send.has_possible_earlier_send or has_saved_document_id
    if branch != "success":
        if is_first_send and branch == "retry":
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # The provider create was not sent.
            return MutationAttempt.retry(
                failure("createDocument", FailureKind.AVAILABILITY, "duplicate lookup is unavailable")
            )
        if branch == _INVALID_LOOKUP:
            if has_possible_create:
                # A later attempt repeats the read; each attempt counts toward the send cap.
                return _retry_lookup_or_select_uncertain(
                    is_at_send_cap,
                    FailureKind.PROTOCOL,
                    "a prior create cannot be confirmed because duplicate lookup was invalid",
                    receipt or Receipt(),
                )
            return MutationAttempt.branch_result("invalidResponse", empty, receipt or Receipt(), problem)
        if has_possible_create and branch != "retry":
            return _retry_lookup_or_select_uncertain(
                is_at_send_cap,
                problem.kind,
                "a prior create cannot be confirmed because duplicate lookup was rejected",
                receipt or Receipt(),
            )
        if branch == "retry":
            return MutationAttempt.retry(problem, delay)
        return MutationAttempt.branch_result(branch, empty, receipt or Receipt(), problem)
    if earlier:
        return MutationAttempt.branch_result(
            "created", CreateDocumentOutput(earlier, True), client.receipt(call, lookup, earlier.document_id)
        )
    # An incomplete search proves nothing, so it is repeated before any cap decision.
    if incomplete:
        if is_first_send:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # The incomplete lookup happened before the create.
        return MutationAttempt.retry(
            failure(
                "createDocument", FailureKind.AVAILABILITY, "provider did not complete the duplicate lookup"
            )
        )
    if is_at_send_cap or has_saved_document_id:
        # A saved ID proves Drive created the document, so it is looked up again and never resent.
        return _retry_lookup_or_select_uncertain(
            is_at_send_cap,
            FailureKind.TRANSPORT,
            "an earlier attempt of this Step may have created the document, "
            "the duplicate lookup does not show it, and no further send is allowed",
            receipt or Receipt(),
        )
    try:
        url, body, content_type = _create_request(client, input, format_name, key)
    except (TypeError, ValueError):
        return _local_failure(
            send, FailureKind.LOCAL_DEFECT, "create request could not be encoded", Receipt()
        )
    observed_statuses: list[int] = []
    try:
        response = await fence.dispatch(
            lambda _deadline: client.exchange(
                call,
                credential,
                "POST",
                url,
                body,
                content_type,
                single_dispatch=True,
                observed_statuses=observed_statuses,
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
        # The write window may cut the body after 2xx headers; that status proves the create.
        accepted_status = next((status for status in observed_statuses if 200 <= status < 300), 0)
        response = Exchange(status=accepted_status, error="afterSend")
    return await _after_create(client, call, send, credential, key, response)


async def _after_create(
    client: Client, call: Call, send: "_CreateSend", credential: Credentials, key: str, response: Exchange
) -> MutationAttempt[CreateDocumentOutput]:
    fence = send.fence
    receipt = client.receipt(call, response)
    if response.error == "notBuilt":
        return _local_failure(send, FailureKind.LOCAL_DEFECT, "provider request could not be built", receipt)
    tokens = google_error_tokens(response.body)
    if 200 <= response.status < 300 and not response.error:
        try:
            document = _created(strict_json_loads(response.body))
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
            return await _reconcile_accepted(
                client, call, send, credential, key, response, FailureKind.PROTOCOL
            )
    if not response.error and _created_document_id(response.body):
        # A document ID contradicts any other status, so the create is read back and never resent.
        return await _reconcile_accepted(client, call, send, credential, key, response, FailureKind.PROTOCOL)
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
    if response.status == 401 and not response.is_error_body_too_large:
        if not hasattr(client._credentials, "resolve_after_rejection"):
            return await _reject_or_select_uncertain(
                send, FailureKind.AUTHENTICATION, "provider rejected the create credential", receipt
            )
        try:
            await client._credentials.resolve_after_rejection(call)
        except Exception as error:
            if is_terminal_credential_error(error):
                return await _reject_or_select_uncertain(
                    send, FailureKind.AUTHENTICATION, "provider rejected the create credential", receipt
                )
            # Google refuses an unauthenticated create unprocessed, so a transient refresh failure retries.
            try:
                await fence.record_can_resend()
            except Exception:
                pass  # A lost canResend marker makes the next attempt count this send.
            return MutationAttempt.retry(
                failure(
                    "createDocument",
                    FailureKind.AUTHENTICATION,
                    "provider rejected the create credential, and its refresh is temporarily unavailable",
                )
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
    if 200 <= response.status < 300 and response.error:
        kind = FailureKind.RESPONSE_TOO_LARGE if response.error == "tooLarge" else FailureKind.TRANSPORT
        return await _reconcile_accepted(client, call, send, credential, key, response, kind)
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
            send,
            credential,
            key,
            response,
            FailureKind.TRANSPORT,
            "provider create outcome is unknown",
        )
    # An oversized refusal body might hide a document ID, so its outcome is unknown.
    if response.status in DEFINITE_REFUSAL_STATUSES and not response.is_error_body_too_large:
        return await _reject_or_select_uncertain(
            send, status_kind(response.status, tokens), "provider rejected the create", receipt
        )
    return await _reconcile(
        client,
        call,
        send,
        credential,
        key,
        response,
        status_kind(response.status, tokens),
        f"provider create outcome is unknown after HTTP {response.status}",
    )


async def _find_after_send(
    client: Client, call: Call, credential: Credentials, key: str
) -> CreatedDocument | None:
    _, branch, _, _, _, document, incomplete = await _lookup(client, call, credential, key)
    return document if branch == "success" and not incomplete else None


async def _reconcile(
    client: Client,
    call: Call,
    send: "_CreateSend",
    credential: Credentials,
    key: str,
    response: Exchange,
    kind: FailureKind,
    message: str,
) -> MutationAttempt[CreateDocumentOutput]:
    """Look the document up by its app property, then resend below the send cap."""
    if document := await _find_after_send(client, call, credential, key):
        try:
            await send.fence.record_provider_id(document.document_id)
        except Exception:
            pass  # The duplicate lookup confirmed this document.
        return MutationAttempt.branch_result(
            "created", CreateDocumentOutput(document), client.receipt(call, response, document.document_id)
        )
    if send.can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send and looks up again.
        return MutationAttempt.retry(failure("createDocument", kind, message))
    return MutationAttempt.uncertain(
        CreateDocumentOutput(),
        failure("createDocument", kind, message + "; the send cap is reached"),
        client.receipt(call, response),
    )


async def _reconcile_accepted(
    client: Client,
    call: Call,
    send: "_CreateSend",
    credential: Credentials,
    key: str,
    response: Exchange,
    kind: FailureKind,
) -> MutationAttempt[CreateDocumentOutput]:
    """Handle a 2xx create whose body is unusable or unreadable; Drive created it, so it is never resent.

    The lookup confirms it, or a saved file ID makes the next attempt read it back; otherwise
    uncertain is selected.
    """
    if document := await _find_after_send(client, call, credential, key):
        try:
            await send.fence.record_provider_id(document.document_id)
        except Exception:
            pass  # The duplicate lookup confirmed this document.
        return MutationAttempt.branch_result(
            "created", CreateDocumentOutput(document), client.receipt(call, response, document.document_id)
        )
    document_id = _created_document_id(response.body)
    if document_id:
        try:
            await send.fence.record_provider_id(document_id)
            return MutationAttempt.retry(
                failure(
                    "createDocument",
                    kind,
                    "provider accepted the create with an unusable response; "
                    "the next attempt reads the document back",
                )
            )
        except Exception:
            pass  # Without the saved ID, the acceptance marker below still forbids a resend.
    try:
        # The saved acceptance makes later attempts look the document up without sending again, up to the cap.
        await send.fence.record_provider_id(ACCEPTED_CREATE_WITHOUT_DOCUMENT_ID)
        return MutationAttempt.retry(
            failure(
                "createDocument",
                kind,
                "provider accepted the create with an unusable response, and the duplicate lookup "
                "does not show it yet; the next attempt looks it up again",
            )
        )
    except Exception:
        pass  # Without a saved acceptance a later attempt could resend, so select uncertain.
    return MutationAttempt.uncertain(
        CreateDocumentOutput(),
        failure(
            "createDocument",
            kind,
            "provider accepted the create with an unusable response, the duplicate lookup "
            "does not show it, and its acceptance checkpoint failed, so it is not sent again",
        ),
        client.receipt(call, response),
    )


def _created_document_id(body: bytes) -> str:
    """Return the Drive ID a create response names, whatever its other fields hold."""
    try:
        value: Any = strict_json_loads(body)
    except (ValueError, TypeError):
        return ""
    if isinstance(value, dict) and isinstance(value.get("id"), str) and DRIVE_ID.fullmatch(value["id"]):
        return str(value["id"])
    return ""


def _retry_lookup_or_select_uncertain(
    is_at_send_cap: bool, kind: FailureKind, message: str, receipt: Receipt
) -> MutationAttempt[CreateDocumentOutput]:
    """Repeat an inconclusive lookup below the send cap; select uncertain at the cap. It never sends."""
    if is_at_send_cap:
        return MutationAttempt.uncertain(
            CreateDocumentOutput(), failure("createDocument", kind, message), receipt
        )
    return MutationAttempt.retry(
        failure("createDocument", kind, message + "; the next attempt looks it up again")
    )


async def _reject_or_select_uncertain(
    send: "_CreateSend", kind: FailureKind, message: str, receipt: Receipt
) -> MutationAttempt[CreateDocumentOutput]:
    """Select providerRejected only when no earlier send may have created the document."""
    if send.has_possible_earlier_send:
        return MutationAttempt.uncertain(
            CreateDocumentOutput(),
            failure("createDocument", kind, message + " after an earlier unconfirmed send"),
            receipt,
        )
    try:
        await send.fence.record_can_resend()
    except Exception:
        pass  # The refusal ends the Step, so a lost checkpoint cannot cause a resend.
    return MutationAttempt.branch_result(
        "providerRejected", CreateDocumentOutput(), receipt, failure("createDocument", kind, message)
    )


def _local_failure(
    send: "_CreateSend", kind: FailureKind, message: str, receipt: Receipt
) -> MutationAttempt[CreateDocumentOutput]:
    """Select defect only when no earlier send of this call may have created the document."""
    if send.has_possible_earlier_send:
        return MutationAttempt.uncertain(
            CreateDocumentOutput(),
            failure("createDocument", kind, message + " after an earlier unconfirmed send"),
            receipt,
        )
    return MutationAttempt.branch_result(
        "defect", CreateDocumentOutput(), receipt, failure("createDocument", kind, message)
    )
