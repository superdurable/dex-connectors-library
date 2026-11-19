# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ALAPI IP lookup Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.alapi import (
    Connection,
    LookupIPLocationInput,
    LookupIPLocationOutput,
    new_lookup_ip_location_step,
)

CONNECTION_NAME = "alapi"
_RESULT = dex.Attribute("alapi-ip-location", LookupIPLocationOutput)


def map_to_operation_input(request: LookupIPLocationInput) -> LookupIPLocationInput:
    """Forward the public IPv4 address to ALAPI."""
    return request


# dex:group group-id:alapi group-label:"ALAPI"
# dex:explanation text:"Start the ALAPI IP location request."
class _StartLookup(dex.Step[LookupIPLocationInput]):
    def get_step_type(self) -> str:
        return "StartALAPILookup"

    def execute(self, context: dex.Context, input: LookupIPLocationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallALAPI"), input)


# dex:group group-id:alapi group-label:"ALAPI"
# dex:explanation text:"Persist the location and complete the Flow."
class _CompleteLookup(dex.Step[QueryResult[LookupIPLocationOutput]]):
    def get_step_type(self) -> str:
        return "CompleteALAPILookup"

    def execute(self, context: dex.Context, input: QueryResult[LookupIPLocationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class IPLocationFlow(dex.Flow[LookupIPLocationInput]):
    """Look up one public IPv4 address through the static ALAPI connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("IPLocationFlow requires the static ALAPI connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._call = new_lookup_ip_location_step(
            step_type="CallALAPI",
            step_input_type=LookupIPLocationInput,
            annotations={
                "groupId": "alapi",
                "groupLabel": "ALAPI",
                "explanation": "Read coarse location fields for one public IPv4 address.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable location Flow identity."""
        return "ALAPIIPLocation"

    def get_steps(self) -> dex.StepList[LookupIPLocationInput]:
        """Register the lookup Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the coarse location result."""
        return dex.PersistenceSchema.of(_RESULT)
