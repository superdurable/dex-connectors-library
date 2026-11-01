# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable incident review Flow using the PagerDuty listIncidents Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pagerduty import (
    Connection,
    IncidentPage,
    ListIncidentsInput,
    new_list_incidents_step,
)

FLOW_TYPE = "PagerDutyIncidentReview"
CONNECTION_NAME = "pagerduty"
_LIST_STEP_TYPE = "ListIncidents"
_OUTCOME = dex.Attribute("pagerduty-incident-review-outcome", IncidentPage)


def map_to_list_incidents_input(request: ListIncidentsInput) -> ListIncidentsInput:
    """Search with the requested page window unchanged."""
    return request


# dex:group group-id:incident-review group-label:"Incident review"
# dex:explanation text:"Check the requested page window before calling PagerDuty."
class _PrepareRequest(dex.Step[ListIncidentsInput]):
    def get_step_type(self) -> str:
        return "PrepareRequest"

    def execute(self, context: dex.Context, input: ListIncidentsInput) -> dex.StepDecision:
        if input.limit < 0 or input.offset < 0:
            return dex.force_fail("limit and offset must not be negative")
        return dex.go_to(dex.StepRef(_LIST_STEP_TYPE), input)


# dex:group group-id:incident-review group-label:"Incident review"
# dex:explanation text:"Record the incident page and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[IncidentPage]]):
    def get_step_type(self) -> str:
        return "CompleteRequest"

    def execute(self, context: dex.Context, input: QueryResult[IncidentPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class IncidentReviewFlow(dex.Flow[ListIncidentsInput]):
    """List one page of incidents through the static pagerduty connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("incident review requires the static pagerduty connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRequest()
        self._list = new_list_incidents_step(
            step_type=_LIST_STEP_TYPE,
            step_input_type=ListIncidentsInput,
            annotations={
                "groupId": "pagerduty",
                "groupLabel": "PagerDuty",
                "explanation": "List recent incidents from PagerDuty.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_incidents_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListIncidentsInput]:
        """Register request check, provider list, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the incident page."""
        return dex.PersistenceSchema.of(_OUTCOME)
