# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fenced WeCom application message Mutation with token and duplicate checking."""

import re
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import SEND_APPLICATION_MESSAGE_DEFINITION
from .client import (
    _DEFINITE_REFUSAL_STATUSES,
    _RETRYABLE_CODES,
    Exchange,
    failure_kind_for_code,
    provider_code,
)
from .models import SendApplicationMessageInput, SentApplicationMessage

if TYPE_CHECKING:
    from .client import Client

_CORP_ID = re.compile(r"^[A-Za-z0-9]{4,64}$")
_AGENT_ID = re.compile(r"^[1-9][0-9]{0,9}$")
_GROUP_ID = re.compile(r"^[1-9][0-9]{0,9}$")
_MESSAGE_ID = re.compile(r"^[A-Za-z0-9_=+/-]{1,128}$")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "wecom", "sendApplicationMessage", message)


def _branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[SentApplicationMessage]:
    return MutationAttempt.branch_result(branch, SentApplicationMessage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str) -> MutationAttempt[SentApplicationMessage]:
    return MutationAttempt.retry(_failure(kind, message))


def _uncertain(kind: FailureKind, message: str) -> MutationAttempt[SentApplicationMessage]:
    return MutationAttempt.uncertain(SentApplicationMessage(), _failure(kind, message), Receipt())


