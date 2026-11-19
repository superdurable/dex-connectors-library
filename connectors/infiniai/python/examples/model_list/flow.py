# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Infini AI model catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.infiniai import (
    Connection,
    ListModelsInput,
    ListModelsOutput,
    new_list_models_step,
)

CONNECTION_NAME = "infiniai"
_RESULT = dex.Attribute("infiniai-model-list", ListModelsOutput)


def map_to_operation_input(request: ListModelsInput) -> ListModelsInput:
    """Forward the empty model-list request."""
    return request


# dex:group group-id:infiniai group-label:"Infini AI"
# dex:explanation text:"Start the Infini AI model-list request."
class _StartListModels(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartInfiniAIModelList"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallInfiniAI"), input)


# dex:group group-id:infiniai group-label:"Infini AI"
# dex:explanation text:"Persist available model metadata and complete the Flow."
class _CompleteListModels(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteInfiniAIModelList"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ModelListFlow(dex.Flow[ListModelsInput]):
    """List models through the static Infini AI connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ModelListFlow requires the static Infini AI connection")
        self._start = _StartListModels()
        self._complete = _CompleteListModels()
        self._call = new_list_models_step(
            step_type="CallInfiniAI",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "infiniai",
                "groupLabel": "Infini AI",
                "explanation": "List model metadata visible to the GenStudio API Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable model-list Flow identity."""
        return "INFINIAIModelList"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the model-list Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded model-list result."""
        return dex.PersistenceSchema.of(_RESULT)
