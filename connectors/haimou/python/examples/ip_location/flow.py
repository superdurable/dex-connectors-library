# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Haimou IP location Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.haimou import (
    Connection,
    LookupIPLocationInput,
    LookupIPLocationOutput,
    new_lookup_ip_location_step,
)

CONNECTION_NAME = "haimou"
_RESULT = dex.Attribute("haimou-IP location", LookupIPLocationOutput)


def map_to_operation_input(request: LookupIPLocationInput) -> LookupIPLocationInput:
    """Forward the requested IP location."""
    return request


# dex:group group-id:haimou group-label:"Haimou"
# dex:explanation text:"Start one Haimou IP location query."
class _StartIPLocation(dex.Step[LookupIPLocationInput]):
    def execute(self, context: dex.Context, input: LookupIPLocationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallHaimou"), input)


# dex:group group-id:haimou group-label:"Haimou"
# dex:explanation text:"Persist location fields before completion."
class _CompleteIPLocation(dex.Step[QueryResult[LookupIPLocationOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[LookupIPLocationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class IPLocationFlow(dex.Flow[LookupIPLocationInput]):
    """Read one public IPv4 location through the static Haimou connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("IPLocationFlow requires the static Haimou connection")
        self._start = _StartIPLocation()
        self._complete = _CompleteIPLocation()
        self._call = new_lookup_ip_location_step(
            step_type="CallHaimou",
            step_input_type=LookupIPLocationInput,
            annotations={
                "groupId": "haimou",
                "groupLabel": "Haimou",
                "explanation": "Read a public IPv4 location from Haimou.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable IP location Flow identity."""
        return "HaimouIPLocation"

    def get_steps(self) -> dex.StepList[LookupIPLocationInput]:
        """Register the IP location Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist coarse location and network fields only."""
        return dex.PersistenceSchema.of(_RESULT)
