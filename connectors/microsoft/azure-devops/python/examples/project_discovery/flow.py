# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable AzureDevOps project discovery Flow using the listProjects Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_azure_devops import (
    Connection,
    ListProjectsInput,
    Projects,
    new_list_projects_step,
)

FLOW_TYPE = "AzureDevOpsProjectDiscovery"
CONNECTION_NAME = "azure-devops"
_CALL_STEP_TYPE = "CallListProjects"
_RESULT = dex.Attribute("azure-devops-projects", Projects)


def map_to_operation_input(request: ListProjectsInput) -> ListProjectsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:azure-devops group-label:"Azure DevOps"
# dex:explanation text:"Start the Azure DevOps request."
class _StartRequest(dex.Step[ListProjectsInput]):
    def get_step_type(self) -> str:
        return "StartListProjects"

    def execute(self, context: dex.Context, input: ListProjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListProjects"), input)


# dex:group group-id:azure-devops group-label:"Azure DevOps"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[Projects]]):
    def get_step_type(self) -> str:
        return "CompleteListProjects"

    def execute(self, context: dex.Context, input: QueryResult[Projects]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProjectDiscoveryFlow(dex.Flow[ListProjectsInput]):
    """Run one listProjects read through the static azure-devops connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("project discovery requires the static azure-devops connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_projects_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListProjectsInput,
            annotations={
                "groupId": "azure-devops",
                "groupLabel": "Azure DevOps",
                "explanation": "List project identities visible to the connected Azure DevOps user.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AzureDevOpsProjectDiscovery"

    def get_steps(self) -> dex.StepList[ListProjectsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
