# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List current API Key-scoped Dimleap model IDs through a Dex Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dimleap import Connection, ListModelsInput, ListModelsOutput, new_list_models_step

CONNECTION_NAME = "dimleap"
model_catalog_attribute = dex.Attribute("dimleap-model-catalog-python", ListModelsOutput)


# dex:group group-id:catalog group-label:"Model catalog"
# dex:explanation text:"Record current model IDs and complete the Flow."
class RecordModelCatalog(dex.Step[QueryResult[ListModelsOutput]]):
    """Persist only the count and first fifty available model IDs."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordDimleapModels"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        """Record non-secret model IDs from this API Key's current catalog."""
        model_catalog_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class ModelCatalogFlow(dex.Flow[ListModelsInput]):
    """Read model IDs available to the connected Dimleap API Key."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and recording Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("catalog Flow requires the static Dimleap connection")
        self.record = RecordModelCatalog()
        self.read = new_list_models_step(
            step_type="ListDimleapModels",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "catalog",
                "groupLabel": "Model catalog",
                "explanation": "GET the current Key-scoped model directory.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "DimleapModelCatalogPython"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the model catalog Attribute."""
        return dex.PersistenceSchema.of(model_catalog_attribute)
