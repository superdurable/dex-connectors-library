# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Pinterest page Flow using the listPins Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pinterest import (
    Connection,
    ListPinsInput,
    PinsPage,
    new_list_pins_step,
)

FLOW_TYPE = "PinterestPinsListPython"
CONNECTION_NAME = "pinterest"
_CALL_STEP_TYPE = "CallListPins"
_RESULT = dex.Attribute("pinterest-python-page", PinsPage)


def map_to_operation_input(request: ListPinsInput) -> ListPinsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:pinterest group-label:"Pinterest"
# dex:explanation text:"Start the Pinterest request."
class _StartRequest(dex.Step[ListPinsInput]):
    def get_step_type(self) -> str:
        return "StartListPins"

    def execute(self, context: dex.Context, input: ListPinsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListPins"), input)


# dex:group group-id:pinterest group-label:"Pinterest"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PinsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListPins"

    def execute(self, context: dex.Context, input: QueryResult[PinsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PinsFlow(dex.Flow[ListPinsInput]):
    """Run one listPins read through the static pinterest connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the pins lookup Flow requires the static pinterest connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_pins_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListPinsInput,
            annotations={
                "groupId": "pinterest",
                "groupLabel": "Pinterest",
                "explanation": "Read one Pinterest page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PinterestPinsListPython"

    def get_steps(self) -> dex.StepList[ListPinsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
