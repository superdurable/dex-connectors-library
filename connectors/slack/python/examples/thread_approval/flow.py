# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A root message starts the Flow, a matching reply approves it, and the Flow replies in the thread."""

import dataclasses
from dataclasses import dataclass, field

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

from dex_connectors.slack import (
    UI_CHANNEL_PICKER_PORT_CHANNEL_ID,
    UI_MEMBER_PICKER_PORT_MEMBER_IDS,
    UI_TEXT_INPUT_PORT_TEXT,
    UI_UNIT_CHANNEL_PICKER,
    UI_UNIT_MEMBER_PICKER,
    UI_UNIT_TEXT_INPUT,
    ChannelThreadCreatedTriggerConfiguration,
    Connection,
    ListThreadMessagesInput,
    ListThreadMessagesOutput,
    Message,
    MessageEvent,
    MessageMatcher,
    PostMessageOutput,
    PostThreadReplyInput,
    ThreadReplyCreatedTriggerConfiguration,
    define_channel_thread_created_trigger_binding,
    define_thread_reply_created_trigger_binding,
    new_list_thread_messages_step,
    new_post_thread_reply_step,
    validate_channel_thread_created_trigger_configuration,
    validate_thread_reply_created_trigger_configuration,
)

CONNECTION_NAME = "slack-workspace"
START_TRIGGER_BINDING = "slack-thread-approval-start"
REPLY_TRIGGER_BINDING = "slack-thread-approval-reply"
COMPLETION_TEXT = "Processing complete."
_READ_THREAD_STEP_TYPE = "ReadSlackThread"
_POST_COMPLETION_STEP_TYPE = "PostSlackCompletion"


@dataclass(frozen=True, slots=True)
class ThreadApprovalInput:
    """Flow start input mapped from the root-message event."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    team_id: str = field(metadata={"dex_json_name": "teamId"})
    channel_id: str = field(metadata={"dex_json_name": "channelId"})
    thread_timestamp: str = field(metadata={"dex_json_name": "threadTimestamp"})


@dataclass(frozen=True, slots=True)
class ThreadState:
    """Durable approval state; status is "", waitingForReply, postingReply, or completed."""

    input: ThreadApprovalInput
    messages: tuple[Message, ...] = ()
    status: str = ""
    reply_event_id: str = field(default="", metadata={"dex_json_name": "replyEventId"})
    reply_user_id: str = field(default="", metadata={"dex_json_name": "replyUserId"})


@dataclass(frozen=True, slots=True)
class RecordThreadReplyInput:
    """RPC input mapped from the reply event: only the event ID and the approver."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    user_id: str = field(metadata={"dex_json_name": "userId"})


@dataclass(frozen=True, slots=True)
class ReplyResult:
    """Whether the reply approved the thread."""

    accepted: bool
    duplicate: bool
    status: str


thread_state = dex.Attribute("slack-python-thread-approval-state", ThreadState)
_THREAD_LOCK = dex.StepOptions(execute_lock_attributes=(thread_state.lock(),))


# dex:group group-id:approval group-label:"Approval"
# dex:explanation text:"Persist the Slack thread identity before reading its messages."
class InitializeSlackThread(dex.Step[ThreadApprovalInput]):
    """Store the thread identity, then read the thread."""

    def get_step_options(self) -> dex.StepOptions:
        """Lock the approval state."""
        return _THREAD_LOCK

    def execute(self, context: dex.Context, input: ThreadApprovalInput) -> dex.StepDecision:
        """Store the identity and read the thread."""
        thread_state.set(context, ThreadState(input))
        return dex.go_to(dex.StepRef(_READ_THREAD_STEP_TYPE), input)


# dex:group group-id:approval group-label:"Approval"
# dex:explanation text:"Store the Slack thread messages before waiting for a matching reply Trigger."
class SlackThreadLoaded(dex.Step[QueryResult[ListThreadMessagesOutput]]):
    """Store the messages, then wait for the reply RPC."""

    def get_step_options(self) -> dex.StepOptions:
        """Lock the approval state."""
        return _THREAD_LOCK

    def execute(self, context: dex.Context, input: QueryResult[ListThreadMessagesOutput]) -> dex.StepDecision:
        """Wait for the reply RPC."""
        state = thread_state.get(context)
        thread_state.set(
            context, dataclasses.replace(state, messages=input.value.messages, status="waitingForReply")
        )
        return dex.dead_end()


