# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Juxingyi model catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.huo15 import (
    Connection,
    ListModelsInput,
    ListModelsOutput,
    new_list_models_step,
)

CONNECTION_NAME = "huo15"
_RESULT = dex.Attribute("huo15-model-list", ListModelsOutput)


def map_to_operation_input(request: ListModelsInput) -> ListModelsInput:
    """Forward the empty model-list request."""
    return request


# dex:group group-id:huo15 group-label:"Juxingyi"
# dex:explanation text:"Start the Juxingyi model-list request."
class _StartListModels(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartHuo15ModelList"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallHuo15"), input)


# dex:group group-id:huo15 group-label:"Juxingyi"
# dex:explanation text:"Persist available model IDs and complete the Flow."
class _CompleteListModels(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteHuo15ModelList"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ModelListFlow(dex.Flow[ListModelsInput]):
    """List models through the static Juxingyi connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ModelListFlow requires the static Juxingyi connection")
        self._start = _StartListModels()
        self._complete = _CompleteListModels()
        self._call = new_list_models_step(
            step_type="CallHuo15",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "huo15",
                "groupLabel": "Juxingyi",
                "explanation": "List model IDs visible to the Juxingyi Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable model-list Flow identity."""
        return "HUO15ModelList"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the model-list Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded model-list result."""
        return dex.PersistenceSchema.of(_RESULT)
