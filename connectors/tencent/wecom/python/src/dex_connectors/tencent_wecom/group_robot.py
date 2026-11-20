# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Single-dispatch WeCom group robot message Mutation."""

import re
from datetime import datetime
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

from ._generated.definitions import SEND_GROUP_ROBOT_MESSAGE_DEFINITION
from .client import (
    _DEFINITE_REFUSAL_STATUSES,
    _RETRYABLE_CODES,
    Exchange,
    failure_kind_for_code,
    provider_code,
)
from .models import SendGroupRobotMessageInput, SentGroupRobotMessage

if TYPE_CHECKING:
    from .client import Client

_WEBHOOK_KEY = re.compile(r"^[A-Za-z0-9-]{8,64}$")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "wecom", "sendGroupRobotMessage", message)


def _branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[SentGroupRobotMessage]:
    return MutationAttempt.branch_result(
        branch, SentGroupRobotMessage(""), Receipt(), _failure(kind, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[SentGroupRobotMessage]:
    return MutationAttempt.retry(_failure(kind, message), delay)


def _uncertain(kind: FailureKind, message: str) -> MutationAttempt[SentGroupRobotMessage]:
    return MutationAttempt.uncertain(SentGroupRobotMessage(""), _failure(kind, message), Receipt())


def _printable_identifier(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 64 and all(33 <= ord(c) <= 126 and c != "|" for c in value)


def _valid_input(input: SendGroupRobotMessageInput) -> bool:
    if not isinstance(input, SendGroupRobotMessageInput) or not input.content:
        return False
    length = len(input.content.encode())
    if input.message_type == "text":
        if length > 2048:
            return False
    elif input.message_type == "markdown":
        if length > 4096 or input.mentioned_user_ids or input.mentioned_mobile_numbers:
            return False
    else:
        return False
    return all(_printable_identifier(x) for x in (*input.mentioned_user_ids, *input.mentioned_mobile_numbers))


class SendGroupRobotMessageOperation:
    """Post one text or markdown message behind a Dex heartbeat Fence."""

    def __init__(self, client: "Client") -> None:
        if client is None:
            raise ValueError("WeCom client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated group robot Mutation definition."""
        return SEND_GROUP_ROBOT_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendGroupRobotMessageInput) -> IdempotencyKey:
        """Keep stable Call identity; WeCom does not receive this key."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: SendGroupRobotMessageInput
    ) -> MutationAttempt[SentGroupRobotMessage]:
        """Classify one fenced post without revealing the webhook key."""
        if not _valid_input(input):
            return _branch("defect", FailureKind.VALIDATION, "WeCom group robot input is invalid")
        try:
            fence = Fence(
                call, self.definition().step_defaults.execute_method_timeout, max_sends=call.max_sends
            )
            decision = await fence.decide()
        except ValueError:
            return _branch("defect", FailureKind.LOCAL_DEFECT, "WeCom dispatch fence is unavailable")
        if (
            decision.kind != DecisionKind.FAIL_CLOSED
            and decision.marker
            and decision.marker.provider_id == "errcode-0"
        ):
            return MutationAttempt.branch_result(
                "sent", SentGroupRobotMessage(input.message_type, len(input.content.encode())), Receipt()
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return _retry(FailureKind.AVAILABILITY, "WeCom dispatch checkpoint is unreadable")
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return _uncertain(FailureKind.TRANSPORT, "WeCom group robot send cap reached")
        prior_sends = decision.prior_send_upper_bound
        can_resend = prior_sends + 1 < call.max_sends
        has_prior_send = prior_sends > 0
        try:
            credentials = await self.client.credentials.resolve(call)
        except Exception:
            return await self._local_failure(
                fence, has_prior_send, FailureKind.AUTHENTICATION, "WeCom credentials are unavailable"
            )
        if credentials.auth_method_id != "group-robot" or credentials.webhook_key is None:
            return await self._local_failure(
                fence, has_prior_send, FailureKind.VALIDATION, "WeCom group robot method is required"
            )
        key = credentials.webhook_key.reveal()
        if not _WEBHOOK_KEY.fullmatch(key):
            return await self._local_failure(
                fence, has_prior_send, FailureKind.AUTHENTICATION, "WeCom webhook key is invalid"
            )
        if input.message_type == "markdown":
            body: dict[str, object] = {"msgtype": "markdown", "markdown": {"content": input.content}}
        else:
            text: dict[str, object] = {"content": input.content}
            if input.mentioned_user_ids:
                text["mentioned_list"] = list(input.mentioned_user_ids)
            if input.mentioned_mobile_numbers:
                text["mentioned_mobile_list"] = list(input.mentioned_mobile_numbers)
            body = {"msgtype": "text", "text": text}
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/cgi-bin/webhook/send",
                params={"key": key},
                headers={"Accept": "application/json", "Content-Type": "application/json"},
                json=body,
            )
        except (httpx.HTTPError, ValueError):
            return await self._local_failure(
                fence, has_prior_send, FailureKind.LOCAL_DEFECT, "WeCom post request is invalid"
            )

        async def send(_deadline: datetime) -> Exchange:
            return await self.client.request(request)

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _retry(FailureKind.TRANSPORT, "WeCom group robot post was not started")
        except TimeoutError:
            return self._retry_or_uncertain(
                can_resend, FailureKind.TRANSPORT, "WeCom post outcome is unknown"
            )
        except Exception:
            return self._retry_or_uncertain(
                can_resend, FailureKind.TRANSPORT, "WeCom post outcome is unknown"
            )
        if exchange.error == "request":
            if not exchange.request_sent:
                await self._record_can_resend(fence)
                return _retry(FailureKind.TRANSPORT, "WeCom could not be reached before posting")
            return self._retry_or_uncertain(
                can_resend, FailureKind.TRANSPORT, "WeCom post outcome is unknown"
            )
        if exchange.status in (429, 503):
            await self._record_can_resend(fence)
            kind = FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY
            return _retry(kind, "WeCom did not process the group robot post", exchange.retry_after)
        if exchange.status in _DEFINITE_REFUSAL_STATUSES:
            decoded = provider_code(exchange.body) if not exchange.error else None
            if exchange.error or decoded and decoded[0] == 0 or decoded and decoded[1].get("msgid"):
                return self._retry_or_uncertain(
                    can_resend, FailureKind.PROTOCOL, "WeCom post outcome is unknown"
                )
            return await self._reject_or_uncertain(
                fence, has_prior_send, FailureKind.PROVIDER_REJECTION, "WeCom refused the group robot post"
            )
        if not 200 <= exchange.status < 300:
            return self._retry_or_uncertain(
                can_resend, FailureKind.AVAILABILITY, "WeCom post outcome is unknown"
            )
        if exchange.error == "read":
            return _uncertain(FailureKind.TRANSPORT, "WeCom group robot post response broke")
        if exchange.error == "too_large":
            return self._retry_or_uncertain(
                can_resend, FailureKind.RESPONSE_TOO_LARGE, "WeCom post response is oversized"
            )
        decoded = provider_code(exchange.body)
        if decoded is None:
            return self._retry_or_uncertain(
                can_resend, FailureKind.PROTOCOL, "WeCom post response is malformed"
            )
        code, _ = decoded
        if code in _RETRYABLE_CODES:
            await self._record_can_resend(fence)
            return _retry(
                failure_kind_for_code(code), "WeCom deferred the group robot post", 60 if code == 45009 else 0
            )
        if code != 0:
            return await self._reject_or_uncertain(
                fence, has_prior_send, failure_kind_for_code(code), "WeCom refused the group robot post"
            )
        try:
            await fence.record_provider_id("errcode-0")
        except Exception:
            pass  # The claimed send remains counted when its acceptance checkpoint is lost.
        return MutationAttempt.branch_result(
            "sent", SentGroupRobotMessage(input.message_type, len(input.content.encode())), Receipt()
        )

    async def _local_failure(
        self, fence: Fence, has_prior_send: bool, kind: FailureKind, message: str
    ) -> MutationAttempt[SentGroupRobotMessage]:
        if has_prior_send:
            return _uncertain(kind, message + "; an earlier post may have reached WeCom")
        try:
            await fence.record_not_sent()
        except Exception:
            pass  # A terminal branch cannot cause another dispatch.
        return _branch("defect", kind, message)

    async def _reject_or_uncertain(
        self, fence: Fence, has_prior_send: bool, kind: FailureKind, message: str
    ) -> MutationAttempt[SentGroupRobotMessage]:
        if has_prior_send:
            return _uncertain(kind, message + "; an earlier post may have reached WeCom")
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
    ) -> MutationAttempt[SentGroupRobotMessage]:
        return _retry(kind, message) if can_resend else _uncertain(kind, message)
