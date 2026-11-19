# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read kuaidi100 data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kuaidi100 import (
    Connection,
    QueryTrackingInput,
    QueryTrackingOutput,
    new_query_tracking_step,
)

CONNECTION_NAME = "kuaidi100"
_OUTCOME = dex.Attribute("kuaidi100-query_tracking-result", QueryTrackingOutput)


def map_to_operation_input(input: QueryTrackingInput) -> QueryTrackingInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:query-tracking group-label:"QueryTracking"
# dex:explanation text:"Start the kuaidi100 Query."
class _StartRequest(dex.Step[QueryTrackingInput]):
    def get_step_type(self) -> str:
        return "StartQueryTracking"

    def execute(self, context: dex.Context, input: QueryTrackingInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallQueryTracking"), input)


# dex:group group-id:query-tracking group-label:"QueryTracking"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[QueryTrackingOutput]]):
    def get_step_type(self) -> str:
        return "CompleteQueryTracking"

    def execute(self, context: dex.Context, input: QueryResult[QueryTrackingOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class QueryTrackingFlow(dex.Flow[QueryTrackingInput]):
    """Run queryTracking using the static `kuaidi100` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("kuaidi100 example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_query_tracking_step(
            step_type="CallQueryTracking",
            step_input_type=QueryTrackingInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "kuaidi100",
                "explanation": "Call the kuaidi100 queryTracking Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            retrieved=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "Kuaidi100QueryTracking"

    def get_steps(self) -> dex.StepList[QueryTrackingInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
