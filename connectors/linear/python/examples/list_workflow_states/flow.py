# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List the states that an application can use to move Linear issues."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.linear import (
    Connection,
    ListWorkflowStatesInput,
    ListWorkflowStatesOutput,
    new_list_workflow_states_step,
)

CONNECTION_NAME = "linear"
FLOW_TYPE = "LinearListWorkflowStatesPython"
_STATES = dex.Attribute("linear-workflow-states", ListWorkflowStatesOutput)


# dex:group group-id:workflow-states group-label:"Workflow states"
# dex:explanation text:"Persist the team states returned by Linear."
class CompleteWorkflowStates(dex.Step[QueryResult[ListWorkflowStatesOutput]]):
    """Persist and return the team's workflow states."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteWorkflowStates"

    def execute(self, context: dex.Context, input: QueryResult[ListWorkflowStatesOutput]) -> dex.StepDecision:
        """Save the safe provider result and complete the Flow."""
        _STATES.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListWorkflowStatesFlow(dex.Flow[ListWorkflowStatesInput]):
    """Read one team's workflow states through the static Linear connection."""

    def __init__(self, connection: Connection) -> None:
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("workflow state Flow requires the static Linear connection")
        self.complete = CompleteWorkflowStates()
        self.list_states = new_list_workflow_states_step(
            step_type="ListLinearWorkflowStates",
            step_input_type=ListWorkflowStatesInput,
            annotations={
                "groupId": "workflow-states",
                "groupLabel": "Workflow states",
                "explanation": "Read the team's available Linear workflow states.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "LinearListWorkflowStatesPython"

    def get_steps(self) -> dex.StepList[ListWorkflowStatesInput]:
        """Register the provider Query and completion Step."""
        return dex.StepList.start_step(self.list_states).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the persisted states Attribute."""
        return dex.PersistenceSchema.of(_STATES)
