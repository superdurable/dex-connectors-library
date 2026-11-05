# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Five DocuSign operation attempts and Go-compatible branch decisions."""

import asyncio
import hashlib
import json
import re
from dataclasses import dataclass
from dataclasses import field as dataclass_field
from datetime import UTC, datetime, timedelta
from typing import Any, Literal, TypeVar, overload

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
from dex_connectors.sdk.providerhttp import BodyTooLargeError, ProviderTransportError

from ._generated import definitions as definitions
from .client import (
    Client,
    ProviderResponse,
    Session,
    SessionFailure,
    _strict_object,
    _string,
    failure,
    is_guid,
)
from .models import (
    CombinedDocument,
    CreatedEnvelope,
    CreateEnvelopeFromTemplateInput,
    DownloadCombinedDocumentInput,
    Envelope,
    EnvelopeCustomField,
    EnvelopeRecipients,
    GetEnvelopeInput,
    ListEnvelopeRecipientsInput,
    Recipient,
    VoidedEnvelope,
    VoidEnvelopeInput,
)

_TIMESTAMP = re.compile(r"(\d{4}-\d\d-\d\d)T(\d\d:\d\d:\d\d)(?:[.,](\d+))?(Z|[+-]\d\d:\d\d)\Z")
_STATUS = re.compile(r"[a-z]{1,32}\Z")
Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class _CreateDispatchCheckpoint:
    is_dispatched: bool = dataclass_field(metadata={"dex_json_name": "isDispatched"})
    dispatched_at: str = dataclass_field(metadata={"dex_json_name": "dispatchedAt"})


def _go_timestamp(value: datetime) -> str:
    return value.astimezone(UTC).isoformat().replace("+00:00", "Z")


def _time(value: str) -> datetime | None:
    if not value:
        return None
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise ValueError("DocuSign returned a malformed response")
    date_part, clock_part, fraction, offset = match.groups()
    try:
        base = datetime.fromisoformat(date_part + "T" + clock_part).replace(tzinfo=UTC)
        if fraction:
            base += timedelta(microseconds=int((fraction + "000000")[:6]))
        if offset != "Z":
            hours, minutes = int(offset[1:3]), int(offset[4:6])
            if minutes > 59:
                raise ValueError("invalid timezone minute")
            delta = timedelta(hours=hours, minutes=minutes)
            base += -delta if offset[0] == "+" else delta
        return base
    except (ValueError, OverflowError) as error:
        raise ValueError("DocuSign returned a malformed response") from error


def _bounded_text(value: object, minimum: int, maximum: int) -> bool:
    if not isinstance(value, str) or not minimum <= len(value) <= maximum:
        return False
    try:
        value.encode("utf-8")
    except UnicodeEncodeError:
        return False
    return True


def _valid_routing_order(value: str) -> bool:
    """Match Go strconv.Atoi and reject negative or overflowing orders."""
    if not re.fullmatch(r"[+-]?[0-9]+", value):
        return False
    number = int(value)
    return 0 <= number <= 2**63 - 1


def _decode_envelope(body: bytes) -> Envelope:
    record = _strict_object(body)
    envelope_id, status = _string(record, "envelopeId"), _string(record, "status").lower()
    if not is_guid(envelope_id) or not _STATUS.fullmatch(status):
        raise ValueError("DocuSign returned a malformed response")
    fields = record.get("customFields")
    custom_fields: list[EnvelopeCustomField] = []
    if fields is not None:
        if not isinstance(fields, dict):
            raise ValueError("DocuSign returned a malformed response")
        text_fields = fields.get("textCustomFields")
        if text_fields is None:
            text_fields = []
        if not isinstance(text_fields, list):
            raise ValueError("DocuSign returned a malformed response")
        for field in text_fields:
            if not isinstance(field, dict):
                raise ValueError("DocuSign returned a malformed response")
            name, value = _string(field, "name"), _string(field, "value")
            if name:
                custom_fields.append(EnvelopeCustomField(name, value))
    names = {
        "created_at": "createdDateTime",
        "sent_at": "sentDateTime",
        "delivered_at": "deliveredDateTime",
        "completed_at": "completedDateTime",
        "declined_at": "declinedDateTime",
        "voided_at": "voidedDateTime",
        "status_changed_at": "statusChangedDateTime",
        "expires_at": "expireDateTime",
    }
    times = {name: _time(_string(record, provider_name)) for name, provider_name in names.items()}
    return Envelope(
        envelope_id=envelope_id.lower(),
        status=status,
        email_subject=_string(record, "emailSubject"),
        voided_reason=_string(record, "voidedReason"),
        custom_fields=tuple(custom_fields),
        **times,
    )


