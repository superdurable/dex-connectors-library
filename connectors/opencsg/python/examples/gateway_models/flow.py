# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read opencsg through one durable provider Query."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.opencsg import (
    Connection,
    ListGatewayModelsInput,
    ListGatewayModelsOutput,
    new_list_gateway_models_step,
)

CONNECTION_NAME = "opencsg"
_OUTCOME = dex.Attribute("opencsg-list_gateway_models-outcome", ListGatewayModelsOutput)


@dataclass(frozen=True)
class GatewayCatalogRequest:
    """Start an unfiltered read of the gateway catalog."""


def map_to_operation_input(input: GatewayCatalogRequest) -> ListGatewayModelsInput:
    """Request the catalog visible to this connection."""
    return ListGatewayModelsInput()


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the opencsg read with the configured connection."
class _Start(dex.Step[GatewayCatalogRequest]):
    def get_step_type(self) -> str:
        return "StartListGatewayModels"

    def execute(self, context: dex.Context, input: GatewayCatalogRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListGatewayModelsQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ListGatewayModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListGatewayModels"

    def execute(self, context: dex.Context, input: QueryResult[ListGatewayModelsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListGatewayModelsFlow(dex.Flow[GatewayCatalogRequest]):
    """Durably read opencsg with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("opencsg Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_gateway_models_step(
            step_type="ListGatewayModelsQuery",
            step_input_type=GatewayCatalogRequest,
            annotations={
                "groupId": "provider",
                "groupLabel": "opencsg",
                "explanation": "Call opencsg listGatewayModels.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "OpencsgListGatewayModels"

    def get_steps(self) -> dex.StepList[GatewayCatalogRequest]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
