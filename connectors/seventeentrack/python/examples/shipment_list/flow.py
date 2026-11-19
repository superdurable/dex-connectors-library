# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable SeventeenTrack model catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.seventeentrack import (
    Connection,
    ListTrackedShipmentsInput,
    TrackedShipmentPage,
    new_list_tracked_shipments_step,
)

CONNECTION_NAME = "seventeentrack"
_RESULT = dex.Attribute("seventeentrack-provider query", TrackedShipmentPage)


def map_to_operation_input(request: ListTrackedShipmentsInput) -> ListTrackedShipmentsInput:
    """Forward the provider query request."""
    return request


# dex:group group-id:seventeentrack group-label:"SeventeenTrack"
# dex:explanation text:"Start the SeventeenTrack provider query request."
class _StartListModels(dex.Step[ListTrackedShipmentsInput]):
    def get_step_type(self) -> str:
        return "StartSeventeenTrackModelList"

    def execute(self, context: dex.Context, input: ListTrackedShipmentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSeventeenTrack"), input)


# dex:group group-id:seventeentrack group-label:"SeventeenTrack"
# dex:explanation text:"Persist available safe provider data and complete the Flow."
class _CompleteListModels(dex.Step[QueryResult[TrackedShipmentPage]]):
    def get_step_type(self) -> str:
        return "CompleteSeventeenTrackModelList"

    def execute(self, context: dex.Context, input: QueryResult[TrackedShipmentPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListTrackedShipmentsFlow(dex.Flow[ListTrackedShipmentsInput]):
    """List provider data through the static SeventeenTrack connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ListTrackedShipmentsFlow requires the static SeventeenTrack connection")
        self._start = _StartListModels()
        self._complete = _CompleteListModels()
        self._call = new_list_tracked_shipments_step(
            step_type="CallSeventeenTrack",
            step_input_type=ListTrackedShipmentsInput,
            annotations={
                "groupId": "seventeentrack",
                "groupLabel": "SeventeenTrack",
                "explanation": "List safe provider data visible to the provider API Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable provider query Flow identity."""
        return "SEVENTEENTRACKModelList"

    def get_steps(self) -> dex.StepList[ListTrackedShipmentsInput]:
        """Register the provider query Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded provider query result."""
        return dex.PersistenceSchema.of(_RESULT)
