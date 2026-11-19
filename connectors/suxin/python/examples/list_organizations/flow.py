# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Suxin listOrganizations Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.suxin import (
    Connection,
    ListOrganizationsInput,
    ListOrganizationsOutput,
    new_list_organizations_step,
)

CONNECTION_NAME = "suxin"
_RESULT = dex.Attribute("suxin-result", ListOrganizationsOutput)


def map_to_operation_input(request: ListOrganizationsInput) -> ListOrganizationsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:suxin group-label:"Suxin"
# dex:explanation text:"Start the Suxin query."
class _StartQuery(dex.Step[ListOrganizationsInput]):
    def execute(self, context: dex.Context, input: ListOrganizationsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:suxin group-label:"Suxin"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListOrganizationsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListOrganizationsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListOrganizationsFlow(dex.Flow[ListOrganizationsInput]):
    """Run one Suxin query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Suxin Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_organizations_step(
            step_type="CallProvider",
            step_input_type=ListOrganizationsInput,
            annotations={
                "groupId": "suxin",
                "groupLabel": "Suxin",
                "explanation": "Run Suxin listOrganizations.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListOrganizationsSuxin"

    def get_steps(self) -> dex.StepList[ListOrganizationsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
