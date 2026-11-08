# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Copper lead status discovery Flow using the listLeadStatuses Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.copper import (
    Connection,
    LeadStatusesList,
    ListLeadStatusesInput,
    new_list_lead_statuses_step,
)

FLOW_TYPE = "CopperLeadStatusDiscovery"
CONNECTION_NAME = "copper"
_CALL_STEP_TYPE = "CallListLeadStatuses"
_RESULT = dex.Attribute("copper-lead-statuses", LeadStatusesList)


def map_to_operation_input(request: ListLeadStatusesInput) -> ListLeadStatusesInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:copper group-label:"Copper"
# dex:explanation text:"Start the Copper request."
class _StartRequest(dex.Step[ListLeadStatusesInput]):
    def get_step_type(self) -> str:
        return "StartListLeadStatuses"

    def execute(self, context: dex.Context, input: ListLeadStatusesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListLeadStatuses"), input)


# dex:group group-id:copper group-label:"Copper"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[LeadStatusesList]]):
    def get_step_type(self) -> str:
        return "CompleteListLeadStatuses"

    def execute(self, context: dex.Context, input: QueryResult[LeadStatusesList]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class LeadStatusDiscoveryFlow(dex.Flow[ListLeadStatusesInput]):
    """Run one listLeadStatuses read through the static copper connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("lead status discovery requires the static copper connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_lead_statuses_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListLeadStatusesInput,
            annotations={
                "groupId": "copper",
                "groupLabel": "Copper",
                "explanation": "List lead statuses visible to the connected Copper user.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "CopperLeadStatusDiscovery"

    def get_steps(self) -> dex.StepList[ListLeadStatusesInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
