# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Xingyapi getInstanceOnlineStatus Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.xingyapi import (
    Connection,
    GetInstanceOnlineStatusInput,
    GetInstanceOnlineStatusOutput,
    new_get_instance_online_status_step,
)

CONNECTION_NAME = "xingyapi"
_RESULT = dex.Attribute("xingyapi-result", GetInstanceOnlineStatusOutput)


def map_to_operation_input(request: GetInstanceOnlineStatusInput) -> GetInstanceOnlineStatusInput:
    """Forward the provider request."""
    return request


# dex:group group-id:xingyapi group-label:"Xingyapi"
# dex:explanation text:"Start the Xingyapi query."
class _StartQuery(dex.Step[GetInstanceOnlineStatusInput]):
    def execute(self, context: dex.Context, input: GetInstanceOnlineStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:xingyapi group-label:"Xingyapi"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetInstanceOnlineStatusOutput]]):
    def execute(
        self, context: dex.Context, input: QueryResult[GetInstanceOnlineStatusOutput]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetInstanceOnlineStatusFlow(dex.Flow[GetInstanceOnlineStatusInput]):
    """Run one Xingyapi query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Xingyapi Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_instance_online_status_step(
            step_type="CallProvider",
            step_input_type=GetInstanceOnlineStatusInput,
            annotations={
                "groupId": "xingyapi",
                "groupLabel": "Xingyapi",
                "explanation": "Run Xingyapi getInstanceOnlineStatus.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            checked=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetInstanceOnlineStatusXingyapi"

    def get_steps(self) -> dex.StepList[GetInstanceOnlineStatusInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