def _decode_created(body: bytes, is_draft: bool) -> CreatedEnvelope:
    record = _strict_object(body)
    envelope_id = _string(record, "envelopeId")
    status = _string(record, "status").lower()
    if not is_guid(envelope_id) or status != ("created" if is_draft else "sent"):
        raise ValueError("DocuSign returned a malformed response")
    return CreatedEnvelope(
        envelope_id=envelope_id.lower(),
        status=status,
        status_changed_at=_time(_string(record, "statusDateTime")),
    )


def _decode_recipients(body: bytes, envelope_id: str) -> EnvelopeRecipients:
    record = _strict_object(body)
    routing_order = _string(record, "currentRoutingOrder")
    if routing_order and not _valid_routing_order(routing_order):
        raise ValueError("DocuSign returned a malformed response")
    recipients: list[Recipient] = []
    collections = (
        ("signers", "signer"),
        ("carbonCopies", "carbonCopy"),
        ("certifiedDeliveries", "certifiedDelivery"),
        ("inPersonSigners", "inPersonSigner"),
        ("agents", "agent"),
        ("editors", "editor"),
        ("intermediaries", "intermediary"),
        ("witnesses", "witness"),
    )
    for collection, recipient_type in collections:
        entries = record.get(collection)
        if entries is None:
            entries = []
        if not isinstance(entries, list):
            raise ValueError("DocuSign returned a malformed response")
        for entry in entries:
            if not isinstance(entry, dict):
                raise ValueError("DocuSign returned a malformed response")
            identifier = _string(entry, "recipientId")
            status = _string(entry, "status").lower()
            order = _string(entry, "routingOrder")
            if not identifier or not status or (order and not _valid_routing_order(order)):
                raise ValueError("DocuSign returned a malformed response")
            recipients.append(
                Recipient(
                    recipient_id=identifier,
                    type=recipient_type,
                    name=_string(entry, "name"),
                    email=_string(entry, "email"),
                    role_name=_string(entry, "roleName"),
                    routing_order=int(order or "0"),
                    status=status,
                    sent_at=_time(_string(entry, "sentDateTime")),
                    delivered_at=_time(_string(entry, "deliveredDateTime")),
                    signed_at=_time(_string(entry, "signedDateTime")),
                    declined_at=_time(_string(entry, "declinedDateTime")),
                    declined_reason=_string(entry, "declinedReason"),
                )
            )

    def sort_key(recipient: Recipient) -> tuple[int, int, int | str]:
        try:
            return (recipient.routing_order, 0, int(recipient.recipient_id))
        except ValueError:
            return (recipient.routing_order, 1, recipient.recipient_id)

    recipients.sort(key=sort_key)
    return EnvelopeRecipients(
        envelope_id=envelope_id, current_routing_order=int(routing_order or "0"), recipients=tuple(recipients)
    )


@overload
def _session_attempt(
    error: SessionFailure, zero: Output, mutation: Literal[False] = False
) -> QueryAttempt[Output]: ...


@overload
def _session_attempt(
    error: SessionFailure, zero: Output, mutation: Literal[True]
) -> MutationAttempt[Output]: ...


def _session_attempt(error: SessionFailure, zero: Output, mutation: bool = False) -> Any:
    kind = MutationAttempt if mutation else QueryAttempt
    if error.category == "retry":
        return kind.retry(error.failure, error.retry_after)
    branch = (
        "providerRejected"
        if error.category == "rejected"
        else "invalidResponse"
        if error.category == "invalidResponse"
        else "defect"
    )
    return kind.branch_result(branch, zero, Receipt(), error.failure)


