# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A received Gmail root message starts a Flow, a received reply invokes its RPC, and the Flow replies."""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    MutationResult,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
    TriggerFilter,
)

from dex_connectors.google_gmail import (
    Connection,
    GetMessageInput,
    Message,
    MessageEvent,
    MessageMatcher,
    MessageReceivedTriggerConfiguration,
    ReplyReceivedTriggerConfiguration,
    ReplyToMessageInput,
    SendMessageOutput,
    define_message_received_trigger_binding,
    define_reply_received_trigger_binding,
    matches_message_event,
    new_get_message_step,
    new_reply_to_message_step,
)

CONNECTION_NAME = "gmail-inbox"
START_TRIGGER_BINDING = "gmail-thread-reply-start"
REPLY_TRIGGER_BINDING = "gmail-thread-reply-received"
FLOW_TYPE = "GmailThreadReplyPython"
REPLY_TEXT = "Processing complete."


@dataclass(frozen=True, slots=True)
class ThreadReplyInput:
    """Flow start input mapped from the root-message event."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    primary_email: str = field(metadata={"dex_json_name": "primaryEmail"})
    message_id: str = field(metadata={"dex_json_name": "messageId"})
    thread_id: str = field(metadata={"dex_json_name": "threadId"})


class ThreadReplyStatus:
    """Lifecycle of one Gmail thread; empty while the Flow reads its root message."""

    READING_ROOT = ""
    WAITING_FOR_REPLY = "waitingForReply"
    REPLYING = "replying"
    COMPLETED = "completed"


@dataclass(frozen=True, slots=True)
class ThreadState:
    """Durable thread state: identity, the root message, and the accepted reply."""

    input: ThreadReplyInput
    status: str = ""
    root_message: Message | None = field(
        default=None, metadata={"dex_json_name": "rootMessage", "dex_json_omit_default": True}
    )
    reply_event_id: str = field(
        default="", metadata={"dex_json_name": "replyEventId", "dex_json_omit_default": True}
    )
    reply_message_id: str = field(
        default="", metadata={"dex_json_name": "replyMessageId", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class ReceiveEmailReplyInput:
    """RPC input mapped from the reply event: only the event and message IDs."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    message_id: str = field(metadata={"dex_json_name": "messageId"})


@dataclass(frozen=True, slots=True)
class ReplyResult:
    """Whether the RPC accepted the reply, and the thread status after it."""

    accepted: bool
    duplicate: bool
    status: str


thread_state = dex.Attribute("gmail-thread-reply-state", ThreadState)
reply_result = dex.Attribute("gmail-thread-reply-result", MutationResult[SendMessageOutput])
_THREAD_LOCK = dex.StepOptions(execute_lock_attributes=(thread_state.lock(),))


# dex:group group-id:reply group-label:"Reply"
# dex:explanation text:"Persist the Gmail thread identity before reading its root message."
class InitializeThread(dex.Step[ThreadReplyInput]):
    """Persist the thread identity before reading its root message."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "InitializeThread"

    def get_step_options(self) -> dex.StepOptions:
        """Lock the thread state."""
        return _THREAD_LOCK

    def execute(self, context: dex.Context, input: ThreadReplyInput) -> dex.StepDecision:
        """Store the identity and read the root message."""
        thread_state.set(context, ThreadState(input))
        return dex.go_to(dex.StepRef("ReadReceivedEmail"), input)


# dex:group group-id:reply group-label:"Reply"
# dex:explanation text:"Store the received Gmail message before waiting for a reply Trigger."
class MessageLoaded(dex.Step[QueryResult[Message]]):
    """Store the root message before waiting for a reply."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "MessageLoaded"

    def get_step_options(self) -> dex.StepOptions:
        """Lock the thread state."""
        return _THREAD_LOCK

    def execute(self, context: dex.Context, input: QueryResult[Message]) -> dex.StepDecision:
        """Wait for the reply RPC."""
        state = thread_state.get(context)
        thread_state.set(
            context, replace(state, root_message=input.value, status=ThreadReplyStatus.WAITING_FOR_REPLY)
        )
        return dex.dead_end()


