# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Answer a customer from one static Outlook Mail connection."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.microsoft_outlook_mail import (
    Connection,
    GetMessageInput,
    Message,
    MessageFlags,
    MessageSummary,
    MovedMessage,
    MoveMessageInput,
    ReplyToMessageInput,
    SearchMessagesInput,
    SearchMessagesOutput,
    SendMessageInput,
    SentMessage,
    SetMessageFlagsInput,
    new_get_message_step,
    new_move_message_step,
    new_reply_to_message_step,
    new_search_messages_step,
    new_send_message_step,
    new_set_message_flags_step,
)

CONNECTION_NAME = "outlook-support-mailbox"
FLOW_TYPE = "OutlookSupportReplyPython"


@dataclass(frozen=True, slots=True)
class SupportRequest:
    """Customer and safe plain-text reply entered at Flow start."""

    customer_email: str = field(metadata={"dex_json_name": "customerEmail"})
    reply_text: str = field(metadata={"dex_json_name": "replyText"})
    new_message_subject: str = field(metadata={"dex_json_name": "newMessageSubject"})


@dataclass(frozen=True, slots=True)
class SupportOutcome:
    """Result of a threaded reply or a new customer message."""

    action: str
    sent_message_id: str = field(metadata={"dex_json_name": "sentMessageId"})
    customer_message_id: str = field(default="", metadata={"dex_json_name": "customerMessageId"})
    archived_folder_id: str = field(default="", metadata={"dex_json_name": "archivedFolderId"})


_REQUEST = dex.Attribute("outlook-python-support-request", SupportRequest)
_SOURCE_MESSAGE_ID = dex.Attribute("outlook-python-source-message-id", str)
_SENT_MESSAGE = dex.Attribute("outlook-python-sent-message", SentMessage)


def search_input(request: SupportRequest) -> SearchMessagesInput:
    """Search the Inbox by exact customer sender."""
    return SearchMessagesInput(folder="inbox", from_=request.customer_email, limit=10)


def message_input(summary: MessageSummary) -> GetMessageInput:
    """Read the chosen immutable Graph message ID."""
    return GetMessageInput(summary.id)


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Validate and retain the customer request before reading Outlook."
class PrepareSupportRequest(dex.Step[SupportRequest]):
    """Reject missing customer details before provider calls."""

    async def execute(self, context: dex.AsyncContext, input: SupportRequest) -> dex.StepDecision:  # type: ignore[override]
        if (
            not input.customer_email.strip()
            or not input.reply_text.strip()
            or not input.new_message_subject.strip()
        ):
            return dex.force_fail("Customer email, reply text, and new-message subject are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("SearchCustomerMessages"), input)


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Choose the newest matching Inbox message or send a new message."
class ChooseCustomerMessage(dex.Step[QueryResult[SearchMessagesOutput]]):
    """Take the newest search result when one exists."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: QueryResult[SearchMessagesOutput]
    ) -> dex.StepDecision:
        if input.value.messages:
            return dex.go_to(dex.StepRef("ReadCustomerMessage"), input.value.messages[0])
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("SendNewCustomerMessage"),
            SendMessageInput(
                to=(request.customer_email,), subject=request.new_message_subject, text=request.reply_text
            ),
        )


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Prepare a threaded reply to the message read from Outlook."
class PrepareThreadedReply(dex.Step[QueryResult[Message]]):
    """Retain the source ID before the durable reply send."""

    async def execute(self, context: dex.AsyncContext, input: QueryResult[Message]) -> dex.StepDecision:  # type: ignore[override]
        _SOURCE_MESSAGE_ID.set(context, input.value.id)
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("ReplyToCustomer"), ReplyToMessageInput(input.value.id, request.reply_text)
        )


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Retain the accepted reply and mark the source message read."
class PrepareReadFlag(dex.Step[MutationResult[SentMessage]]):
    """Carry the confirmed reply into the final result."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[SentMessage]
    ) -> dex.StepDecision:
        _SENT_MESSAGE.set(context, input.value)
        return dex.go_to(
            dex.StepRef("MarkCustomerMessageRead"),
            SetMessageFlagsInput(message_id=_SOURCE_MESSAGE_ID.get(context), is_read=True),
        )


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Move the answered message to the Archive folder."
class PrepareArchive(dex.Step[MutationResult[MessageFlags]]):
    """Archive only after the read flag is accepted."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[MessageFlags]
    ) -> dex.StepDecision:
        return dex.go_to(
            dex.StepRef("ArchiveCustomerMessage"),
            MoveMessageInput(_SOURCE_MESSAGE_ID.get(context), "archive"),
        )


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Complete with the sent reply and Archive folder identity."
class CompleteReply(dex.Step[MutationResult[MovedMessage]]):
    """Return the confirmed reply and destination folder."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[MovedMessage]
    ) -> dex.StepDecision:
        return dex.graceful_complete(
            SupportOutcome(
                "replied",
                _SENT_MESSAGE.get(context).message_id,
                _SOURCE_MESSAGE_ID.get(context),
                input.value.destination_folder_id,
            )
        )


