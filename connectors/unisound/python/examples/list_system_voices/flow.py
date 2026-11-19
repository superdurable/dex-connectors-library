# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Unisound list system voices Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.unisound import (
    Connection,
    ListSystemVoicesInput,
    ListSystemVoicesOutput,
    new_list_system_voices_step,
)

CONNECTION_NAME = "unisound"
_RESULT = dex.Attribute("unisound-list-system-voices-result", ListSystemVoicesOutput)


def map_to_operation_input(request: ListSystemVoicesInput) -> ListSystemVoicesInput:
    """Forward the typed request to the provider Query."""
    return request


# dex:group group-id:unisound group-label:"Unisound"
# dex:explanation text:"Start the Unisound list system voices request."
class _StartQuery(dex.Step[ListSystemVoicesInput]):
    def get_step_type(self) -> str:
        return "StartUnisoundVoiceCatalog"

    def execute(self, context: dex.Context, input: ListSystemVoicesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallUnisoundVoiceCatalog"), input)


# dex:group group-id:unisound group-label:"Unisound"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[ListSystemVoicesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteUnisoundVoiceCatalog"

    def execute(self, context: dex.Context, input: QueryResult[ListSystemVoicesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class UnisoundVoiceCatalogFlow(dex.Flow[ListSystemVoicesInput]):
    """Run the list system voices Query with a static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("UnisoundVoiceCatalogFlow requires the static Unisound connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_system_voices_step(
            step_type="CallUnisoundVoiceCatalog",
            step_input_type=ListSystemVoicesInput,
            annotations={
                "groupId": "unisound",
                "groupLabel": "Unisound",
                "explanation": "List system voices with the configured Unisound connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable list system voices Flow identity."""
        return "UnisoundVoiceCatalog"

    def get_steps(self) -> dex.StepList[ListSystemVoicesInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded list system voices result."""
        return dex.PersistenceSchema.of(_RESULT)
