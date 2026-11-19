# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Polyv channel-catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.polyv import (
    Connection,
    ListChannelsInput,
    ListChannelsOutput,
    new_list_channels_step,
)

CONNECTION_NAME = "polyv"
_RESULT = dex.Attribute("polyv-channel-catalog", ListChannelsOutput)


def map_to_operation_input(request: ListChannelsInput) -> ListChannelsInput:
    """Forward the channel list request to Polyv."""
    return request


# dex:group group-id:polyv group-label:"Polyv"
# dex:explanation text:"Start the Polyv channel-catalog request."
class _StartProfile(dex.Step[ListChannelsInput]):
    def get_step_type(self) -> str:
        return "StartPolyvChannelCatalog"

    def execute(self, context: dex.Context, input: ListChannelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPolyv"), input)


# dex:group group-id:polyv group-label:"Polyv"
# dex:explanation text:"Persist current profile and complete the Flow."
class _CompleteProfile(dex.Step[QueryResult[ListChannelsOutput]]):
    def get_step_type(self) -> str:
        return "CompletePolyvChannelCatalog"

    def execute(self, context: dex.Context, input: QueryResult[ListChannelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ChannelCatalogFlow(dex.Flow[ListChannelsInput]):
    """Read one channel catalog through the static Polyv connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ChannelCatalogFlow requires the static Polyv connection")
        self._start = _StartProfile()
        self._complete = _CompleteProfile()
        self._call = new_list_channels_step(
            step_type="CallPolyv",
            step_input_type=ListChannelsInput,
            annotations={
                "groupId": "polyv",
                "groupLabel": "Polyv",
                "explanation": "Read Polyv channel IDs.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable channel-catalog Flow identity."""
        return "PolyvChannelCatalog"

    def get_steps(self) -> dex.StepList[ListChannelsInput]:
        """Register the channel catalog Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the channel-catalog result."""
        return dex.PersistenceSchema.of(_RESULT)
