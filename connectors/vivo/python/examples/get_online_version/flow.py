# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable vivo get online version Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.vivo import (
    Connection,
    GetOnlineVersionInput,
    GetOnlineVersionOutput,
    new_get_online_version_step,
)

CONNECTION_NAME = "vivo"
_RESULT = dex.Attribute("vivo-get-online-version-result", GetOnlineVersionOutput)


def map_to_operation_input(request: GetOnlineVersionInput) -> GetOnlineVersionInput:
    """Forward the typed request to the provider Query."""
    return request


# dex:group group-id:vivo group-label:"vivo"
# dex:explanation text:"Start the vivo get online version request."
class _StartQuery(dex.Step[GetOnlineVersionInput]):
    def get_step_type(self) -> str:
        return "StartVivoOnlineVersion"

    def execute(self, context: dex.Context, input: GetOnlineVersionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallVivoOnlineVersion"), input)


# dex:group group-id:vivo group-label:"vivo"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetOnlineVersionOutput]]):
    def get_step_type(self) -> str:
        return "CompleteVivoOnlineVersion"

    def execute(self, context: dex.Context, input: QueryResult[GetOnlineVersionOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class VivoOnlineVersionFlow(dex.Flow[GetOnlineVersionInput]):
    """Run the get online version Query with a static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("VivoOnlineVersionFlow requires the static vivo connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_online_version_step(
            step_type="CallVivoOnlineVersion",
            step_input_type=GetOnlineVersionInput,
            annotations={
                "groupId": "vivo",
                "groupLabel": "vivo",
                "explanation": "Get online version with the configured vivo connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable get online version Flow identity."""
        return "VivoOnlineVersion"

    def get_steps(self) -> dex.StepList[GetOnlineVersionInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded get online version result."""
        return dex.PersistenceSchema.of(_RESULT)
