# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable 51Tracking shipment-status-count Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fiftyonetracking import (
    Connection,
    GetShipmentStatusCountsInput,
    ShipmentStatusCounts,
    new_get_shipment_status_counts_step,
)

CONNECTION_NAME = "fiftyonetracking"
_RESULT = dex.Attribute("fiftyonetracking-status-counts", ShipmentStatusCounts)


def map_to_operation_input(request: GetShipmentStatusCountsInput) -> GetShipmentStatusCountsInput:
    """Forward the empty aggregate-count request."""
    return request


# dex:group group-id:fiftyonetracking group-label:"51Tracking"
# dex:explanation text:"Start the 51Tracking status-count query."
class _StartStatusCounts(dex.Step[GetShipmentStatusCountsInput]):
    def execute(self, context: dex.Context, input: GetShipmentStatusCountsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("Call51Tracking"), input)


# dex:group group-id:fiftyonetracking group-label:"51Tracking"
# dex:explanation text:"Persist aggregate shipment counts and complete the Flow."
class _CompleteStatusCounts(dex.Step[QueryResult[ShipmentStatusCounts]]):
    def execute(self, context: dex.Context, input: QueryResult[ShipmentStatusCounts]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class StatusCountsFlow(dex.Flow[GetShipmentStatusCountsInput]):
    """Query aggregate parcel counts through the static 51Tracking connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("StatusCountsFlow requires the static 51Tracking connection")
        self._start = _StartStatusCounts()
        self._complete = _CompleteStatusCounts()
        self._call = new_get_shipment_status_counts_step(
            step_type="Call51Tracking",
            step_input_type=GetShipmentStatusCountsInput,
            annotations={
                "groupId": "fiftyonetracking",
                "groupLabel": "51Tracking",
                "explanation": "Read non-personal shipment status totals from 51Tracking.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            counted=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable status-count Flow identity."""
        return "FiftyOneTrackingStatusCounts"

    def get_steps(self) -> dex.StepList[GetShipmentStatusCountsInput]:
        """Register the status-count Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist aggregate counts without individual tracking numbers."""
        return dex.PersistenceSchema.of(_RESULT)
