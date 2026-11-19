# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Siliconflow provider data in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.siliconflow import Connection, ListModelsInput, ListModelsOutput, new_list_models_step

CONNECTION_NAME = "siliconflow"
_RESULT = dex.Attribute("siliconflow-query-result", ListModelsOutput)


def map_to_operation_input(input: ListModelsInput) -> ListModelsInput:
    """Pass the application Query input to the provider."""
    return input


# dex:group group-id:siliconflow group-label:"Siliconflow"
# dex:explanation text:"Start the Siliconflow Query."
class _StartQuery(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartSiliconflowQuery"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSiliconflow"), input)


# dex:group group-id:siliconflow group-label:"Siliconflow"
# dex:explanation text:"Persist the Siliconflow Query result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteSiliconflowQuery"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProviderQueryFlow(dex.Flow[ListModelsInput]):
    """Read the configured Siliconflow account through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProviderQueryFlow requires the static Siliconflow connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_models_step(
            step_type="CallSiliconflow",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "siliconflow",
                "groupLabel": "Siliconflow",
                "explanation": "Read Siliconflow account data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Siliconflow Query Flow identity."""
        return "SiliconflowProviderQuery"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider Query output."""
        return dex.PersistenceSchema.of(_RESULT)
