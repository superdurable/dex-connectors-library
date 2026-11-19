# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Maiyun response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.maiyun import (
    Connection,
    LookupIPLocationInput,
    LookupIPLocationOutput,
    new_lookup_ip_location_step,
)

CONNECTION_NAME = "maiyun"
_RESULT = dex.Attribute("maiyun-query-result", LookupIPLocationOutput)


def map_to_operation_input(input: LookupIPLocationInput) -> LookupIPLocationInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Maiyun provider Query."
class _StartQuery(dex.Step[LookupIPLocationInput]):
    def get_step_type(self) -> str:
        return "StartLookupIPLocationQuery"

    def execute(self, context: dex.Context, input: LookupIPLocationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("LookupIPLocationQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Maiyun Query result."
class _CompleteQuery(dex.Step[QueryResult[LookupIPLocationOutput]]):
    def get_step_type(self) -> str:
        return "CompleteLookupIPLocationQuery"

    def execute(self, context: dex.Context, input: QueryResult[LookupIPLocationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class MaiyunLookupIPLocationFlow(dex.Flow[LookupIPLocationInput]):
    """Run the Maiyun Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Maiyun Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_lookup_ip_location_step(
            step_type="LookupIPLocationQuery",
            step_input_type=LookupIPLocationInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Maiyun",
                "explanation": "Call Maiyun LookupIPLocation using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "MaiyunLookupIPLocation"

    def get_steps(self) -> dex.StepList[LookupIPLocationInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