# dex:group group-id:gmail group-label:"Gmail"
# dex:explanation text:"Complete after Gmail confirms the thread reply was sent."
class ReplySent(dex.Step[MutationResult[SendMessageOutput]]):
    """Complete after Gmail confirms the reply."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "ReplySent"

    def get_step_options(self) -> dex.StepOptions:
        """Lock the thread state."""
        return _THREAD_LOCK

    def execute(self, context: dex.Context, input: MutationResult[SendMessageOutput]) -> dex.StepDecision:
        """Record completion."""
        state = replace(thread_state.get(context), status=ThreadReplyStatus.COMPLETED)
        thread_state.set(context, state)
        return dex.graceful_complete(state)


class GmailThreadReplyFlow(dex.Flow[ThreadReplyInput]):
    """A matching root message starts the Flow, a matching reply invokes the RPC, and the Flow replies."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Gmail connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"Gmail thread reply Flow requires the {CONNECTION_NAME} connection")
        self.initialize = InitializeThread()
        self.loaded = MessageLoaded()
        self.sent = ReplySent()
        self.read = new_get_message_step(
            step_type="ReadReceivedEmail",
            step_input_type=ThreadReplyInput,
            annotations={
                "groupId": "gmail",
                "groupLabel": "Gmail",
                "explanation": "Read the received Gmail message that started the Flow.",
            },
            connection=connection,
            connection_name="gmail-inbox",
            map_to_operation_input=map_to_get_message_input,
            read=self.loaded,
        )
        self.reply = new_reply_to_message_step(
            step_type="ReplyToReceivedEmail",
            step_input_type=ThreadState,
            annotations={
                "groupId": "gmail",
                "groupLabel": "Gmail",
                "explanation": "Reply after the received email Trigger invokes the typed RPC.",
            },
            connection=connection,
            connection_name="gmail-inbox",
            map_to_operation_input=map_to_reply_input,
            sent=self.sent,
            result_attribute=reply_result,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "GmailThreadReplyPython"

    def get_steps(self) -> dex.StepList[ThreadReplyInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self.initialize).other_steps(
            self.read, self.loaded, self.reply, self.sent
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the thread state and the reply result for operator inspection."""
        return dex.PersistenceSchema.of(thread_state, reply_result)

    # The analyzer requires a lone return statement with literal binding values.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_message_received_trigger_binding(
                connection_name="gmail-inbox",
                binding_name="gmail-thread-reply-start",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="query",
                            unit_id="searchQueryInput",
                            label="Gmail search query",
                            description="Enter Gmail search syntax such as label:inbox is:unread; leave "
                            "blank to inspect the most recent inbox messages.",
                            required=False,
                            bindings=(ConnectorUIBinding(port="query", json_pointer="/searchQuery"),),
                        ),
                        ConnectorUIUnit(
                            id="message",
                            unit_id="textInput",
                            label="Start message contains",
                            description="Enter plain text that the root message must contain; leave blank "
                            "to match any message content.",
                            required=False,
                            bindings=(
                                ConnectorUIBinding(
                                    port="text", json_pointer="/messageMatcher/messageContains"
                                ),
                            ),
                        ),
                        ConnectorUIUnit(
                            id="senders",
                            unit_id="emailListInput",
                            label="Allowed root senders",
                            description="Enter complete sender email addresses; leave blank to allow any "
                            "sender matched by the Gmail query.",
                            required=False,
                            bindings=(
                                ConnectorUIBinding(
                                    port="emails", json_pointer="/messageMatcher/senderEmails"
                                ),
                            ),
                        ),
                    )
                ),
            ),
            define_reply_received_trigger_binding(
                connection_name="gmail-inbox",
                binding_name="gmail-thread-reply-received",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="query",
                            unit_id="searchQueryInput",
                            label="Gmail search query",
                            description="Enter Gmail search syntax that can find thread replies; leave blank "
                            "to inspect recent inbox messages.",
                            required=False,
                            bindings=(ConnectorUIBinding(port="query", json_pointer="/searchQuery"),),
                        ),
                        ConnectorUIUnit(
                            id="message",
                            unit_id="textInput",
                            label="Reply message contains",
                            description="Enter plain text that a reply must contain; leave blank to match "
                            "any reply content.",
                            required=False,
                            bindings=(
                                ConnectorUIBinding(port="text", json_pointer="/replyMatcher/messageContains"),
                            ),
                        ),
                        ConnectorUIUnit(
                            id="senders",
                            unit_id="emailListInput",
                            label="Allowed reply senders",
                            description="Enter complete sender email addresses; leave blank to allow any "
                            "sender matched by the Gmail query.",
                            required=False,
                            bindings=(
                                ConnectorUIBinding(port="emails", json_pointer="/replyMatcher/senderEmails"),
                            ),
                        ),
                    )
                ),
            ),
        ]

    @dex.rpc(lock_attributes=(thread_state.lock(),))
    def receive_email_reply(
        self, context: dex.Context, input: ReceiveEmailReplyInput
    ) -> dex.RPCResult[ReplyResult]:
        """Accept the first matching reply once and schedule the completion reply.

        A repeated event ID is a duplicate. While the Flow still reads its root message, the RPC raises,
        so the Trigger keeps the reply pending and retries it on the next poll.
        """
        state = thread_state.get(context)
        if state.reply_event_id == input.event_id:
            return dex.RPCResult(ReplyResult(False, True, state.status))
        if state.status == ThreadReplyStatus.READING_ROOT:
            raise RuntimeError("Gmail thread is still reading its root message")
        if state.status != ThreadReplyStatus.WAITING_FOR_REPLY:
            return dex.RPCResult(ReplyResult(False, False, state.status))
        accepted = replace(
            state,
            status=ThreadReplyStatus.REPLYING,
            reply_event_id=input.event_id,
            reply_message_id=input.message_id,
        )
        thread_state.set(context, accepted)
        return dex.RPCResult(
            ReplyResult(True, False, accepted.status),
            next_steps=(dex.StepMovement.of(dex.StepRef("ReplyToReceivedEmail"), accepted),),
        )

    @dex.rpc(lock_attributes=(thread_state.lock(),))
    def get_thread_status(self, context: dex.Context) -> dex.RPCResult[ThreadState]:
        """Read the thread state."""
        return dex.RPCResult(thread_state.get(context))


def map_to_get_message_input(input: ThreadReplyInput) -> GetMessageInput:
    """Read the root message that started the Flow."""
    return GetMessageInput(input.message_id)


def map_to_reply_input(state: ThreadState) -> ReplyToMessageInput:
    """Reply to the accepted reply message in its thread."""
    return ReplyToMessageInput(state.reply_message_id, REPLY_TEXT)


def resolve_flow_id(event: TriggerEvent[MessageEvent]) -> str:
    """Build a stable, PII-safe Flow ID from the connection name and Gmail thread ID."""
    return f"gmail-thread-reply-{CONNECTION_NAME}-{event.payload.thread_id}"


def map_to_flow_input(event: TriggerEvent[MessageEvent]) -> ThreadReplyInput:
    """Map a root event to the Flow input."""
    payload = event.payload
    return ThreadReplyInput(event.id, payload.primary_email, payload.message_id, payload.thread_id)


def map_to_receive_email_reply_input(event: TriggerEvent[MessageEvent]) -> ReceiveEmailReplyInput:
    """Map a reply event to the RPC input."""
    return ReceiveEmailReplyInput(event.id, event.payload.message_id)


def new_start_trigger_filter(
    configuration: MessageReceivedTriggerConfiguration,
) -> TriggerFilter[MessageEvent]:
    """The application's root-message admission rule, built from the binding configuration."""
    return _new_message_trigger_filter(configuration.message_matcher, False)


def new_reply_trigger_filter(configuration: ReplyReceivedTriggerConfiguration) -> TriggerFilter[MessageEvent]:
    """The application's reply admission rule, built from the binding configuration."""
    return _new_message_trigger_filter(configuration.reply_matcher, True)


def _new_message_trigger_filter(
    matcher: MessageMatcher | None, is_reply_required: bool
) -> TriggerFilter[MessageEvent]:
    def accepts(event: TriggerEvent[MessageEvent]) -> bool:
        message = event.payload
        if not (event.id and message.primary_email and message.message_id and message.thread_id):
            return False
        return message.is_reply == is_reply_required and matches_message_event(matcher, message)

    return accepts
