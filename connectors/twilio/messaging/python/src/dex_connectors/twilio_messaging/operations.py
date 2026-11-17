# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Twilio send and status operations with Go-compatible outcome classification."""

import json
from dataclasses import replace
from datetime import timedelta
from urllib.parse import urlsplit

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
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import GET_MESSAGE_DEFINITION, SEND_MESSAGE_DEFINITION
from .client import (
    _MESSAGE,
    _PHONE,
    Client,
    Exchange,
    error_code,
    failure,
    is_whatsapp,
    parse_sender,
    rejection,
)
from .models import GetMessageInput, Message, SendMessageInput

# Only these statuses prove Twilio refused a send; other 4xx statuses leave the outcome unknown.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


def _message_sid(body: bytes) -> str:
    """Return the top-level sid of a response body, or empty when it has none."""
    try:
        resource = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return ""
    sid = resource.get("sid") if isinstance(resource, dict) else None
    return sid if isinstance(sid, str) else ""


def _send_form(client: Client, input: SendMessageInput) -> tuple[list[tuple[str, str]], Message]:
    recipient = input.to.strip()
    if not _PHONE.fullmatch(recipient) and not is_whatsapp(recipient):
        raise ValueError("recipient must be an E.164 phone number or a whatsapp:+E.164 address")
    sender, service = parse_sender(input.sender) if input.sender.strip() else client.default_sender
    if not sender and not service:
        raise ValueError("sender is required: set defaultSender on the connection or sender on the input")
    if sender and is_whatsapp(sender) != is_whatsapp(recipient):
        raise ValueError("sender and recipient must both be whatsapp: addresses or both be phone numbers")
    try:
        input.body.encode("utf-8")
    except UnicodeEncodeError as error:
        raise ValueError("body must be valid UTF-8") from error
    if len(input.body) > 1600:
        raise ValueError("body cannot exceed 1600 characters")
    if not input.body.strip() and not input.media_urls:
        raise ValueError("body or at least one media URL is required")
    if len(input.media_urls) > 10:
        raise ValueError("at most 10 media URLs are allowed")
    form = [("To", recipient)]
    if input.body:
        form.append(("Body", input.body))
    for media_url in input.media_urls:
        if (
            len(media_url.encode("utf-8")) > 2048
            or not media_url
            or any(ord(ch) <= 32 or ord(ch) > 126 for ch in media_url)
        ):
            raise ValueError("media URLs must be printable ASCII without spaces and at most 2048 bytes")
        parsed = urlsplit(media_url)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password:
            raise ValueError("media URLs must be absolute HTTPS URLs without credentials")
        form.append(("MediaUrl", media_url))
    form.append(("MessagingServiceSid", service) if service else ("From", sender))
    return form, Message(
        account_sid=client.account_sid, messaging_service_sid=service, from_=sender, to=recipient
    )


