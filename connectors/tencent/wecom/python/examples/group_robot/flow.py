# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Post one WeCom group robot message through a durable Dex Flow."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.tencent_wecom import (
    Connection,
    SendGroupRobotMessageInput,
    SentGroupRobotMessage,
    new_send_group_robot_message_step,
)

CONNECTION_NAME = "wecom-robot"
_RESULT = dex.Attribute("wecom-python-group-robot-result", SentGroupRobotMessage)


def map_to_message_input(input: SendGroupRobotMessageInput) -> SendGroupRobotMessageInput:
    """Pass the non-secret message fields to the provider Step."""
    return input


# dex:group group-id:robot-send group-label:"Group robot send"
# dex:explanation text:"Start a fenced WeCom group robot post."
class _Start(dex.Step[SendGroupRobotMessageInput]):
    def get_step_type(self) -> str:
        return "StartWeComRobotSend"

    def execute(self, context: dex.Context, input: SendGroupRobotMessageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SendWeComRobotMessage"), input)


# dex:group group-id:robot-send group-label:"Group robot send"
# dex:explanation text:"Persist the confirmed group robot post."
class _Sent(dex.Step[MutationResult[SentGroupRobotMessage]]):
    def get_step_type(self) -> str:
        return "WeComRobotMessageSent"

    def execute(self, context: dex.Context, input: MutationResult[SentGroupRobotMessage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WeComGroupRobotFlow(dex.Flow[SendGroupRobotMessageInput]):
    """Post once to the group chat owning the static robot connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("WeCom robot Flow requires its static connection")
        self._start = _Start()
        self._sent = _Sent()
        self._send = new_send_group_robot_message_step(
            step_type="SendWeComRobotMessage",
            step_input_type=SendGroupRobotMessageInput,
            annotations={
                "groupId": "robot-send",
                "groupLabel": "Group robot send",
                "explanation": "Send a WeCom group robot message behind a single-dispatch Fence.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_message_input,
            sent=self._sent,
        )

    def get_flow_type(self) -> str:
        """Return the stable Worker Flow identity."""
        return "WeComPythonGroupRobotSend"

    def get_steps(self) -> dex.StepList[SendGroupRobotMessageInput]:
        """Register the start, provider, and confirmation Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._send, self._sent)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the confirmed secret-free result."""
        return dex.PersistenceSchema.of(_RESULT)