def _printable_identifier(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 64 and all(33 <= ord(c) <= 126 and c != "|" for c in value)


def _valid_input(input: SendApplicationMessageInput) -> bool:
    if (
        not isinstance(input, SendApplicationMessageInput)
        or not input.content
        or len(input.content.encode()) > 2048
    ):
        return False
    if (
        input.message_type not in {"text", "markdown"}
        or input.message_type == "markdown"
        and input.is_confidential
    ):
        return False
    has_lists = bool(input.to_user_ids or input.to_department_ids or input.to_tag_ids)
    if input.to_all_members == has_lists:
        return False
    if len(input.to_user_ids) > 1000 or len(input.to_department_ids) > 100 or len(input.to_tag_ids) > 100:
        return False
    if any(not _printable_identifier(value) or value == "@all" for value in input.to_user_ids):
        return False
    return all(_GROUP_ID.fullmatch(value) for value in (*input.to_department_ids, *input.to_tag_ids))


def _split(value: str) -> tuple[str, ...]:
    return tuple(part for part in value.split("|") if part)


class SendApplicationMessageOperation:
    """Send through a self-built app with a three-send cap and duplicate checking."""

    def __init__(self, client: "Client") -> None:
        if client is None:
            raise ValueError("WeCom client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated application Mutation definition."""
        return SEND_APPLICATION_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendApplicationMessageInput) -> IdempotencyKey:
        """Keep stable Call identity while WeCom deduplicates identical request content."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: SendApplicationMessageInput
    ) -> MutationAttempt[SentApplicationMessage]:
        """Send an identical JSON request on permitted attempts and classify its result."""
        if not _valid_input(input):
            return _branch("defect", FailureKind.VALIDATION, "WeCom application input is invalid")
        try:
            fence = Fence(
                call, self.definition().step_defaults.execute_method_timeout, max_sends=call.max_sends
            )
            decision = await fence.decide()
        except ValueError:
            return _branch("defect", FailureKind.LOCAL_DEFECT, "WeCom dispatch fence is unavailable")
        if decision.kind != DecisionKind.FAIL_CLOSED and decision.marker and decision.marker.provider_id:
            marker = decision.marker.provider_id
            return MutationAttempt.branch_result(
                "sent", SentApplicationMessage(message_id="" if marker == "errcode-0" else marker), Receipt()
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return _retry(FailureKind.AVAILABILITY, "WeCom dispatch checkpoint is unreadable")
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return _uncertain(FailureKind.TRANSPORT, "WeCom application send cap reached")
        prior_sends = decision.prior_send_upper_bound
        can_resend = prior_sends + 1 < call.max_sends
        has_prior_send = prior_sends > 0
        first_attempt = getattr(call.context, "first_attempt_at", None)
        if (
            has_prior_send
            and isinstance(first_attempt, datetime)
            and datetime.now(UTC) >= first_attempt + timedelta(hours=3, minutes=50)
        ):
            return _uncertain(FailureKind.TRANSPORT, "WeCom duplicate-check window may have expired")
        try:
            credentials = await self.client.credentials.resolve(call)
        except Exception:
            return await self._local_failure(
                fence, has_prior_send, FailureKind.AUTHENTICATION, "WeCom credentials are unavailable"
            )
        if credentials.auth_method_id != "application" or credentials.corp_secret is None:
            return await self._local_failure(
                fence, has_prior_send, FailureKind.VALIDATION, "WeCom application method is required"
            )
        corp_id = credentials.corp_id or ""
        agent_id = credentials.agent_id or ""
        secret = credentials.corp_secret.reveal()
        if (
            not _CORP_ID.fullmatch(corp_id)
            or not _AGENT_ID.fullmatch(agent_id)
            or not secret
            or any(ord(c) <= 32 or ord(c) >= 127 for c in secret)
        ):
            return await self._local_failure(
                fence, has_prior_send, FailureKind.AUTHENTICATION, "WeCom application credentials are invalid"
            )
        body: dict[str, object] = {
            "msgtype": input.message_type,
            "agentid": int(agent_id),
            "enable_duplicate_check": 1,
            "duplicate_check_interval": 14400,
        }
        if input.to_all_members:
            body["touser"] = "@all"
        else:
            for key, values in (
                ("touser", input.to_user_ids),
                ("toparty", input.to_department_ids),
                ("totag", input.to_tag_ids),
            ):
                if values:
                    body[key] = "|".join(values)
        body[input.message_type] = {"content": input.content}
        if input.message_type == "text" and input.is_confidential:
            body["safe"] = 1
        token, token_kind, token_decision = await self.client.access_token(corp_id, secret)
        if token_decision == "retry":
            return await self._retry_before_send(
                fence,
                has_prior_send,
                token_kind or FailureKind.AVAILABILITY,
                "WeCom access token is temporarily unavailable",
            )
        if token_decision == "providerRejected":
            return await self._local_failure(
                fence,
                has_prior_send,
                token_kind or FailureKind.PROVIDER_REJECTION,
                "WeCom refused application credentials",
                "providerRejected",
            )
        if token_decision == "defect":
            return await self._local_failure(
                fence,
                has_prior_send,
                token_kind or FailureKind.LOCAL_DEFECT,
                "WeCom access token request is invalid",
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/cgi-bin/message/send",
                params={"access_token": token},
                headers={"Accept": "application/json", "Content-Type": "application/json"},
                json=body,
            )
        except (httpx.HTTPError, ValueError):
            return await self._local_failure(
                fence, has_prior_send, FailureKind.LOCAL_DEFECT, "WeCom application request is invalid"
            )

        async def send(_deadline: datetime) -> Exchange:
            return await self.client.request(request)

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _retry(FailureKind.TRANSPORT, "WeCom application send was not started")
        except TimeoutError:
            return self._retry_or_uncertain(
                can_resend, FailureKind.TRANSPORT, "WeCom application outcome is unknown"
            )
        except Exception:
            return self._retry_or_uncertain(
                can_resend, FailureKind.TRANSPORT, "WeCom application outcome is unknown"
            )
        if exchange.error == "request":
            if not exchange.request_sent:
                await self._record_can_resend(fence)
                return _retry(FailureKind.TRANSPORT, "WeCom could not be reached before sending")
            return self._retry_or_uncertain(
                can_resend, FailureKind.TRANSPORT, "WeCom application outcome is unknown"
            )
        if exchange.status in (429, 503):
            await self._record_can_resend(fence)
            return _retry(
                FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY,
                "WeCom did not process the application message",
            )
        if exchange.status in _DEFINITE_REFUSAL_STATUSES:
            decoded = provider_code(exchange.body) if not exchange.error else None
            if exchange.error or decoded and (decoded[0] == 0 or decoded[1].get("msgid")):
                return self._retry_or_uncertain(
                    can_resend, FailureKind.PROTOCOL, "WeCom application outcome is unknown"
                )
            return await self._reject_or_uncertain(
                fence, has_prior_send, FailureKind.PROVIDER_REJECTION, "WeCom refused application message"
            )
        if not 200 <= exchange.status < 300:
            return self._retry_or_uncertain(
                can_resend, FailureKind.AVAILABILITY, "WeCom application outcome is unknown"
            )
        if exchange.error == "read":
            return _uncertain(FailureKind.TRANSPORT, "WeCom application response broke")
        if exchange.error == "too_large":
            return self._retry_or_uncertain(
                can_resend, FailureKind.RESPONSE_TOO_LARGE, "WeCom application response is oversized"
            )
        decoded = provider_code(exchange.body)
        if decoded is None:
            return self._retry_or_uncertain(
                can_resend, FailureKind.PROTOCOL, "WeCom application response is malformed"
            )
        code, data = decoded
        if code in {40014, 42001, 41001}:
            await self.client.invalidate_token(corp_id, secret, token)
            await self._record_can_resend(fence)
            return _retry(failure_kind_for_code(code), "WeCom rejected the cached access token")
        if code in _RETRYABLE_CODES:
            await self._record_can_resend(fence)
            return _retry(failure_kind_for_code(code), "WeCom deferred the application message")
        if code != 0:
            return await self._reject_or_uncertain(
                fence, has_prior_send, failure_kind_for_code(code), "WeCom refused application message"
            )
        keys = ("msgid", "invaliduser", "invalidparty", "invalidtag", "unlicenseduser")
        if any(not isinstance(data.get(key, ""), str) for key in keys):
            return _uncertain(FailureKind.PROTOCOL, "WeCom accepted a message but returned malformed fields")
        message_id = data.get("msgid", "")
        marker = message_id if _MESSAGE_ID.fullmatch(message_id) else "errcode-0"
        try:
            await fence.record_provider_id(marker)
        except Exception:
            pass  # The claimed send remains counted when its acceptance checkpoint is lost.
        return MutationAttempt.branch_result(
            "sent",
            SentApplicationMessage(
                message_id,
                _split(data.get("invaliduser", "")),
                _split(data.get("invalidparty", "")),
                _split(data.get("invalidtag", "")),
                _split(data.get("unlicenseduser", "")),
            ),
            Receipt(),
        )

    async def _retry_before_send(
        self, fence: Fence, has_prior_send: bool, kind: FailureKind, message: str
    ) -> MutationAttempt[SentApplicationMessage]:
        if not has_prior_send:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # A lost notSent checkpoint cannot create a provider send.
        return _retry(kind, message)

    async def _local_failure(
        self,
        fence: Fence,
        has_prior_send: bool,
        kind: FailureKind,
        message: str,
        branch: str = "defect",
    ) -> MutationAttempt[SentApplicationMessage]:
        if has_prior_send:
            return _uncertain(kind, message + "; an earlier send may have reached WeCom")
        try:
            await fence.record_not_sent()
        except Exception:
            pass  # A terminal branch cannot create another provider send.
        return _branch(branch, kind, message)

    async def _reject_or_uncertain(
        self, fence: Fence, has_prior_send: bool, kind: FailureKind, message: str
    ) -> MutationAttempt[SentApplicationMessage]:
        if has_prior_send:
            return _uncertain(kind, message + "; an earlier send may have reached WeCom")
        await self._record_can_resend(fence)
        return _branch("providerRejected", kind, message)

    @staticmethod
    async def _record_can_resend(fence: Fence) -> None:
        try:
            await fence.record_can_resend()
        except Exception:
            pass  # A lost proof leaves the claimed send counted.

    @staticmethod
    def _retry_or_uncertain(
        can_resend: bool, kind: FailureKind, message: str
    ) -> MutationAttempt[SentApplicationMessage]:
        return _retry(kind, message) if can_resend else _uncertain(kind, message)
