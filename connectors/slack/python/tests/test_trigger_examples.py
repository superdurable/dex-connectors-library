# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the thread and button approval examples' filters, mappers, and bindings."""

from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

import dex_connectors.slack as slack
from examples.button_approval import flow as button
from examples.thread_approval import flow as thread

AT = datetime(2026, 1, 1, tzinfo=UTC)


def connection(name: str) -> slack.Connection:
    return slack.new_connection(
        slack.Client(slack.Config(), StaticCredentialProvider({})), ConnectionRef("slack", name)
    )


def message(
    timestamp: str, thread_timestamp: str, user: str = "U2", text: str = "Approved!"
) -> TriggerEvent[slack.MessageEvent]:
    return TriggerEvent("Ev1", AT, slack.MessageEvent("T1", "C1", timestamp, thread_timestamp, user, text))


def test_thread_filters_apply_the_saved_matchers_and_shapes() -> None:
    start = thread.new_start_trigger_filter(
        slack.ChannelThreadCreatedTriggerConfiguration("C1", slack.MessageMatcher("request"))
    )
    reply = thread.new_reply_trigger_filter(
        slack.ThreadReplyCreatedTriggerConfiguration("C1", slack.MessageMatcher("approved", ("U2",)))
    )
    assert start(message("1.0", "1.0", text="Please REQUEST approval"))
    assert not start(message("1.1", "1.0", text="request"))
    assert reply(message("1.1", "1.0"))
    assert not reply(message("1.1", "1.0", user="U7"))
    assert not reply(message("1.0", "1.0"))
    with pytest.raises(ValueError, match="at least one poster"):
        thread.new_reply_trigger_filter(slack.ThreadReplyCreatedTriggerConfiguration("C1"))
    root = message("1.0", "1.0", user="U1")
    assert (
        thread.resolve_flow_id(root)
        == thread.resolve_flow_id(message("1.1", "1.0"))
        == "slack-thread-approval-T1-C1-1.0"
    )
    assert thread.map_to_flow_input(root) == thread.ThreadApprovalInput("Ev1", "T1", "C1", "1.0")
    assert thread.map_to_record_thread_reply_input(message("1.1", "1.0")) == thread.RecordThreadReplyInput(
        "Ev1", "U2"
    )


def click(action_id: str, value: str = "1.0", user: str = "U2") -> TriggerEvent[slack.ButtonClickEvent]:
    return TriggerEvent(
        f"T1:C1:1.9:{action_id}:3.0",
        AT,
        slack.ButtonClickEvent("T1", "C1", "1.9", "1.0", user, action_id, value, "3.0"),
    )


def test_button_decisions_accept_the_first_click_on_the_posted_message_only() -> None:
    accepts = button.new_button_trigger_filter(slack.ButtonClickedTriggerConfiguration("C1", (), ("U2",)))
    assert accepts(click("approve")) and accepts(click("reject"))
    assert (
        not accepts(click("other"))
        and not accepts(click("approve", user="U7"))
        and not accepts(click("approve", ""))
    )
    assert button.resolve_button_flow_id(click("approve")) == "slack-button-approval-T1-C1-1.0"
    state = button.ApprovalState(
        button.ApprovalInput("Ev0", "T1", "C1", "1.0", "U1"), "waitingForDecision", "1.9"
    )
    decision = button.map_to_record_button_decision_input(click("reject"))
    decided, result = button.decide_button_click(state, decision)
    assert result == button.DecisionResult(True, False, "postingDecision")
    assert (decided.decision, decided.decider_user_id) == ("rejected", "U2")
    assert button.decide_button_click(decided, decision)[1] == button.DecisionResult(
        False, True, "postingDecision"
    )
    copied = button.RecordButtonDecisionInput("Ev9", "approve", "U2", "5.5")
    assert button.decide_button_click(state, copied) == (
        state,
        button.DecisionResult(False, False, "waitingForDecision"),
    )
    with pytest.raises(RuntimeError, match="still being posted"):
        button.decide_button_click(button.ApprovalState(state.input, "postingButtons"), decision)
    posted = button.map_to_buttons_input(state)
    assert slack.validate_post_button_message_input(posted) is None
    assert [(item.action_id, item.value, item.style) for item in posted.buttons] == [
        ("approve", "1.0", "primary"),
        ("reject", "1.0", "danger"),
    ]
    assert button.map_to_decision_input(decided).text == "Rejected by <@U2>."


@pytest.mark.parametrize(
    ("flow", "expected"),
    [
        (
            thread.ThreadApprovalFlow(connection(thread.CONNECTION_NAME)),
            {
                ("channelThreadCreated", "slack-thread-approval-start"): [
                    "/channelId",
                    "/threadTriggerMatcher/messageContains",
                    "/threadTriggerMatcher/posterUserIds",
                ],
                ("threadReplyCreated", "slack-thread-approval-reply"): [
                    "/channelId",
                    "/threadReplyMatcher/messageContains",
                    "/threadReplyMatcher/posterUserIds",
                ],
            },
        ),
        (
            button.ButtonApprovalFlow(connection(button.CONNECTION_NAME)),
            {
                ("channelThreadCreated", "slack-button-approval-start"): [
                    "/channelId",
                    "/threadTriggerMatcher/messageContains",
                    "/threadTriggerMatcher/posterUserIds",
                ],
                ("buttonClicked", "slack-button-approval-decision"): ["/channelId", "/clickerUserIds"],
            },
        ),
    ],
)
def test_flows_register_and_declare_bindings_with_guided_units(
    flow: thread.ThreadApprovalFlow | button.ButtonApprovalFlow, expected: dict[tuple[str, str], list[str]]
) -> None:
    dex.Registry([flow], allow_async_handlers=True)
    declared = {}
    for definition in flow.get_connector_trigger_bindings():
        assert definition.configuration_ui is not None
        for unit in definition.configuration_ui.units:
            assert len(unit.description) > 60, unit.id
        declared[(definition.definition.trigger.trigger_name, definition.binding_name)] = [
            item.json_pointer for unit in definition.configuration_ui.units for item in unit.bindings
        ]
        assert definition.connection_name == flow.get_connector_trigger_bindings()[0].connection_name
    assert declared == expected
