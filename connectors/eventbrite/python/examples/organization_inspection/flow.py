# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Eventbrite organization inspection Flow using the listOrganizations Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.eventbrite import (
    Connection,
    ListOrganizationsInput,
    OrganizationsPage,
    new_list_organizations_step,
)

FLOW_TYPE = "EventbriteOrganizationInspection"
CONNECTION_NAME = "eventbrite"
_CALL_STEP_TYPE = "CallEventbrite"
_RESULT = dex.Attribute("eventbrite-organizations-page", OrganizationsPage)


def map_to_operation_input(request: ListOrganizationsInput) -> ListOrganizationsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:eventbrite group-label:"Eventbrite"
# dex:explanation text:"Start the Eventbrite request."
class _StartRequest(dex.Step[ListOrganizationsInput]):
    def get_step_type(self) -> str:
        return "StartEventbriteRequest"

    def execute(self, context: dex.Context, input: ListOrganizationsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_CALL_STEP_TYPE), input)


# dex:group group-id:eventbrite group-label:"Eventbrite"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[OrganizationsPage]]):
    def get_step_type(self) -> str:
        return "CompleteEventbriteRequest"

    def execute(self, context: dex.Context, input: QueryResult[OrganizationsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class OrganizationInspectionFlow(dex.Flow[ListOrganizationsInput]):
    """Run one listOrganizations read through the static eventbrite connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the organization inspection requires the static eventbrite connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_organizations_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListOrganizationsInput,
            annotations={
                "groupId": "eventbrite",
                "groupLabel": "Eventbrite",
                "explanation": "List organizations accessible to the connected Eventbrite account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListOrganizationsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
