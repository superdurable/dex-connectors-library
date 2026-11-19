# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Moxin listModels Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.moxin import (
    Connection,
    ListModelsInput,
    ModelPage,
    new_list_models_step,
)

CONNECTION_NAME = "moxin"
_RESULT = dex.Attribute("moxin-result", ModelPage)


def map_to_operation_input(request: ListModelsInput) -> ListModelsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:moxin group-label:"Moxin"
# dex:explanation text:"Start the Moxin query."
class _StartQuery(dex.Step[ListModelsInput]):
    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:moxin group-label:"Moxin"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ModelPage]]):
    def execute(self, context: dex.Context, input: QueryResult[ModelPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListModelsFlow(dex.Flow[ListModelsInput]):
    """Run one Moxin query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Moxin Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_models_step(
            step_type="CallProvider",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "moxin",
                "groupLabel": "Moxin",
                "explanation": "Run Moxin listModels.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListModelsMoxin"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