class SendMessageOperation:
    """Fence one nonrepeatable Twilio Messages POST and classify its outcome."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Twilio client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated send Mutation definition."""
        return SEND_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendMessageInput) -> IdempotencyKey:
        """Record the Call ID without sending it to Twilio as a replay key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendMessageInput) -> MutationAttempt[Message]:
        """Send under a fence, resending an unknown outcome below the send cap.

        Twilio accepts no idempotency key, so an unknown outcome retries while possible
        sends stay below `call.max_sends` and selects uncertain at the cap. A saved message
        SID is read back instead of resent, and a definite refusal is never resent.
        """
        empty = Message()
        try:
            form, requested = _send_form(self.client, input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure("sendMessage", FailureKind.VALIDATION, str(error))
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
                    "sendMessage", FailureKind.LOCAL_DEFECT, "Twilio dispatch fence could not be initialized"
                ),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    "sendMessage",
                    FailureKind.AVAILABILITY,
                    "Twilio send checkpoint is unreadable; no message was sent in this attempt",
                )
            )
        saved_sid = decision.marker.provider_id if decision.marker else ""
        # A saved SID proves Twilio created the message, so it is read back and never resent.
        if decision.kind == DecisionKind.READ_BACK_REQUIRED or (
            decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED and saved_sid
        ):
            return await self._read_back(call, requested, saved_sid)
        # This attempt's send would be the last one the cap allows when no further send remains.
        can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        # An earlier send may have created the message, so a refusal of a resend proves nothing.
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        authorization, credential_failure, is_transient = await self.client.authorization(call, "sendMessage")
        if credential_failure is not None:
            if has_possible_earlier_send:
                # notSent would hide the earlier possible send, so nothing is recorded.
                return MutationAttempt.retry(credential_failure)
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(credential_failure)
            if is_transient:
                return MutationAttempt.retry(credential_failure)
            return MutationAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        assert authorization is not None
        try:
            exchange = await fence.dispatch(
                lambda _: self.client.exchange(
                    authorization, "POST", self.client.messages_url + ".json", form
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    "sendMessage",
                    FailureKind.TRANSPORT,
                    "Twilio dispatch checkpoint or attempt time is unavailable",
                )
            )
        except (TimeoutError, DispatchNotAllowedError):
            return self._resend_or_select_uncertain(
                call, can_resend_unknown_outcome, requested, None,
                FailureKind.TRANSPORT, "Twilio message outcome is unknown",
            )
        if exchange.error == "notBuilt":
            try:
                await fence.record_can_resend()
            except Exception:
                pass  # The request was never sent; a lost checkpoint leaves the claim, which still counts.
            if has_possible_earlier_send:
                return MutationAttempt.retry(
                    failure(
                        "sendMessage",
                        FailureKind.LOCAL_DEFECT,
                        "Twilio request could not be built; no message was sent in this attempt",
                    )
                )
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("sendMessage", FailureKind.LOCAL_DEFECT, "Twilio request could not be built"),
            )
        if exchange.error:
            if exchange.error == "beforeSend":
                try:
                    await fence.record_can_resend()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            "sendMessage",
                            FailureKind.TRANSPORT,
                            "Twilio could not be reached and its dispatch checkpoint is unavailable",
                        )
                    )
                return MutationAttempt.retry(
                    failure(
                        "sendMessage",
                        FailureKind.TRANSPORT,
                        "Twilio could not be reached, so no message was sent",
                    )
                )
            return self._resend_or_select_uncertain(
                call, can_resend_unknown_outcome, requested, None,
                FailureKind.TRANSPORT, "Twilio message outcome is unknown",
            )
        receipt = self.client.receipt(call, exchange)
        status = exchange.status
        if 200 <= status < 300:
            if exchange.is_body_too_large:
                return self._resend_or_select_uncertain(
                    call, can_resend_unknown_outcome, requested, exchange,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Twilio message response exceeds the configured size limit",
                )
            if exchange.has_body_read_failed:
                return self._resend_or_select_uncertain(
                    call, can_resend_unknown_outcome, requested, exchange,
                    FailureKind.TRANSPORT, "Twilio message response was interrupted",
                )
            try:
                message = self.client.decode_message(exchange.body, authorization)
            except ValueError:
                if _MESSAGE.fullmatch(_message_sid(exchange.body)):
                    # Twilio returned a created message SID, so a resend could duplicate the message.
                    return MutationAttempt.uncertain(
                        requested,
                        failure(
                            "sendMessage",
                            FailureKind.PROTOCOL,
                            "Twilio accepted the request but returned an unusable message",
                        ),
                        receipt,
                    )
                return self._resend_or_select_uncertain(
                    call, can_resend_unknown_outcome, requested, exchange,
                    FailureKind.PROTOCOL, "Twilio message response is malformed",
                )
            try:
                await fence.record_provider_id(message.sid)
            except Exception:
                # The accepted response remains definitive when the recovery checkpoint fails.
                pass
            return MutationAttempt.branch_result(
                "accepted", message, self.client.receipt(call, exchange, message.sid)
            )
        if status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                suffix = (
                    " before creating it; retry checkpoint is unavailable"
                    if status == 429
                    else " and retry checkpoint are temporarily unavailable"
                )
                text = (
                    "Twilio rate limited the message" + suffix if status == 429 else "Twilio service" + suffix
                )
                return MutationAttempt.retry(
                    failure(
                        "sendMessage",
                        FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY,
                        text,
                    ),
                    self.client.retry_after(exchange),
                )
            if status == 429:
                return MutationAttempt.retry(
                    failure(
                        "sendMessage",
                        FailureKind.RATE_LIMIT,
                        "Twilio rate limited the message before creating it",
                    ),
                    self.client.retry_after(exchange),
                )
            return MutationAttempt.retry(
                failure("sendMessage", FailureKind.AVAILABILITY, "Twilio service is temporarily unavailable"),
                self.client.retry_after(exchange),
            )
        if status in _DEFINITE_REFUSAL_STATUSES and not _MESSAGE.fullmatch(_message_sid(exchange.body)):
            code = error_code(exchange.body)
            rejected = replace(requested, error_code=code)
            refusal = (
                failure(
                    "sendMessage",
                    FailureKind.AUTHENTICATION,
                    "Twilio rejected the message credentials; update the connection",
                )
                if status == 401
                else rejection("sendMessage", "message", status, code)
            )
            rejection_receipt = self.client.receipt(call, exchange, error_code=code)
            if has_possible_earlier_send:
                return MutationAttempt.uncertain(rejected, refusal, rejection_receipt)
            try:
                await fence.record_can_resend()
            except Exception:
                pass  # The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
            return MutationAttempt.branch_result("providerRejected", rejected, rejection_receipt, refusal)
        return self._resend_or_select_uncertain(
            call, can_resend_unknown_outcome, requested, exchange,
            FailureKind.AVAILABILITY, "Twilio message outcome is unknown",
        )

    def _resend_or_select_uncertain(
        self,
        call: Call,
        can_resend_unknown_outcome: bool,
        requested: Message,
        exchange: Exchange | None,
        kind: FailureKind,
        message: str,
    ) -> MutationAttempt[Message]:
        """Retry a possibly sent message while the send cap allows another send."""
        unknown = failure("sendMessage", kind, message)
        if can_resend_unknown_outcome:
            # The claim stays the last marker, so the next attempt counts this send.
            return MutationAttempt.retry(unknown, self.client.retry_after(exchange) if exchange else 0)
        receipt = self.client.receipt(call, exchange) if exchange else self.client.receipt(call)
        return MutationAttempt.uncertain(requested, unknown, receipt)

    async def _read_back(self, call: Call, requested: Message, provider_id: str) -> MutationAttempt[Message]:
        if not _MESSAGE.fullmatch(provider_id):
            return MutationAttempt.uncertain(
                requested,
                failure("sendMessage", FailureKind.TRANSPORT, "Twilio send outcome cannot be confirmed"),
                self.client.receipt(call),
            )
        authorization, credential_failure, is_transient = await self.client.authorization(call, "sendMessage")
        if credential_failure is not None:
            if is_transient:
                return MutationAttempt.retry(credential_failure)
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "sendMessage",
                    FailureKind.AUTHENTICATION,
                    "Twilio accepted message cannot be read; reconnect the connection",
                ),
                self.client.receipt(call, message_sid=provider_id),
            )
        assert authorization is not None
        exchange = await self.client.exchange(
            authorization, "GET", self.client.messages_url + "/" + provider_id + ".json"
        )
        if (
            exchange.error
            or exchange.has_body_read_failed
            or exchange.status in (408, 429)
            or exchange.status >= 500
        ):
            return MutationAttempt.retry(
                failure(
                    "sendMessage",
                    FailureKind.AVAILABILITY,
                    "Twilio accepted message read-back is temporarily unavailable",
                )
            )
        receipt = self.client.receipt(call, exchange, provider_id)
        if exchange.status != 200 or exchange.is_body_too_large:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "sendMessage", FailureKind.PROTOCOL, "Twilio accepted message could not be confirmed"
                ),
                receipt,
            )
        try:
            message = self.client.decode_message(exchange.body, authorization)
            if message.sid != provider_id or message.to != requested.to:
                raise ValueError("message identity differs")
        except ValueError:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "sendMessage", FailureKind.PROTOCOL, "Twilio accepted message could not be confirmed"
                ),
                receipt,
            )
        return MutationAttempt.branch_result("accepted", message, receipt)


class GetMessageOperation:
    """Read a Twilio Message by its stable SID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Twilio client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated message Query definition."""
        return GET_MESSAGE_DEFINITION

    async def invoke(self, call: Call, input: GetMessageInput) -> QueryAttempt[Message]:
        """Classify message reads, including transient provider and transport failures."""
        empty = Message()
        sid = input.message_sid.strip()
        if not _MESSAGE.fullmatch(sid):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "getMessage",
                    FailureKind.VALIDATION,
                    "message SID must be SM or MM followed by 32 lowercase hexadecimal characters",
                ),
            )
        authorization, credential_failure, is_transient = await self.client.authorization(call, "getMessage")
        if credential_failure is not None:
            if is_transient:
                return QueryAttempt.retry(credential_failure)
            return QueryAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        assert authorization is not None
        exchange = await self.client.exchange(
            authorization, "GET", self.client.messages_url + "/" + sid + ".json"
        )
        if exchange.error == "notBuilt":
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("getMessage", FailureKind.LOCAL_DEFECT, "Twilio request could not be built"),
            )
        if exchange.error:
            return QueryAttempt.retry(
                failure(
                    "getMessage", FailureKind.TRANSPORT, "Twilio message status is temporarily unavailable"
                )
            )
        receipt = self.client.receipt(call, exchange, sid)
        status = exchange.status
        if 200 <= status < 300:
            if exchange.has_body_read_failed:
                return QueryAttempt.retry(
                    failure("getMessage", FailureKind.TRANSPORT, "Twilio message response was interrupted")
                )
            if exchange.is_body_too_large:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    receipt,
                    failure(
                        "getMessage",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Twilio message response exceeds the configured size limit",
                    ),
                )
            try:
                message = self.client.decode_message(exchange.body, authorization)
                if message.sid != sid:
                    raise ValueError("message SID differs")
            except ValueError:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    receipt,
                    failure("getMessage", FailureKind.PROTOCOL, "Twilio returned an invalid message"),
                )
            return QueryAttempt.branch_result("found", message, receipt)
        if status == 404:
            code = error_code(exchange.body)
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                self.client.receipt(call, exchange, sid, code),
                failure("getMessage", FailureKind.NOT_FOUND, "Twilio has no message with that SID"),
            )
        if status == 429:
            return QueryAttempt.retry(
                failure("getMessage", FailureKind.RATE_LIMIT, "Twilio rate limited the message query"),
                self.client.retry_after(exchange),
            )
        if status == 408 or status >= 500:
            return QueryAttempt.retry(
                failure(
                    "getMessage", FailureKind.AVAILABILITY, "Twilio message status is temporarily unavailable"
                )
            )
        if 400 <= status < 500:
            code = error_code(exchange.body)
            return QueryAttempt.branch_result(
                "providerRejected",
                empty,
                self.client.receipt(call, exchange, sid, code),
                rejection("getMessage", "message query", status, code),
            )
        return QueryAttempt.branch_result(
            "invalidResponse",
            empty,
            receipt,
            failure("getMessage", FailureKind.PROTOCOL, "Twilio returned an unexpected HTTP status"),
        )
