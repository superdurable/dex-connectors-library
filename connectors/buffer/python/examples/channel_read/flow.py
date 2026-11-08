# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Buffer channel lookup Flow using getChannel."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.buffer import (
    ChannelProfile,
    Connection,
    GetChannelInput,
    new_get_channel_step,
)

FLOW_TYPE = "BufferChannelReadPython"
CONNECTION_NAME = "buffer"
_CALL_STEP_TYPE = "CallBuffer"
_RESULT = dex.Attribute("buffer-channel-read", ChannelProfile)


def map_to_operation_input(request: GetChannelInput) -> GetChannelInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:buffer group-label:"Buffer"
# dex:explanation text:"Start the Buffer request."
class _StartRequest(dex.Step[GetChannelInput]):
    def get_step_type(self) -> str:
        return "StartBufferRequest"

    def execute(self, context: dex.Context, input: GetChannelInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallBuffer"), input)


# dex:group group-id:buffer group-label:"Buffer"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ChannelProfile]]):
    def get_step_type(self) -> str:
        return "CompleteBufferRequest"

    def execute(self, context: dex.Context, input: QueryResult[ChannelProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ChannelReadFlow(dex.Flow[GetChannelInput]):
    """Read channel metadata through the static buffer connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("channel lookup requires the static buffer connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_channel_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetChannelInput,
            annotations={
                "groupId": "buffer",
                "groupLabel": "Buffer",
                "explanation": "Read the connected channel name and social service from Buffer.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "BufferChannelReadPython"

    def get_steps(self) -> dex.StepList[GetChannelInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