# dex:group group-id:outlook-support group-label:"Outlook support"
# dex:explanation text:"Complete after Outlook accepts the new message."
class CompleteNewMessage(dex.Step[MutationResult[SentMessage]]):
    """Return the accepted new message ID."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[SentMessage]
    ) -> dex.StepDecision:
        return dex.graceful_complete(SupportOutcome("sentNewMessage", input.value.message_id))


class SupportReplyFlow(dex.Flow[SupportRequest]):
    """Search, read, reply, flag, move, or send through Outlook Mail."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("support reply requires the static Outlook Mail connection")
        self.prepare = PrepareSupportRequest()
        self.choose = ChooseCustomerMessage()
        self.prepare_reply = PrepareThreadedReply()
        self.prepare_flag = PrepareReadFlag()
        self.prepare_archive = PrepareArchive()
        self.complete_reply = CompleteReply()
        self.complete_new = CompleteNewMessage()
        self.search = new_search_messages_step(
            step_type="SearchCustomerMessages",
            step_input_type=SupportRequest,
            annotations={
                "groupId": "outlook-support",
                "groupLabel": "Outlook support",
                "explanation": "Search the Inbox for the customer's messages.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=search_input,
            searched=self.choose,
        )
        self.read = new_get_message_step(
            step_type="ReadCustomerMessage",
            step_input_type=MessageSummary,
            annotations={
                "groupId": "outlook-support",
                "groupLabel": "Outlook support",
                "explanation": "Read the newest customer message without marking it read.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=message_input,
            found=self.prepare_reply,
        )
        self.reply = new_reply_to_message_step(
            step_type="ReplyToCustomer",
            step_input_type=ReplyToMessageInput,
            annotations={
                "groupId": "outlook-support",
                "groupLabel": "Outlook support",
                "explanation": "Send one threaded reply through a marked draft.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            sent=self.prepare_flag,
        )
        self.flags = new_set_message_flags_step(
            step_type="MarkCustomerMessageRead",
            step_input_type=SetMessageFlagsInput,
            annotations={
                "groupId": "outlook-support",
                "groupLabel": "Outlook support",
                "explanation": "Mark the answered message read.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            updated=self.prepare_archive,
        )
        self.move = new_move_message_step(
            step_type="ArchiveCustomerMessage",
            step_input_type=MoveMessageInput,
            annotations={
                "groupId": "outlook-support",
                "groupLabel": "Outlook support",
                "explanation": "Move the answered message to Archive.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            moved=self.complete_reply,
        )
        self.send = new_send_message_step(
            step_type="SendNewCustomerMessage",
            step_input_type=SendMessageInput,
            annotations={
                "groupId": "outlook-support",
                "groupLabel": "Outlook support",
                "explanation": "Send a new message when no Inbox message matched.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            sent=self.complete_new,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "OutlookSupportReplyPython"

    def get_steps(self) -> dex.StepList[SupportRequest]:
        """Register every local and connector Step once."""
        return dex.StepList.start_step(self.prepare).other_steps(
            self.search,
            self.choose,
            self.read,
            self.prepare_reply,
            self.reply,
            self.prepare_flag,
            self.flags,
            self.prepare_archive,
            self.move,
            self.complete_reply,
            self.send,
            self.complete_new,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the request, source ID, and confirmed sent message."""
        return dex.PersistenceSchema.of(_REQUEST, _SOURCE_MESSAGE_ID, _SENT_MESSAGE)
