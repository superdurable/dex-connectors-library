# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A root message starts the Flow, which replies with buttons and completes on the first decider's click."""

import dataclasses
from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    MutationResult,
    TriggerBindingDefinition,
    TriggerEvent,
    TriggerFilter,
)

from dex_connectors.slack import (
    BUTTON_STYLE_DANGER,
    BUTTON_STYLE_PRIMARY,
    UI_CHANNEL_PICKER_PORT_CHANNEL_ID,
    UI_MEMBER_PICKER_PORT_MEMBER_IDS,
    UI_TEXT_INPUT_PORT_TEXT,
    UI_UNIT_CHANNEL_PICKER,
    UI_UNIT_MEMBER_PICKER,
    UI_UNIT_TEXT_INPUT,
    ButtonClickedTriggerConfiguration,
    ButtonClickEvent,
    ChannelThreadCreatedTriggerConfiguration,
    Connection,
    MessageButton,
    MessageEvent,
    PostButtonMessageInput,
    PostMessageOutput,
    PostThreadReplyInput,
    define_button_clicked_trigger_binding,
    define_channel_thread_created_trigger_binding,
    new_post_button_message_step,
    new_post_thread_reply_step,
    validate_button_clicked_trigger_configuration,
    validate_channel_thread_created_trigger_configuration,
)

CONNECTION_NAME = "slack-button-approval"
START_TRIGGER_BINDING = "slack-button-approval-start"
BUTTON_TRIGGER_BINDING = "slack-button-approval-decision"
APPROVE_ACTION_ID = "approve"
"""Action ID of the Approve button; a code constant, so a user never types it."""
REJECT_ACTION_ID = "reject"
"""Action ID of the Reject button; a code constant, so a user never types it."""
APPROVAL_REQUEST_TEXT = "Approve this request?"
_POST_BUTTONS_STEP_TYPE = "PostSlackApprovalButtons"
_POST_DECISION_STEP_TYPE = "PostSlackDecision"


@dataclass(frozen=True, slots=True)
class ApprovalInput:
    """Flow start input mapped from the root-message event."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    team_id: str = field(metadata={"dex_json_name": "teamId"})
    channel_id: str = field(metadata={"dex_json_name": "channelId"})
    thread_timestamp: str = field(metadata={"dex_json_name": "threadTimestamp"})
    requester_user_id: str = field(metadata={"dex_json_name": "requesterUserId"})


@dataclass(frozen=True, slots=True)
class ApprovalState:
    """Durable approval; status is postingButtons, waitingForDecision, postingDecision, or completed."""

    input: ApprovalInput
    status: str
    button_message_timestamp: str = field(default="", metadata={"dex_json_name": "buttonMessageTimestamp"})
    decision: str = ""
    decision_event_id: str = field(default="", metadata={"dex_json_name": "decisionEventId"})
    decider_user_id: str = field(default="", metadata={"dex_json_name": "deciderUserId"})


@dataclass(frozen=True, slots=True)
class RecordButtonDecisionInput:
    """RPC input mapped from a click: the event ID, button, clicker, and clicked message only."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    action_id: str = field(metadata={"dex_json_name": "actionId"})
    user_id: str = field(metadata={"dex_json_name": "userId"})
    message_timestamp: str = field(metadata={"dex_json_name": "messageTimestamp"})


@dataclass(frozen=True, slots=True)
class DecisionResult:
    """Whether the click decided the approval."""

    accepted: bool
    duplicate: bool
    status: str


approval_state = dex.Attribute("slack-python-button-approval-state", ApprovalState)
_APPROVAL_LOCK = dex.StepOptions(execute_lock_attributes=(approval_state.lock(),))


def decide_button_click(
    state: ApprovalState, input: RecordButtonDecisionInput
) -> tuple[ApprovalState, DecisionResult]:
    """Return the state after input and whether input decided the approval.

    It raises while the buttons are still being posted, so the Trigger retries the click until the Flow
    waits for it.
    """
    if state.decision_event_id == input.event_id:
        return state, DecisionResult(False, True, state.status)
    if state.status == "postingButtons":
        raise RuntimeError("Slack approval buttons are still being posted")
    # A button with the same action and value on another message, such as a copy, does not decide.
    if state.status != "waitingForDecision" or input.message_timestamp != state.button_message_timestamp:
        return state, DecisionResult(False, False, state.status)
    decided = dataclasses.replace(
        state,
        status="postingDecision",
        decision="rejected" if input.action_id == REJECT_ACTION_ID else "approved",
        decision_event_id=input.event_id,
        decider_user_id=input.user_id,
    )
    return decided, DecisionResult(True, False, decided.status)


