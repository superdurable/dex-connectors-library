# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Guance dashboard-page Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.guance import (
    Connection,
    ListDashboardsPageInput,
    ListDashboardsPageOutput,
    new_list_dashboards_page_step,
)

CONNECTION_NAME = "guance"
_RESULT = dex.Attribute("guance-dashboard-page", ListDashboardsPageOutput)


def map_to_operation_input(request: ListDashboardsPageInput) -> ListDashboardsPageInput:
    """Forward the requested dashboard page."""
    return request


# dex:group group-id:guance group-label:"Guance"
# dex:explanation text:"Start one Guance dashboard-page query."
class _StartDashboardPage(dex.Step[ListDashboardsPageInput]):
    def execute(self, context: dex.Context, input: ListDashboardsPageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGuance"), input)


# dex:group group-id:guance group-label:"Guance"
# dex:explanation text:"Persist dashboard identifiers and pagination before completion."
class _CompleteDashboardPage(dex.Step[QueryResult[ListDashboardsPageOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListDashboardsPageOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DashboardPageFlow(dex.Flow[ListDashboardsPageInput]):
    """List one dashboard page through the static Guance connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DashboardPageFlow requires the static Guance connection")
        self._start = _StartDashboardPage()
        self._complete = _CompleteDashboardPage()
        self._call = new_list_dashboards_page_step(
            step_type="CallGuance",
            step_input_type=ListDashboardsPageInput,
            annotations={
                "groupId": "guance",
                "groupLabel": "Guance",
                "explanation": "Read dashboard IDs and names from the selected workspace.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable dashboard-page Flow identity."""
        return "GuanceDashboardPage"

    def get_steps(self) -> dex.StepList[ListDashboardsPageInput]:
        """Register the dashboard-page Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist dashboard IDs, names, and pagination only."""
        return dex.PersistenceSchema.of(_RESULT)
