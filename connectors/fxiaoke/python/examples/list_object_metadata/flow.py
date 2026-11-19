# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable FXiaoke CRM object metadata Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fxiaoke import (
    Connection,
    ListObjectMetadataInput,
    ListObjectMetadataOutput,
    new_list_object_metadata_step,
)

CONNECTION_NAME = "fxiaoke"
_RESULT = dex.Attribute("fxiaoke-object-metadata", ListObjectMetadataOutput)


def map_to_operation_input(request: ListObjectMetadataInput) -> ListObjectMetadataInput:
    """Forward the CRM object metadata request."""
    return request


# dex:group group-id:fxiaoke group-label:"FXiaoke"
# dex:explanation text:"Start one FXiaoke CRM object metadata query."
class _StartObjectMetadata(dex.Step[ListObjectMetadataInput]):
    def execute(self, context: dex.Context, input: ListObjectMetadataInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallFXiaoke"), input)


# dex:group group-id:fxiaoke group-label:"FXiaoke"
# dex:explanation text:"Persist CRM object names and availability before completion."
class _CompleteObjectMetadata(dex.Step[QueryResult[ListObjectMetadataOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListObjectMetadataOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ObjectMetadataFlow(dex.Flow[ListObjectMetadataInput]):
    """List CRM object metadata through the static FXiaoke connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ObjectMetadataFlow requires the static FXiaoke connection")
        self._start = _StartObjectMetadata()
        self._complete = _CompleteObjectMetadata()
        self._call = new_list_object_metadata_step(
            step_type="CallFXiaoke",
            step_input_type=ListObjectMetadataInput,
            annotations={
                "groupId": "fxiaoke",
                "groupLabel": "FXiaoke",
                "explanation": "List accessible CRM object names and active flags.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable CRM object metadata Flow identity."""
        return "FXiaokeObjectMetadata"

    def get_steps(self) -> dex.StepList[ListObjectMetadataInput]:
        """Register the CRM object metadata Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned CRM object metadata only."""
        return dex.PersistenceSchema.of(_RESULT)
