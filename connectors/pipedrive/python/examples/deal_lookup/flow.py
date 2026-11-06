# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one deal using the static Pipedrive connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pipedrive import Connection, CRMObject, GetObjectInput, new_get_object_step

CONNECTION_NAME = "pipedrive-crm"
_RESULT = dex.Attribute("pipedrive-deal-lookup-result", CRMObject)


# dex:group group-id:deal-lookup group-label:"Deal lookup"
# dex:explanation text:"Persist the complete Pipedrive deal and complete the Flow."
class _DealFound(dex.Step[QueryResult[CRMObject]]):
    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompletePipedriveDealLookup"

    def execute(self, context: dex.Context, input: QueryResult[CRMObject]) -> dex.StepDecision:
        """Persist the safe record and return it to the caller."""
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DealLookupFlow(dex.Flow[GetObjectInput]):
    """Fetch one complete Pipedrive deal by its decimal ID."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("deal lookup requires the static Pipedrive connection")
        self._found = _DealFound()
        self._get = new_get_object_step(
            step_type="GetPipedriveDeal",
            step_input_type=GetObjectInput,
            annotations={
                "groupId": "deal-lookup",
                "groupLabel": "Deal lookup",
                "explanation": "Read one complete deal from Pipedrive by ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self._found,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "PipedriveDealLookupPython"

    def get_steps(self) -> dex.StepList[GetObjectInput]:
        """Start with the provider Query and complete when found."""
        return dex.StepList.start_step(self._get).other_steps(self._found)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the returned deal Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