@overload
def _response_attempt(
    client: Client,
    operation: str,
    response: ProviderResponse,
    zero: Output,
    envelope_id: str,
    mutation: Literal[False] = False,
) -> QueryAttempt[Output]: ...


@overload
def _response_attempt(
    client: Client,
    operation: str,
    response: ProviderResponse,
    zero: Output,
    envelope_id: str,
    mutation: Literal[True],
) -> MutationAttempt[Output]: ...


def _response_attempt(
    client: Client,
    operation: str,
    response: ProviderResponse,
    zero: Output,
    envelope_id: str,
    mutation: bool = False,
) -> Any:
    outcome = client.classify(operation, response)
    kind = MutationAttempt if mutation else QueryAttempt
    if outcome.retry:
        return kind.retry(outcome.failure, outcome.retry_after)
    branch = "notFound" if outcome.not_found else "providerRejected"
    return kind.branch_result(branch, zero, client.receipt(envelope_id, outcome.error_code), outcome.failure)


@overload
def _read_error(
    client: Client,
    operation: str,
    error: Exception,
    zero: Output,
    envelope_id: str,
    mutation: Literal[False] = False,
) -> QueryAttempt[Output]: ...


@overload
def _read_error(
    client: Client, operation: str, error: Exception, zero: Output, envelope_id: str, mutation: Literal[True]
) -> MutationAttempt[Output]: ...


def _read_error(
    client: Client,
    operation: str,
    error: Exception,
    zero: Output,
    envelope_id: str,
    mutation: bool = False,
) -> Any:
    kind = MutationAttempt if mutation else QueryAttempt
    if isinstance(error, BodyTooLargeError):
        reason = failure(
            operation,
            FailureKind.RESPONSE_TOO_LARGE,
            "DocuSign response exceeds the configured maxResponseBytes",
        )
        return kind.branch_result("invalidResponse", zero, client.receipt(envelope_id), reason)
    return kind.retry(failure(operation, FailureKind.TRANSPORT, "DocuSign could not be reached"))


class GetEnvelopeOperation:
    """Read one envelope and its custom fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return definitions.GET_ENVELOPE_DEFINITION

    async def invoke(self, call: Call, input: GetEnvelopeInput) -> QueryAttempt[Envelope]:
        """Map status, body, and transport outcomes to declared branches."""
        operation, zero = "getEnvelope", Envelope(status="")
        if not is_guid(input.envelope_id):
            return QueryAttempt.branch_result(
                "defect",
                zero,
                __import__("dex_connectors.sdk", fromlist=["Receipt"]).Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "envelopeId must be a DocuSign envelope ID GUID, "
                    "such as 00000000-0000-0000-0000-000000000000",
                ),
            )
        try:
            session = await self.client.open_session(call, operation)
        except SessionFailure as error:
            return _session_attempt(error, zero)
        try:
            response = await self.client.request(
                call, session, "GET", "/envelopes/" + input.envelope_id, {"include": "custom_fields"}
            )
        except (BodyTooLargeError, ProviderTransportError) as error:
            return _read_error(self.client, operation, error, zero, input.envelope_id)
        if response.status != 200:
            return _response_attempt(self.client, operation, response, zero, input.envelope_id)
        try:
            envelope = _decode_envelope(response.body)
            if envelope.envelope_id != input.envelope_id.lower():
                raise ValueError("DocuSign returned a malformed response")
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(input.envelope_id),
                failure(operation, FailureKind.PROTOCOL, "DocuSign returned a malformed response"),
            )
        return QueryAttempt.branch_result("found", envelope, self.client.receipt(envelope.envelope_id))


class ListEnvelopeRecipientsOperation:
    """Flatten all DocuSign recipient collections in routing order."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return definitions.LIST_ENVELOPE_RECIPIENTS_DEFINITION

    async def invoke(
        self, call: Call, input: ListEnvelopeRecipientsInput
    ) -> QueryAttempt[EnvelopeRecipients]:
        """Return recipients or the provider refusal."""
        operation, zero = "listEnvelopeRecipients", EnvelopeRecipients()
        if not is_guid(input.envelope_id):
            from dex_connectors.sdk import Receipt

            return QueryAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "envelopeId must be a DocuSign envelope ID GUID, "
                    "such as 00000000-0000-0000-0000-000000000000",
                ),
            )
        try:
            session = await self.client.open_session(call, operation)
        except SessionFailure as error:
            return _session_attempt(error, zero)
        try:
            response = await self.client.request(
                call, session, "GET", "/envelopes/" + input.envelope_id + "/recipients"
            )
        except (BodyTooLargeError, ProviderTransportError) as error:
            return _read_error(self.client, operation, error, zero, input.envelope_id)
        if response.status != 200:
            return _response_attempt(self.client, operation, response, zero, input.envelope_id)
        try:
            result = _decode_recipients(response.body, input.envelope_id.lower())
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(input.envelope_id.lower()),
                failure(operation, FailureKind.PROTOCOL, "DocuSign returned a malformed response"),
            )
        return QueryAttempt.branch_result("listed", result, self.client.receipt(result.envelope_id))


