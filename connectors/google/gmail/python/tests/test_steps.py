# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Gmail Step registration through the public Dex registry."""

import dex
import pytest
from dex_connectors.sdk import MutationResult, StepAnnotations

from dex_connectors.google_gmail import (
    SendMessageInput,
    SendMessageOutput,
    new_send_message_step,
)
from dex_connectors.google_gmail.gmailmock import Mock


class Sent(dex.Step[MutationResult[SendMessageOutput]]):
    """Complete a test Flow after one accepted Gmail send."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[SendMessageOutput]
    ) -> dex.StepDecision:
        """Return the accepted message ID."""
        return dex.graceful_complete(input.value.message_id)


class SendFlow(dex.Flow[SendMessageInput]):
    """Register the operation-specific sendMessage Step as the start."""

    def __init__(self, send: dex.Step[SendMessageInput], sent: Sent) -> None:
        self.send = send
        self.sent = sent

    def get_steps(self) -> dex.StepList[SendMessageInput]:
        """Register both instances with stable Step names."""
        return dex.StepList.start_step(self.send).other_steps(self.sent)


def test_send_step_factory_registers_and_checks_static_connection_name() -> None:
    """The send factory binds the mock operation and rejects a mismatched name."""
    mock = Mock("support")
    sent = Sent()
    annotations = StepAnnotations("gmail", "Gmail", "Send one Gmail message.")
    send = new_send_message_step(
        step_type="SendEmail",
        step_input_type=SendMessageInput,
        annotations=annotations,
        connection=mock.connection,
        connection_name="support",
        map_to_operation_input=lambda value: value,
        sent=sent,
    )
    dex.Registry([SendFlow(send, sent)], allow_async_handlers=True)
    assert send.get_step_type() == "SendEmail"
    assert send.get_step_options().execute_retry is not None
    assert send.get_step_options().execute_durability == dex.StepDurability.ASYNC
    with pytest.raises(ValueError, match="ConnectionName"):
        new_send_message_step(
            step_type="SendEmail",
            step_input_type=SendMessageInput,
            annotations=annotations,
            connection=mock.connection,
            connection_name="another-name",
            map_to_operation_input=lambda value: value,
            sent=sent,
        )