# dex:group group-id:request group-label:"Approval request"
# dex:explanation text:"Persist the Slack thread identity before posting the approval buttons."
class InitializeApproval(dex.Step[ApprovalInput]):
    """Store the identity, then post the buttons."""

    def get_step_options(self) -> dex.StepOptions:
        """Lock the approval state."""
        return _APPROVAL_LOCK

    def execute(self, context: dex.Context, input: ApprovalInput) -> dex.StepDecision:
        """Store the identity and post the buttons."""
        state = ApprovalState(input, "postingButtons")
        approval_state.set(context, state)
        return dex.go_to(dex.StepRef(_POST_BUTTONS_STEP_TYPE), state)


# dex:group group-id:request group-label:"Approval request"
# dex:explanation text:"Record the posted button message, then wait for an Approve or Reject click."
class ApprovalButtonsPosted(dex.Step[MutationResult[PostMessageOutput]]):
    """Record the button message, then wait for the decision RPC."""

    def get_step_options(self) -> dex.StepOptions:
        """Lock the approval state."""
        return _APPROVAL_LOCK

    def execute(self, context: dex.Context, input: MutationResult[PostMessageOutput]) -> dex.StepDecision:
        """Wait for the decision RPC."""
        state = dataclasses.replace(
            approval_state.get(context),
            button_message_timestamp=input.value.message.timestamp,
            status="waitingForDecision",
        )
        approval_state.set(context, state)
        return dex.dead_end()


# dex:group group-id:decision group-label:"Decision"
# dex:explanation text:"Complete after Slack confirms the decision reply was posted."
class ApprovalDecisionPosted(dex.Step[MutationResult[PostMessageOutput]]):
    """Record completion."""

    def get_step_options(self) -> dex.StepOptions:
        """Lock the approval state."""
        return _APPROVAL_LOCK

    def execute(self, context: dex.Context, input: MutationResult[PostMessageOutput]) -> dex.StepDecision:
        """Complete with the final state."""
        state = dataclasses.replace(approval_state.get(context), status="completed")
        approval_state.set(context, state)
        return dex.graceful_complete(state)


def map_to_buttons_input(state: ApprovalState) -> PostButtonMessageInput:
    """Each button value carries the request's thread, so a click finds its Flow without Slack's report."""
    thread = state.input.thread_timestamp
    return PostButtonMessageInput(
        state.input.channel_id,
        thread,
        APPROVAL_REQUEST_TEXT,
        (
            MessageButton(APPROVE_ACTION_ID, "Approve", thread, BUTTON_STYLE_PRIMARY),
            MessageButton(REJECT_ACTION_ID, "Reject", thread, BUTTON_STYLE_DANGER),
        ),
    )


def map_to_decision_input(state: ApprovalState) -> PostThreadReplyInput:
    """Reply with the decision and who made it."""
    verb = "Rejected" if state.decision == "rejected" else "Approved"
    return PostThreadReplyInput(
        state.input.channel_id, state.input.thread_timestamp, f"{verb} by <@{state.decider_user_id}>."
    )