class VoidEnvelopeOperation:
    """Repeatably void an envelope and read back state refusals."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return definitions.VOID_ENVELOPE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: VoidEnvelopeInput) -> IdempotencyKey:
        """Use the stable Call ID because DocuSign accepts no idempotency header."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: VoidEnvelopeInput) -> MutationAttempt[VoidedEnvelope]:
        """Send the void request, then classify a status refusal by read-back."""
        from dex_connectors.sdk import Receipt

        operation, zero = "voidEnvelope", VoidedEnvelope()
        if (
            not is_guid(input.envelope_id)
            or not _bounded_text(input.voided_reason, 1, 200)
            or not input.voided_reason.strip()
        ):
            return MutationAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                failure(
                    operation, FailureKind.VALIDATION, "voidedReason must be 1 to 200 characters of text"
                ),
            )
        envelope_id = input.envelope_id.lower()
        try:
            session = await self.client.open_session(call, operation)
        except SessionFailure as error:
            return _session_attempt(error, zero, True)
        try:
            response = await self.client.request(
                call,
                session,
                "PUT",
                "/envelopes/" + envelope_id,
                body={"status": "voided", "voidedReason": input.voided_reason},
            )
        except BodyTooLargeError:
            return MutationAttempt.branch_result(
                "voided", VoidedEnvelope(envelope_id=envelope_id), self.client.receipt(envelope_id)
            )
        except ProviderTransportError:
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "DocuSign could not be reached to void the envelope; voiding again is safe",
                )
            )
        if response.status == 200:
            return MutationAttempt.branch_result(
                "voided", VoidedEnvelope(envelope_id=envelope_id), self.client.receipt(envelope_id)
            )
        outcome = self.client.classify(operation, response)
        if outcome.retry:
            return MutationAttempt.retry(outcome.failure, outcome.retry_after)
        receipt = self.client.receipt(envelope_id, outcome.error_code)
        if outcome.not_found:
            return MutationAttempt.branch_result("notFound", zero, receipt, outcome.failure)
        if outcome.error_code.upper() not in (
            "ENVELOPE_CANNOT_VOID_INVALID_STATE",
            "ENVELOPE_INVALID_STATUS",
        ):
            return MutationAttempt.branch_result("providerRejected", zero, receipt, outcome.failure)
        try:
            read = await self.client.request(
                call, session, "GET", "/envelopes/" + envelope_id, {"include": "custom_fields"}
            )
            if read.status != 200:
                return _response_attempt(self.client, operation, read, zero, envelope_id, True)
            envelope = _decode_envelope(read.body)
            if envelope.envelope_id != envelope_id:
                raise ValueError("DocuSign returned a malformed response")
        except (BodyTooLargeError, ProviderTransportError) as error:
            return _read_error(self.client, operation, error, zero, envelope_id, True)
        except ValueError:
            return MutationAttempt.branch_result(
                "invalidResponse",
                zero,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "DocuSign returned a malformed response"),
            )
        if envelope.status == "voided":
            return MutationAttempt.branch_result(
                "voided",
                VoidedEnvelope(
                    envelope_id=envelope_id,
                    was_already_voided=True,
                    status="voided",
                    voided_reason=envelope.voided_reason,
                ),
                receipt,
            )
        if envelope.status in ("created", "completed", "declined"):
            return MutationAttempt.branch_result(
                "notVoidable",
                VoidedEnvelope(envelope_id=envelope_id, status=envelope.status),
                receipt,
                failure(
                    operation,
                    FailureKind.CONFLICT,
                    f"DocuSign does not void an envelope whose status is {envelope.status}",
                ),
            )
        return MutationAttempt.branch_result(
            "providerRejected", VoidedEnvelope(status=envelope.status), receipt, outcome.failure
        )


