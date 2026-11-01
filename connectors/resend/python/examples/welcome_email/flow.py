# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable welcome email Flow using the Resend sendEmail Mutation."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.resend import Connection, SendEmailInput, SentEmail, new_send_email_step

FLOW_TYPE = "ResendWelcomeEmail"
CONNECTION_NAME = "resend"
_SEND_STEP_TYPE = "SendEmail"
_OUTCOME = dex.Attribute("resend-welcome-email-outcome", SentEmail)


@dataclass(frozen=True, slots=True)
class WelcomeEmailRequest:
    """Sender on a Resend-verified domain, recipients, subject, and HTML body."""

    from_address: str = field(metadata={"dex_json_name": "from"})
    to: tuple[str, ...]
    subject: str
    html: str


def map_to_send_email_input(request: WelcomeEmailRequest) -> SendEmailInput:
    """Send the request unchanged; Resend deduplicates retries by Call ID."""
    return SendEmailInput(request.from_address, request.to, request.subject, request.html)


# dex:group group-id:welcome-email group-label:"Welcome email"
# dex:explanation text:"Check the welcome email fields before calling Resend."
class _PrepareRequest(dex.Step[WelcomeEmailRequest]):
    def get_step_type(self) -> str:
        return "PrepareRequest"

    def execute(self, context: dex.Context, input: WelcomeEmailRequest) -> dex.StepDecision:
        if not input.from_address.strip() or not input.to or not input.subject.strip() or not input.html:
            return dex.force_fail("from, to, subject, and html are required")
        return dex.go_to(dex.StepRef(_SEND_STEP_TYPE), input)


# dex:group group-id:welcome-email group-label:"Welcome email"
# dex:explanation text:"Record the Resend email ID and complete the Flow."
class _CompleteRequest(dex.Step[MutationResult[SentEmail]]):
    def get_step_type(self) -> str:
        return "CompleteRequest"

    def execute(self, context: dex.Context, input: MutationResult[SentEmail]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class WelcomeEmailFlow(dex.Flow[WelcomeEmailRequest]):
    """Send one welcome email through the static resend connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("welcome email requires the static resend connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRequest()
        self._send = new_send_email_step(
            step_type=_SEND_STEP_TYPE,
            step_input_type=WelcomeEmailRequest,
            annotations={
                "groupId": "resend",
                "groupLabel": "Resend",
                "explanation": "Send the welcome email through Resend.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_send_email_input,
            sent=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[WelcomeEmailRequest]:
        """Register request check, provider send, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._send, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Resend email identifier."""
        return dex.PersistenceSchema.of(_OUTCOME)
