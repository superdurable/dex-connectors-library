# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Vercel lookup Flow using the listDeployments Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.vercel import Connection, DeploymentsPage, ListDeploymentsInput, new_list_deployments_step

FLOW_TYPE = "VercelLookup"
CONNECTION_NAME = "vercel"
_OPERATION_STEP_TYPE = "ListDeploymentsVercel"
_REQUEST = dex.Attribute("vercel-lookup-request", ListDeploymentsInput)
_OUTPUT = dex.Attribute("vercel-lookup-output", DeploymentsPage)


def map_to_operation_input(request: ListDeploymentsInput) -> ListDeploymentsInput:
    """List deployments for the requested team, or the personal account when blank."""
    return request


# dex:group group-id:vercel-lookup group-label:"Vercel lookup"
# dex:explanation text:"Record the lookup request before listing Vercel deployments."
class _StartLookup(dex.Step[ListDeploymentsInput]):
    def get_step_type(self) -> str:
        return "StartVercelLookup"

    def execute(self, context: dex.Context, input: ListDeploymentsInput) -> dex.StepDecision:
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:vercel-lookup group-label:"Vercel lookup"
# dex:explanation text:"Record the deployments and complete the Flow."
class _CompleteLookup(dex.Step[QueryResult[DeploymentsPage]]):
    def get_step_type(self) -> str:
        return "CompleteVercelLookup"

    def execute(self, context: dex.Context, input: QueryResult[DeploymentsPage]) -> dex.StepDecision:
        _OUTPUT.set(context, input.value)
        return dex.graceful_complete(input.value)


class VercelLookupFlow(dex.Flow[ListDeploymentsInput]):
    """List Vercel deployments through the static vercel connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Vercel lookup requires the static vercel connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._list = new_list_deployments_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=ListDeploymentsInput,
            annotations={
                "groupId": "vercel",
                "groupLabel": "Vercel",
                "explanation": "List deployments visible to a personal account or team.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListDeploymentsInput]:
        """Register the start, provider list, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and deployment page."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTPUT)