# dex:group group-id:slack group-label:"Slack"
# dex:explanation text:"Complete after Slack confirms the thread reply was posted."
class SlackCompletionPosted(dex.Step[MutationResult[PostMessageOutput]]):
    """Record completion."""

    def get_step_options(self) -> dex.StepOptions:
        """Lock the approval state."""
        return _THREAD_LOCK

    def execute(self, context: dex.Context, input: MutationResult[PostMessageOutput]) -> dex.StepDecision:
        """Complete with the final state."""
        state = dataclasses.replace(thread_state.get(context), status="completed")
        thread_state.set(context, state)
        return dex.graceful_complete(state)


def map_to_read_thread_input(input: ThreadApprovalInput) -> ListThreadMessagesInput:
    """Read the first page of the new thread."""
    return ListThreadMessagesInput(input.channel_id, input.thread_timestamp, page_size=15)


def map_to_completion_input(state: ThreadState) -> PostThreadReplyInput:
    """Reply in the approved thread."""
    return PostThreadReplyInput(state.input.channel_id, state.input.thread_timestamp, COMPLETION_TEXT)


class ThreadApprovalFlow(dex.Flow[ThreadApprovalInput]):
    """A matching root message starts the Flow, a matching reply approves it, and the Flow replies."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Slack connection to the connector Steps."""
        self.initialize = InitializeSlackThread()
        self.loaded = SlackThreadLoaded()
        self.posted = SlackCompletionPosted()
        self.read_thread = new_list_thread_messages_step(
            step_type=_READ_THREAD_STEP_TYPE,
            step_input_type=ThreadApprovalInput,
            annotations={
                "groupId": "slack",
                "groupLabel": "Slack",
                "explanation": "Read the messages in the newly created Slack thread.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_read_thread_input,
            read=self.loaded,
        )
        self.post_completion = new_post_thread_reply_step(
            step_type=_POST_COMPLETION_STEP_TYPE,
            step_input_type=ThreadState,
            annotations={
                "groupId": "slack",
                "groupLabel": "Slack",
                "explanation": "Reply to the Slack thread after the reply Trigger invokes the RPC.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_completion_input,
            sent=self.posted,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "SlackThreadApprovalPython"

    def get_steps(self) -> dex.StepList[ThreadApprovalInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self.initialize).other_steps(
            self.read_thread, self.loaded, self.post_completion, self.posted
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the approval state."""
        return dex.PersistenceSchema.of(thread_state)

    # Dex Web configures these static bindings; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_channel_thread_created_trigger_binding(
                connection_name="slack-workspace",
                binding_name="slack-thread-approval-start",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "channel",
                            UI_UNIT_CHANNEL_PICKER,
                            "Approval channel",
                            "Select the joined Slack channel whose new root threads may start this Flow; "
                            "the picker "
                            "stores its stable channel ID.",
                            True,
                            (ConnectorUIBinding(UI_CHANNEL_PICKER_PORT_CHANNEL_ID, "/channelId"),),
                        ),
                        ConnectorUIUnit(
                            "message",
                            UI_UNIT_TEXT_INPUT,
                            "Start message contains",
                            "Enter plain text that the root thread message must contain, ignoring case; "
                            "leave blank "
                            "to match any message content.",
                            False,
                            (
                                ConnectorUIBinding(
                                    UI_TEXT_INPUT_PORT_TEXT, "/threadTriggerMatcher/messageContains"
                                ),
                            ),
                        ),
                        ConnectorUIUnit(
                            "members",
                            UI_UNIT_MEMBER_PICKER,
                            "Members allowed to start",
                            "Select workspace members allowed to start the Flow; leave empty to allow any "
                            "member "
                            "posting in the selected channel.",
                            False,
                            (
                                ConnectorUIBinding(
                                    UI_MEMBER_PICKER_PORT_MEMBER_IDS, "/threadTriggerMatcher/posterUserIds"
                                ),
                            ),
                        ),
                    )
                ),
            ),
            define_thread_reply_created_trigger_binding(
                connection_name="slack-workspace",
                binding_name="slack-thread-approval-reply",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "channel",
                            UI_UNIT_CHANNEL_PICKER,
                            "Approval channel",
                            "Select the same joined Slack channel used by the start Trigger; the picker "
                            "stores its "
                            "stable channel ID.",
                            True,
                            (ConnectorUIBinding(UI_CHANNEL_PICKER_PORT_CHANNEL_ID, "/channelId"),),
                        ),
                        ConnectorUIUnit(
                            "message",
                            UI_UNIT_TEXT_INPUT,
                            "Approval reply contains",
                            "Enter plain text that an approval reply must contain, such as approved, "
                            "ignoring case; "
                            "leave blank to match any reply content.",
                            False,
                            (
                                ConnectorUIBinding(
                                    UI_TEXT_INPUT_PORT_TEXT, "/threadReplyMatcher/messageContains"
                                ),
                            ),
                        ),
                        ConnectorUIUnit(
                            "approvers",
                            UI_UNIT_MEMBER_PICKER,
                            "Members allowed to approve",
                            "Select the workspace members whose thread replies count as approval; at least "
                            "one "
                            "approver is required.",
                            True,
                            (
                                ConnectorUIBinding(
                                    UI_MEMBER_PICKER_PORT_MEMBER_IDS, "/threadReplyMatcher/posterUserIds"
                                ),
                            ),
                        ),
                    )
                ),
            ),
        ]

    @dex.rpc(lock_attributes=(thread_state.lock(),))
    def record_thread_reply(
        self, context: dex.Context, input: RecordThreadReplyInput
    ) -> dex.RPCResult[ReplyResult]:
        """Accept the first matching reply once and schedule the completion; a repeat is a duplicate."""
        state = thread_state.get(context)
        if state.reply_event_id == input.event_id:
            return dex.RPCResult(ReplyResult(False, True, state.status))
        # The Flow is still reading its root message. The error keeps the reply pending, so the Trigger
        # retries it after the Flow starts waiting instead of consuming it as a no-op.
        if state.status == "":
            raise RuntimeError("Slack thread is still reading its root message")
        if state.status != "waitingForReply":
            return dex.RPCResult(ReplyResult(False, False, state.status))
        accepted = dataclasses.replace(
            state, status="postingReply", reply_event_id=input.event_id, reply_user_id=input.user_id
        )
        thread_state.set(context, accepted)
        return dex.RPCResult(
            ReplyResult(True, False, accepted.status),
            next_steps=(dex.StepMovement.of(dex.StepRef(_POST_COMPLETION_STEP_TYPE), accepted),),
        )

    @dex.rpc(lock_attributes=(thread_state.lock(),))
    def get_thread_status(self, context: dex.Context) -> dex.RPCResult[ThreadState]:
        """Read the approval state."""
        return dex.RPCResult(thread_state.get(context))


def resolve_flow_id(event: TriggerEvent[MessageEvent]) -> str:
    """Build the Flow ID from team, channel, and root timestamp, so the root and its replies share it."""
    message = event.payload
    return f"slack-thread-approval-{message.team_id}-{message.channel_id}-{message.thread_timestamp}"


def map_to_flow_input(event: TriggerEvent[MessageEvent]) -> ThreadApprovalInput:
    """Map a root event to the Flow input."""
    message = event.payload
    return ThreadApprovalInput(event.id, message.team_id, message.channel_id, message.thread_timestamp)


def map_to_record_thread_reply_input(event: TriggerEvent[MessageEvent]) -> RecordThreadReplyInput:
    """Map a reply event to the RPC input."""
    return RecordThreadReplyInput(event.id, event.payload.user_id)


def new_start_trigger_filter(
    configuration: ChannelThreadCreatedTriggerConfiguration,
) -> TriggerFilter[MessageEvent]:
    """The application's root-message admission rule, built from the binding configuration."""
    validate_channel_thread_created_trigger_configuration(configuration)
    return _MessageFilter(configuration.channel_id, configuration.thread_trigger_matcher, False)


def new_reply_trigger_filter(
    configuration: ThreadReplyCreatedTriggerConfiguration,
) -> TriggerFilter[MessageEvent]:
    """The application's reply admission rule, built from the binding configuration."""
    validate_thread_reply_created_trigger_configuration(configuration)
    return _MessageFilter(configuration.channel_id, configuration.thread_reply_matcher, True)


class _MessageFilter:
    def __init__(self, channel_id: str, matcher: MessageMatcher, is_reply_required: bool) -> None:
        self._channel_id = channel_id
        self._matcher = matcher
        self._is_reply_required = is_reply_required

    def __call__(self, event: TriggerEvent[MessageEvent]) -> bool:
        message = event.payload
        is_reply = message.thread_timestamp not in ("", message.timestamp)
        if (
            not event.id
            or not message.team_id
            or message.channel_id != self._channel_id
            or not message.thread_timestamp
            or not message.user_id
            or is_reply != self._is_reply_required
        ):
            return False
        contains = self._matcher.message_contains
        if contains and contains.lower() not in message.text.lower():
            return False
        posters = self._matcher.poster_user_ids
        return not posters or message.user_id in posters
