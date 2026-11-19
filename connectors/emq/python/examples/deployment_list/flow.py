# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List EMQX Cloud deployments through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.emq import (
    Connection,
    ListDeploymentsInput,
    ListDeploymentsOutput,
    new_list_deployments_step,
)

CONNECTION_NAME = "emq"
_OUTCOME = dex.Attribute("emq-deployment-list-outcome", ListDeploymentsOutput)


def map_to_operation_input(input: ListDeploymentsInput) -> ListDeploymentsInput:
    """Pass the empty request to the provider Query."""
    return input


# dex:group group-id:deployment-list group-label:"Deployment list"
# dex:explanation text:"Start the EMQX Cloud deployment lookup."
class _StartRequest(dex.Step[ListDeploymentsInput]):
    def get_step_type(self) -> str:
        return "StartListDeployments"

    def execute(self, context: dex.Context, input: ListDeploymentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListDeployments"), input)


# dex:group group-id:deployment-list group-label:"Deployment list"
# dex:explanation text:"Persist the scoped deployments and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ListDeploymentsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListDeployments"

    def execute(self, context: dex.Context, input: QueryResult[ListDeploymentsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DeploymentListFlow(dex.Flow[ListDeploymentsInput]):
    """Read scoped deployments from the static `emq` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("deployment list requires the static emq connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_list_deployments_step(
            step_type="CallListDeployments",
            step_input_type=ListDeploymentsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "EMQX Cloud",
                "explanation": "Call EMQX Cloud's listDeployments operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "EMQXCloudDeploymentList"

    def get_steps(self) -> dex.StepList[ListDeploymentsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned deployment list."""
        return dex.PersistenceSchema.of(_OUTCOME)
