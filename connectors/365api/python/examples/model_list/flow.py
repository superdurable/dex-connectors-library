# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable 365 API model catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors._365api import (
    Connection,
    ListModelsInput,
    ListModelsOutput,
    new_list_models_step,
)

CONNECTION_NAME = "hby365"
_RESULT = dex.Attribute("hby365-model-list", ListModelsOutput)


def map_to_operation_input(request: ListModelsInput) -> ListModelsInput:
    """Forward the empty model-list request."""
    return request


# dex:group group-id:hby365 group-label:"365 API"
# dex:explanation text:"Start the 365 API model-list request."
class _StartListModels(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "Start365APIModelList"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("Call365API"), input)


# dex:group group-id:hby365 group-label:"365 API"
# dex:explanation text:"Persist available model IDs and complete the Flow."
class _CompleteListModels(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "Complete365APIModelList"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ModelListFlow(dex.Flow[ListModelsInput]):
    """List models through the static 365 API connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ModelListFlow requires the static 365 API connection")
        self._start = _StartListModels()
        self._complete = _CompleteListModels()
        self._call = new_list_models_step(
            step_type="Call365API",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "hby365",
                "groupLabel": "365 API",
                "explanation": "List model IDs visible to the 365 API Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable model-list Flow identity."""
        return "HBY365ModelList"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the model-list Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded model-list result."""
        return dex.PersistenceSchema.of(_RESULT)
