# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable IPDataCloud IP location Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ipdatacloud import (
    Connection,
    LookupIPLocationInput,
    LookupIPLocationOutput,
    new_lookup_ip_location_step,
)

CONNECTION_NAME = "ipdatacloud"
_RESULT = dex.Attribute("ipdatacloud-IP location", LookupIPLocationOutput)


def map_to_operation_input(request: LookupIPLocationInput) -> LookupIPLocationInput:
    """Forward the requested IP address."""
    return request


# dex:group group-id:ipdatacloud group-label:"IPDataCloud"
# dex:explanation text:"Start one IPDataCloud IP location query."
class _StartIPLocation(dex.Step[LookupIPLocationInput]):
    def execute(self, context: dex.Context, input: LookupIPLocationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallIPDataCloud"), input)


# dex:group group-id:ipdatacloud group-label:"IPDataCloud"
# dex:explanation text:"Persist coarse IP location before completion."
class _CompleteIPLocation(dex.Step[QueryResult[LookupIPLocationOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[LookupIPLocationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class IPLocationFlow(dex.Flow[LookupIPLocationInput]):
    """List one IP address through the static IPDataCloud connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("IPLocationFlow requires the static IPDataCloud connection")
        self._start = _StartIPLocation()
        self._complete = _CompleteIPLocation()
        self._call = new_lookup_ip_location_step(
            step_type="CallIPDataCloud",
            step_input_type=LookupIPLocationInput,
            annotations={
                "groupId": "ipdatacloud",
                "groupLabel": "IPDataCloud",
                "explanation": "Read coarse location for one public IPv4 address.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable IP location Flow identity."""
        return "IPDataCloudIPLocation"

    def get_steps(self) -> dex.StepList[LookupIPLocationInput]:
        """Register the IP location Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist coarse IP location only."""
        return dex.PersistenceSchema.of(_RESULT)
