# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable PostHog Flow listing one bounded page of saved insights."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.posthog import Connection, InsightsPage, ListInsightsInput, new_list_insights_step

CONNECTION_NAME = "posthog"
_LIST_STEP_TYPE = "ListPostHogInsights"
_OUTCOME = dex.Attribute("posthog-python-insight-list-outcome", InsightsPage)


def map_to_list_insights_input(request: ListInsightsInput) -> ListInsightsInput:
    """Forward the validated page request to the provider Query."""
    return request


# dex:group group-id:insight-list group-label:"Insight list"
# dex:explanation text:"Start the saved insight query for the selected PostHog project."
class _Start(dex.Step[ListInsightsInput]):
    def get_step_type(self) -> str:
        return "StartPostHogInsightList"

    def execute(self, context: dex.Context, input: ListInsightsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListPostHogInsights"), input)


# dex:group group-id:insight-list group-label:"Insight list"
# dex:explanation text:"Complete after PostHog returns the saved insight page."
class _Listed(dex.Step[QueryResult[InsightsPage]]):
    def get_step_type(self) -> str:
        return "PostHogInsightsListed"

    def execute(self, context: dex.Context, input: QueryResult[InsightsPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class InsightListFlow(dex.Flow[ListInsightsInput]):
    """List saved insight identities through the static posthog connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("insight listing requires the static posthog connection")
        self._start = _Start()
        self._listed = _Listed()
        self._list = new_list_insights_step(
            step_type=_LIST_STEP_TYPE,
            step_input_type=ListInsightsInput,
            annotations={
                "groupId": "insight-list",
                "groupLabel": "Insight list",
                "explanation": "List one bounded PostHog insight page without analysis results.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_insights_input,
            listed=self._listed,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PostHogPythonInsightList"

    def get_steps(self) -> dex.StepList[ListInsightsInput]:
        """Register the start, provider query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._list, self._listed)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the bounded insight page."""
        return dex.PersistenceSchema.of(_OUTCOME)
