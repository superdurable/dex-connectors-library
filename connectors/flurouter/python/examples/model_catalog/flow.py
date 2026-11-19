# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read FluRouter model IDs through a statically named connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.flurouter import (
    Connection,
    ListModelsInput,
    ListModelsOutput,
    new_list_models_step,
)

CONNECTION_NAME = "flurouter"
model_catalog_result = dex.Attribute("flurouter-model-catalog-python", ListModelsOutput)


# dex:group group-id:models group-label:"Models"
# dex:explanation text:"Persist available model IDs and complete the Flow."
class RecordModelCatalog(dex.Step[QueryResult[ListModelsOutput]]):
    """Persist model IDs after provider success."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "RecordFluRouterModelsPython"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        """Store model IDs without the secret API Key."""
        model_catalog_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class ModelCatalogFlow(dex.Flow[ListModelsInput]):
    """Read available model IDs using the named FluRouter connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("model Flow requires the static FluRouter connection")
        self.record = RecordModelCatalog()
        self.lookup = new_list_models_step(
            step_type="ListFluRouterModelsPython",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "models",
                "groupLabel": "Models",
                "explanation": "Read model IDs available to this FluRouter Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the stable model catalog Flow identity."""
        return "FluRouterModelCatalogPython"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the provider and completion Steps."""
        return dex.StepList.start_step(self.lookup).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable model ID list."""
        return dex.PersistenceSchema.of(model_catalog_result)
