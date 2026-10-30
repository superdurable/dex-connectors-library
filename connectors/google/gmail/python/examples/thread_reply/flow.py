# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Gmail message and reply in its existing thread with Dex Steps."""

import dex
from dex_connectors.sdk import MutationResult, QueryResult, QueryStep, StepAnnotations

from dex_connectors.google_gmail import (
    Connection,
    GetMessageInput,
    Message,
    ReplyToMessageInput,
    SendMessageOutput,
    new_get_message_step,
    new_reply_to_message_step,
)

from .models import CONNECTION_NAME, ThreadReplyInput, ThreadReplyOutput

_REPLY_TEXT = dex.Attribute("gmail-thread-reply-text", str)


# dex:group group-id:gmail group-label:"Gmail"
# dex:explanation text:"Complete after Gmail confirms the threaded reply was sent."
class ReplySent(dex.Step[MutationResult[SendMessageOutput]]):
    """Complete after Gmail definitely accepts the threaded reply."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[SendMessageOutput]
    ) -> dex.StepDecision:
        """Return provider reply IDs without credentials or message bodies."""
        return dex.graceful_complete(
            ThreadReplyOutput(input.value.thread_id, input.value.message_id)
        )


# dex:group group-id:gmail group-label:"Gmail"
# dex:explanation text:"Route a successful source message read to the threaded reply."
class MessageRead(dex.Step[QueryResult[Message]]):
    """Send the reply after a successful source-message read."""

    def __init__(
        self, reply_step: dex.StepTarget[ReplyToMessageInput]
    ) -> None:
        self._reply_step = reply_step

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: QueryResult[Message]
    ) -> dex.StepDecision:
        """Reuse the source ID and the reply text stored at Flow admission."""
        return dex.go_to(
            self._reply_step,
            ReplyToMessageInput(input.value.message_id, _REPLY_TEXT.get(context)),
        )


# dex:group group-id:gmail group-label:"Gmail"
# dex:explanation text:"Validate the request and retain reply text before reading Gmail."
class PrepareReply(dex.Step[ThreadReplyInput]):
    """Persist the reply text before the Gmail query Step starts."""

    def __init__(self, read_step: dex.StepTarget[ThreadReplyInput]) -> None:
        self._read_step = read_step

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: ThreadReplyInput
    ) -> dex.StepDecision:
        """Reject blank input and carry the source ID into getMessage."""
        if not input.message_id.strip() or not input.reply_text.strip():
            return dex.force_fail("Gmail message ID and reply text are required")
        _REPLY_TEXT.set(context, input.reply_text)
        return dex.go_to(self._read_step, input)


class ThreadReplyFlow(dex.Flow[ThreadReplyInput]):
    """Demonstrate getMessage and replyToMessage with one static Gmail connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Gmail Flow requires the static gmail-inbox connection")
        self.reply_sent = ReplySent()
        self.reply = new_reply_to_message_step(
            step_type="ReplyToReceivedEmail",
            step_input_type=ReplyToMessageInput,
            annotations=StepAnnotations(
                group_id="gmail", group_label="Gmail",
                explanation="Reply in the received message's thread.",
            ),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            sent=self.reply_sent,
        )
        self.message_read = MessageRead(self.reply)
        self.read: QueryStep[ThreadReplyInput, GetMessageInput, Message] = new_get_message_step(
            step_type="ReadReceivedEmail",
            step_input_type=ThreadReplyInput,
            annotations=StepAnnotations(
                group_id="gmail", group_label="Gmail",
                explanation="Read the source Gmail message.",
            ),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: GetMessageInput(value.message_id),
            read=self.message_read,
        )
        self.prepare = PrepareReply(self.read)

    def get_steps(self) -> dex.StepList[ThreadReplyInput]:
        """Register each Step instance once under its stable Dex identity."""
        return dex.StepList.start_step(self.prepare).other_steps(
            self.read, self.message_read, self.reply, self.reply_sent
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the non-secret reply text required after the read Step."""
        return dex.PersistenceSchema.of(_REPLY_TEXT)