class ButtonApprovalFlow(dex.Flow[ApprovalInput]):
    """A root message starts the Flow, which replies with buttons and completes on a decider's click."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Slack connection to the connector Steps."""
        self.initialize = InitializeApproval()
        self.buttons_posted = ApprovalButtonsPosted()
        self.decision_posted = ApprovalDecisionPosted()
        self.post_buttons = new_post_button_message_step(
            step_type=_POST_BUTTONS_STEP_TYPE,
            step_input_type=ApprovalState,
            annotations={
                "groupId": "request",
                "groupLabel": "Approval request",
                "explanation": "Reply in the Slack thread with Approve and Reject buttons.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_buttons_input,
            sent=self.buttons_posted,
        )
        self.post_decision = new_post_thread_reply_step(
            step_type=_POST_DECISION_STEP_TYPE,
            step_input_type=ApprovalState,
            annotations={
                "groupId": "decision",
                "groupLabel": "Decision",
                "explanation": "Reply in the Slack thread with the decision and who made it.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_decision_input,
            sent=self.decision_posted,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "SlackButtonApprovalPython"

    def get_steps(self) -> dex.StepList[ApprovalInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self.initialize).other_steps(
            self.post_buttons, self.buttons_posted, self.post_decision, self.decision_posted
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the approval state."""
        return dex.PersistenceSchema.of(approval_state)

    # Dex Web configures these static bindings; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_channel_thread_created_trigger_binding(
                connection_name="slack-button-approval",
                binding_name="slack-button-approval-start",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "channel",
                            UI_UNIT_CHANNEL_PICKER,
                            "Approval channel",
                            "Select the joined Slack channel whose new root messages start an approval; the "
                            "picker "
                            "stores its stable channel ID, and the Flow replies there with Approve and "
                            "Reject buttons.",
                            True,
                            (ConnectorUIBinding(UI_CHANNEL_PICKER_PORT_CHANNEL_ID, "/channelId"),),
                        ),
                        ConnectorUIUnit(
                            "message",
                            UI_UNIT_TEXT_INPUT,
                            "Request message contains",
                            "Enter plain text that the root message must contain, such as request approval; "
                            "matching "
                            "ignores case, and blank matches any message content.",
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
                            "Members allowed to request",
                            "Select workspace members whose root messages may start an approval; leave "
                            "empty to "
                            "allow any member posting in the selected channel.",
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
            define_button_clicked_trigger_binding(
                connection_name="slack-button-approval",
                binding_name="slack-button-approval-decision",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "channel",
                            UI_UNIT_CHANNEL_PICKER,
                            "Approval channel",
                            "Select the same joined Slack channel as the request Trigger; clicks on Approve "
                            "and Reject "
                            "in other channels are ignored, and the picker stores the stable channel ID.",
                            True,
                            (ConnectorUIBinding(UI_CHANNEL_PICKER_PORT_CHANNEL_ID, "/channelId"),),
                        ),
                        ConnectorUIUnit(
                            "approvers",
                            UI_UNIT_MEMBER_PICKER,
                            "Members allowed to decide",
                            "Select the workspace members whose Approve or Reject click decides; a click by "
                            "anyone "
                            "else is ignored, and an empty selection lets any member who can see the "
                            "message decide.",
                            False,
                            (ConnectorUIBinding(UI_MEMBER_PICKER_PORT_MEMBER_IDS, "/clickerUserIds"),),
                        ),
                    )
                ),
            ),
        ]

    @dex.rpc(lock_attributes=(approval_state.lock(),))
    def record_button_decision(
        self, context: dex.Context, input: RecordButtonDecisionInput
    ) -> dex.RPCResult[DecisionResult]:
        """Record the first Approve or Reject click on the posted message and schedule the decision reply.

        Later clicks, a redelivery of the deciding click, and clicks on another message change nothing.
        """
        state, result = decide_button_click(approval_state.get(context), input)
        if not result.accepted:
            return dex.RPCResult(result)
        approval_state.set(context, state)
        return dex.RPCResult(
            result, next_steps=(dex.StepMovement.of(dex.StepRef(_POST_DECISION_STEP_TYPE), state),)
        )

    @dex.rpc(lock_attributes=(approval_state.lock(),))
    def get_approval_status(self, context: dex.Context) -> dex.RPCResult[ApprovalState]:
        """Read the approval state."""
        return dex.RPCResult(approval_state.get(context))


def _approval_flow_id(team_id: str, channel_id: str, thread_timestamp: str) -> str:
    return f"slack-button-approval-{team_id}-{channel_id}-{thread_timestamp}"


def resolve_flow_id(event: TriggerEvent[MessageEvent]) -> str:
    """Name the approval after the thread its root message started."""
    return _approval_flow_id(event.payload.team_id, event.payload.channel_id, event.payload.thread_timestamp)


def resolve_button_flow_id(event: TriggerEvent[ButtonClickEvent]) -> str:
    """Find the approval from the request's thread timestamp, which each button carries as its value."""
    return _approval_flow_id(event.payload.team_id, event.payload.channel_id, event.payload.value)


def map_to_flow_input(event: TriggerEvent[MessageEvent]) -> ApprovalInput:
    """Map a root event to the Flow input."""
    message = event.payload
    return ApprovalInput(
        event.id, message.team_id, message.channel_id, message.thread_timestamp, message.user_id
    )


def map_to_record_button_decision_input(event: TriggerEvent[ButtonClickEvent]) -> RecordButtonDecisionInput:
    """Map a click to the RPC input."""
    click = event.payload
    return RecordButtonDecisionInput(event.id, click.action_id, click.user_id, click.message_timestamp)


def new_start_trigger_filter(
    configuration: ChannelThreadCreatedTriggerConfiguration,
) -> TriggerFilter[MessageEvent]:
    """The application's root-message admission rule, built from the binding configuration."""
    validate_channel_thread_created_trigger_configuration(configuration)
    matcher = configuration.thread_trigger_matcher

    def accepts(event: TriggerEvent[MessageEvent]) -> bool:
        message = event.payload
        if (
            not event.id
            or not message.team_id
            or message.channel_id != configuration.channel_id
            or not message.user_id
            or message.thread_timestamp != message.timestamp
        ):
            return False
        if matcher.message_contains and matcher.message_contains.lower() not in message.text.lower():
            return False
        return not matcher.poster_user_ids or message.user_id in matcher.poster_user_ids

    return accepts


def new_button_trigger_filter(
    configuration: ButtonClickedTriggerConfiguration,
) -> TriggerFilter[ButtonClickEvent]:
    """The application's admission rule for Approve and Reject clicks, from the binding configuration."""
    validate_button_clicked_trigger_configuration(configuration)

    def accepts(event: TriggerEvent[ButtonClickEvent]) -> bool:
        click = event.payload
        if not event.id or not click.team_id or click.channel_id != configuration.channel_id:
            return False
        if (
            not click.value
            or not click.user_id
            or click.action_id not in (APPROVE_ACTION_ID, REJECT_ACTION_ID)
        ):
            return False
        return not configuration.clicker_user_ids or click.user_id in configuration.clicker_user_ids

    return accepts
