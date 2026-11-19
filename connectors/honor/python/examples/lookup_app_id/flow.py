# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Honor app ID lookup Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.honor import (
    Connection,
    LookupAppIDInput,
    LookupAppIDOutput,
    new_lookup_app_id_step,
)

CONNECTION_NAME = "honor"
_RESULT = dex.Attribute("honor-app-id-lookup", LookupAppIDOutput)


def map_to_operation_input(request: LookupAppIDInput) -> LookupAppIDInput:
    """Forward the requested app ID lookup."""
    return request


# dex:group group-id:honor group-label:"Honor"
# dex:explanation text:"Start one Honor app ID lookup query."
class _StartAppIDLookup(dex.Step[LookupAppIDInput]):
    def execute(self, context: dex.Context, input: LookupAppIDInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallHonor"), input)


# dex:group group-id:honor group-label:"Honor"
# dex:explanation text:"Persist the matched app ID before completion."
class _CompleteAppIDLookup(dex.Step[QueryResult[LookupAppIDOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[LookupAppIDOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AppIDLookupFlow(dex.Flow[LookupAppIDInput]):
    """Look up one owned Android app through the static Honor connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("AppIDLookupFlow requires the static Honor connection")
        self._start = _StartAppIDLookup()
        self._complete = _CompleteAppIDLookup()
        self._call = new_lookup_app_id_step(
            step_type="CallHonor",
            step_input_type=LookupAppIDInput,
            annotations={
                "groupId": "honor",
                "groupLabel": "Honor",
                "explanation": "Look up the owned Android app ID with Honor.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable app ID lookup Flow identity."""
        return "HonorAppIDLookup"

    def get_steps(self) -> dex.StepList[LookupAppIDInput]:
        """Register the app ID lookup Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider app ID lookup result only."""
        return dex.PersistenceSchema.of(_RESULT)
