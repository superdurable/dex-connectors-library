# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable issue review Flow using the Sentry listIssues Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.sentry import (
    Connection,
    IssuePage,
    ListIssuesInput,
    new_list_issues_step,
)

FLOW_TYPE = "SentryIssueReview"
CONNECTION_NAME = "sentry"
_LIST_STEP_TYPE = "ListIssues"
_OUTCOME = dex.Attribute("sentry-issue-review-outcome", IssuePage)


def map_to_list_issues_input(request: ListIssuesInput) -> ListIssuesInput:
    """List with the requested organization and query unchanged."""
    return request


# dex:group group-id:issue-review group-label:"Issue review"
# dex:explanation text:"Check the requested page window before calling Sentry."
class _PrepareRequest(dex.Step[ListIssuesInput]):
    def get_step_type(self) -> str:
        return "PrepareRequest"

    def execute(self, context: dex.Context, input: ListIssuesInput) -> dex.StepDecision:
        if not input.organization.strip():
            return dex.force_fail("organization slug is required")
        return dex.go_to(dex.StepRef(_LIST_STEP_TYPE), input)


# dex:group group-id:issue-review group-label:"Issue review"
# dex:explanation text:"Record the issue page and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[IssuePage]]):
    def get_step_type(self) -> str:
        return "CompleteRequest"

    def execute(self, context: dex.Context, input: QueryResult[IssuePage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class IssueReviewFlow(dex.Flow[ListIssuesInput]):
    """List one page of issues through the static sentry connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("issue review requires the static sentry connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRequest()
        self._list = new_list_issues_step(
            step_type=_LIST_STEP_TYPE,
            step_input_type=ListIssuesInput,
            annotations={
                "groupId": "sentry",
                "groupLabel": "Sentry",
                "explanation": "List the organization's issues from Sentry.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_issues_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListIssuesInput]:
        """Register request check, provider list, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the issue page."""
        return dex.PersistenceSchema.of(_OUTCOME)