class DownloadCombinedDocumentOperation:
    """Hash a bounded combined PDF without placing its bytes in Dex state."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return definitions.DOWNLOAD_COMBINED_DOCUMENT_DEFINITION

    async def invoke(
        self, call: Call, input: DownloadCombinedDocumentInput
    ) -> QueryAttempt[CombinedDocument]:
        """Download a PDF, optionally store it, and return only its digest."""
        from dex_connectors.sdk import Receipt

        operation, zero = "downloadCombinedDocument", CombinedDocument()
        if not is_guid(input.envelope_id):
            return QueryAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "envelopeId must be a DocuSign envelope ID GUID, "
                    "such as 00000000-0000-0000-0000-000000000000",
                ),
            )
        envelope_id = input.envelope_id.lower()
        try:
            session = await self.client.open_session(call, operation)
        except SessionFailure as error:
            return _session_attempt(error, zero)
        stop_heartbeat = asyncio.Event()
        heartbeat_task = asyncio.create_task(_keep_document_heartbeat(call, stop_heartbeat))
        try:
            return await self._download(call, input, session, operation, zero, envelope_id)
        finally:
            stop_heartbeat.set()
            await heartbeat_task

    async def _download(
        self,
        call: Call,
        input: DownloadCombinedDocumentInput,
        session: Session,
        operation: str,
        zero: CombinedDocument,
        envelope_id: str,
    ) -> QueryAttempt[CombinedDocument]:
        try:
            response = await self.client.request(
                call,
                session,
                "GET",
                "/envelopes/" + envelope_id + "/documents/combined",
                {"certificate": str(input.include_certificate).lower()},
                accept="application/pdf",
                maximum_bytes=self.client.config.max_document_bytes + 1,
                timeout=110,
            )
        except BodyTooLargeError:
            return QueryAttempt.branch_result(
                "tooLarge",
                zero,
                self.client.receipt(envelope_id),
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "the combined PDF exceeds the configured maxDocumentBytes",
                ),
            )
        except ProviderTransportError:
            return QueryAttempt.retry(
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "DocuSign could not be reached to download the combined PDF",
                )
            )
        if not 200 <= response.status < 300:
            return _response_attempt(self.client, operation, response, zero, envelope_id)
        content_type = response.headers.get("Content-Type", "").split(";", 1)[0].strip().lower()
        if content_type != "application/pdf" or not response.body.startswith(b"%PDF-"):
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(envelope_id),
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "DocuSign returned something other than a PDF for the combined document",
                ),
            )
        if len(response.body) > self.client.config.max_document_bytes:
            return QueryAttempt.branch_result(
                "tooLarge",
                zero,
                self.client.receipt(envelope_id),
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "the combined PDF exceeds the configured maxDocumentBytes",
                ),
            )
        location = ""
        if self.client.document_store is not None:
            try:
                location = await self.client.document_store.store_combined_document(
                    session.account_id, envelope_id, input.include_certificate, response.body
                )
                if not isinstance(location, str) or len(location.encode("utf-8")) > 1024:
                    raise ValueError("invalid stored location")
            except Exception:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    zero,
                    self.client.receipt(envelope_id),
                    failure(
                        operation,
                        FailureKind.LOCAL_DEFECT,
                        "the CombinedDocumentStore did not store the combined PDF",
                    ),
                )
        document = CombinedDocument(
            envelope_id=envelope_id,
            content_type="application/pdf",
            byte_count=len(response.body),
            sha256=hashlib.sha256(response.body).hexdigest(),
            includes_certificate=input.include_certificate,
            stored_location=location,
        )
        return QueryAttempt.branch_result("downloaded", document, self.client.receipt(envelope_id))


async def _keep_document_heartbeat(call: Call, stop: asyncio.Event) -> None:
    while not stop.is_set():
        try:
            await asyncio.wait_for(stop.wait(), timeout=5)
        except TimeoutError:
            if call.context is not None:
                try:
                    await call.context.heartbeat()
                except Exception:
                    pass  # A missed liveness heartbeat is repeated on the next interval.


class CreateEnvelopeFromTemplateOperation:
    """Create one marked envelope, reconciling every ambiguous send."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return definitions.CREATE_ENVELOPE_FROM_TEMPLATE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateEnvelopeFromTemplateInput) -> IdempotencyKey:
        """Tag the envelope with the stable Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: CreateEnvelopeFromTemplateInput
    ) -> MutationAttempt[CreatedEnvelope]:
        """Send once after a checkpoint and read back uncertain outcomes."""
        from dex_connectors.sdk import Receipt

        operation, zero = "createEnvelopeFromTemplate", CreatedEnvelope(status="")
        marker_value = str(call.idempotency_key or call.id)
        try:
            body = _create_body(input, marker_value)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        context = call.context
        prior = False
        checkpoint = None
        if context is not None:
            try:
                if context.has_last_heartbeat_value():
                    checkpoint = context.get_last_heartbeat_value(_CreateDispatchCheckpoint)
                    prior = checkpoint is None or checkpoint.is_dispatched
            except Exception:
                prior = True
        try:
            session = await self.client.open_session(call, operation)
        except SessionFailure as error:
            if prior and context is not None:
                fallback_checkpoint = _CreateDispatchCheckpoint(True, _go_timestamp(context.first_attempt_at))
                await _record_checkpoint(
                    context, checkpoint or fallback_checkpoint
                )
            if prior and error.category != "retry":
                return MutationAttempt.uncertain(zero, error.failure, self.client.receipt())
            return _session_attempt(error, zero, True)
        if prior or (context is not None and context.attempt > 1):
            recovered = await self._read_back(call, session, marker_value, "prior" if prior else "before")
            if recovered is not None:
                return recovered
            if prior:
                return MutationAttempt.uncertain(
                    zero,
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        "an earlier attempt sent the DocuSign create request and its outcome is unknown",
                    ),
                    self.client.receipt(),
                )
        if context is None:
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the create checkpoint could not be recorded, so nothing was sent to DocuSign",
                )
            )
        try:
            await context.heartbeat(_CreateDispatchCheckpoint(True, _go_timestamp(self.client.now())))
        except Exception:
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the create checkpoint could not be recorded, so nothing was sent to DocuSign",
                )
            )
        try:
            response = await self.client.request(call, session, "POST", "/envelopes", body=body, timeout=40)
        except ProviderTransportError as error:
            if error.provably_not_sent:
                await _clear_checkpoint(context)
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        "DocuSign could not be reached, so no envelope was created",
                    )
                )
            return await self._recover_or_uncertain(call, session, marker_value)
        except BodyTooLargeError:
            return await self._recover_or_uncertain(call, session, marker_value)
        if response.status in (200, 201):
            try:
                created = _decode_created(response.body, input.is_draft)
                return MutationAttempt.branch_result(
                    "created", created, self.client.receipt(created.envelope_id)
                )
            except ValueError:
                return await self._recover_or_uncertain(call, session, marker_value)
        outcome = self.client.classify(operation, response)
        if outcome.retry and response.status < 500 and response.status != 408:
            await _clear_checkpoint(context)
            return MutationAttempt.retry(outcome.failure, outcome.retry_after)
        if 400 <= response.status < 500 and response.status != 408:
            return MutationAttempt.branch_result(
                "providerRejected", zero, self.client.receipt("", outcome.error_code), outcome.failure
            )
        return await self._recover_or_uncertain(call, session, marker_value, outcome.failure)

    async def _read_back(
        self,
        call: Call,
        session: Session,
        marker_value: str,
        mode: Literal["prior", "before", "after"],
    ) -> MutationAttempt[CreatedEnvelope] | None:
        context = call.context
        first_attempt = (
            context.first_attempt_at
            if context is not None and hasattr(context, "first_attempt_at")
            else self.client.now()
        )
        since = first_attempt - timedelta(hours=1)
        query = {
            "from_date": since.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "custom_field": "dexIdempotencyKey=" + marker_value,
            "include": "custom_fields",
        }
        try:
            response = await self.client.request(call, session, "GET", "/envelopes", query)
        except BodyTooLargeError:
            return None
        except ProviderTransportError:
            if mode == "prior" and context is not None:
                await _record_checkpoint(context, context.get_last_heartbeat_value(_CreateDispatchCheckpoint))
            return (
                MutationAttempt.retry(
                    failure(
                        "createEnvelopeFromTemplate",
                        FailureKind.AVAILABILITY,
                        "DocuSign could not list envelopes to find the one "
                        "an earlier attempt may have created",
                    )
                )
                if mode != "after"
                else None
            )
        if response.status != 200:
            outcome = self.client.classify("createEnvelopeFromTemplate", response)
            if outcome.retry and mode != "after":
                if mode == "prior" and context is not None:
                    prior_checkpoint = context.get_last_heartbeat_value(_CreateDispatchCheckpoint)
                    await _record_checkpoint(context, prior_checkpoint)
                return MutationAttempt.retry(outcome.failure, outcome.retry_after)
            return None
        try:
            matches = _tagged_envelopes(response.body, marker_value)
        except ValueError:
            return None
        if len(matches) == 1:
            match = matches[0]
            return MutationAttempt.branch_result(
                "created",
                CreatedEnvelope(
                    match.envelope_id,
                    status=match.status,
                    status_changed_at=match.status_changed_at,
                    was_recovered=True,
                ),
                self.client.receipt(match.envelope_id),
            )
        if len(matches) > 1:
            return MutationAttempt.uncertain(
                CreatedEnvelope(status=""),
                failure(
                    "createEnvelopeFromTemplate",
                    FailureKind.CONFLICT,
                    "several DocuSign envelopes carry this Step's dexIdempotencyKey, "
                    "so none was adopted and none was sent",
                ),
                self.client.receipt(),
            )
        return None

    async def _recover_or_uncertain(
        self, call: Call, session: Session, marker_value: str, reason: Any = None
    ) -> MutationAttempt[CreatedEnvelope]:
        recovered = await self._read_back(call, session, marker_value, "after")
        if recovered is not None:
            return recovered
        return MutationAttempt.uncertain(
            CreatedEnvelope(status=""),
            reason
            or failure(
                "createEnvelopeFromTemplate",
                FailureKind.TRANSPORT,
                "DocuSign's answer to the create request was lost",
            ),
            self.client.receipt(),
        )


def _tagged_envelopes(body: bytes, marker_value: str) -> list[Envelope]:
    record = _strict_object(body)
    entries = record.get("envelopes", [])
    if not isinstance(entries, list):
        raise ValueError("DocuSign returned a malformed response")
    matches = []
    for entry in entries:
        envelope = _decode_envelope(json.dumps(entry).encode())
        if any(
            field.name == "dexIdempotencyKey" and field.value == marker_value
            for field in envelope.custom_fields
        ):
            matches.append(envelope)
    return matches


async def _clear_checkpoint(context: Any) -> None:
    try:
        await context.heartbeat()
    except Exception:
        pass  # A lost clear still forces read-back before a later send.


async def _record_checkpoint(context: Any, checkpoint: _CreateDispatchCheckpoint | None) -> None:
    try:
        if checkpoint is None:
            checkpoint = _CreateDispatchCheckpoint(
                True, _go_timestamp(context.first_attempt_at)
            )
        await context.heartbeat(checkpoint)
    except Exception:
        pass  # A failed re-record leaves the earlier checkpoint in place.


def _create_body(input: CreateEnvelopeFromTemplateInput, marker_value: str) -> dict[str, Any]:
    if not isinstance(input.template_id, str) or not is_guid(input.template_id):
        raise ValueError(
            "templateId must be a DocuSign template ID GUID, such as 00000000-0000-0000-0000-000000000000"
        )
    if not 1 <= len(input.template_roles) <= 100:
        raise ValueError("templateRoles must fill 1 to 100 template roles")
    if not _bounded_text(input.email_subject, 0, 100) or not _bounded_text(input.email_blurb, 0, 10000):
        raise ValueError("emailSubject must be at most 100 characters and emailBlurb at most 10000")
    seen = set()
    roles = []
    for role in input.template_roles:
        role_name = role.get("roleName") if isinstance(role, dict) else role.role_name
        role_person_name = role.get("name") if isinstance(role, dict) else role.name
        if (
            not isinstance(role_name, str)
            or not isinstance(role_person_name, str)
            or not _bounded_text(role_name, 1, 100)
            or not _bounded_text(role_person_name, 1, 100)
            or not role_name.strip()
            or not role_person_name.strip()
        ):
            raise ValueError("roleName and name must be 1 to 100 characters")
        email = role.get("email") if isinstance(role, dict) else role.email
        if not isinstance(email, str) or not _bounded_text(email, 1, 100):
            raise ValueError("email must be one email address of at most 100 characters")
        local, separator, domain = email.partition("@")
        if (
            not separator
            or not local
            or "." not in domain
            or "@" in domain
            or len(email.encode("utf-8")) > 100
            or any(character in email for character in " \t\r\n<>,;")
        ):
            raise ValueError("email must be one email address of at most 100 characters")
        routing_order = role.get("routingOrder", 0) if isinstance(role, dict) else role.routing_order
        if type(routing_order) is not int or routing_order < 0 or routing_order > 999:
            raise ValueError("routingOrder must be 1 to 999, or 0 to keep the template's order")
        if role_name.lower() in seen:
            raise ValueError("template role is filled twice")
        seen.add(role_name.lower())
        item = {"roleName": role_name, "name": role_person_name, "email": email}
        if routing_order:
            item["routingOrder"] = str(routing_order)
        roles.append(item)
    if len(input.custom_fields) > 10:
        raise ValueError("customFields holds at most 10 fields")
    fields = [{"name": "dexIdempotencyKey", "value": marker_value, "show": "false", "required": "false"}]
    names = {"dexidempotencykey"}
    for field in input.custom_fields:
        field_name = field.get("name") if isinstance(field, dict) else field.name
        field_value = field.get("value") if isinstance(field, dict) else field.value
        if (
            not isinstance(field_name, str)
            or not isinstance(field_value, str)
            or not _bounded_text(field_name, 1, 50)
            or not _bounded_text(field_value, 0, 100)
            or field_name.lower() in names
        ):
            raise ValueError("customFields needs unique names of 1 to 50 characters and values at most 100")
        names.add(field_name.lower())
        fields.append({"name": field_name, "value": field_value, "show": "false", "required": "false"})
    body: dict[str, Any] = {
        "templateId": input.template_id.lower(),
        "templateRoles": roles,
        "status": "created" if input.is_draft else "sent",
        "customFields": {"textCustomFields": fields},
    }
    if input.email_subject:
        body["emailSubject"] = input.email_subject
    if input.email_blurb:
        body["emailBlurb"] = input.email_blurb
    return body
