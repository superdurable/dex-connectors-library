# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ZEGO room-count Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zego import (
    Connection,
    GetRoomUserCountInput,
    GetRoomUserCountOutput,
    new_get_room_user_count_step,
)

CONNECTION_NAME = "zego"
_RESULT = dex.Attribute("zego-room-count", GetRoomUserCountOutput)


def map_to_operation_input(request: GetRoomUserCountInput) -> GetRoomUserCountInput:
    """Forward the ZIM room ID to ZEGO."""
    return request


# dex:group group-id:zego group-label:"ZEGO"
# dex:explanation text:"Start the ZEGO room-count request."
class _StartRoomCount(dex.Step[GetRoomUserCountInput]):
    def get_step_type(self) -> str:
        return "StartZEGOGetRoomUserCount"

    def execute(self, context: dex.Context, input: GetRoomUserCountInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallZEGO"), input)


# dex:group group-id:zego group-label:"ZEGO"
# dex:explanation text:"Persist current room count and complete the Flow."
class _CompleteRoomCount(dex.Step[QueryResult[GetRoomUserCountOutput]]):
    def get_step_type(self) -> str:
        return "CompleteZEGOGetRoomUserCount"

    def execute(self, context: dex.Context, input: QueryResult[GetRoomUserCountOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetRoomUserCountFlow(dex.Flow[GetRoomUserCountInput]):
    """Read one ZIM room count through the static ZEGO connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("GetRoomUserCountFlow requires the static ZEGO connection")
        self._start = _StartRoomCount()
        self._complete = _CompleteRoomCount()
        self._call = new_get_room_user_count_step(
            step_type="CallZEGO",
            step_input_type=GetRoomUserCountInput,
            annotations={
                "groupId": "zego",
                "groupLabel": "ZEGO",
                "explanation": "Read the current ZIM room member count.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable room-count Flow identity."""
        return "ZEGOGetRoomUserCount"

    def get_steps(self) -> dex.StepList[GetRoomUserCountInput]:
        """Register the room count Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the room-count result."""
        return dex.PersistenceSchema.of(_RESULT)
