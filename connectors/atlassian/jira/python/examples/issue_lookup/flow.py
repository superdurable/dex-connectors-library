# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Jira issue through a Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.atlassian_jira import Connection, GetIssueInput, Issue, new_get_issue_step

CONNECTION_NAME = "jira-issue-lookup"
FLOW_TYPE = "JiraIssueLookupPython"
ISSUE = dex.Attribute("jira-issue-lookup-result", Issue)


# dex:group group-id:jira-lookup group-label:"Jira issue lookup"
# dex:explanation text:"Persist the issue read through the Jira connection and complete the Flow."
class CompleteIssueLookup(dex.Step[QueryResult[Issue]]):
    """Complete the Flow with the provider-safe issue model."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteIssueLookup"

    def execute(self, context: dex.Context, input: QueryResult[Issue]) -> dex.StepDecision:
        """Persist the issue and expose it as the Flow output."""
        ISSUE.set(context, input.value)
        return dex.graceful_complete(input.value)


class IssueLookupFlow(dex.Flow[GetIssueInput]):
    """Read one issue through the static Jira connection."""

    def __init__(self, connection: Connection) -> None:
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Jira issue lookup requires its static connection name")
        self.complete = CompleteIssueLookup()
        # dex:group group-id:jira-lookup group-label:"Jira issue lookup"
        # dex:explanation text:"Read one Jira issue by key or numeric ID."
        self.get_issue = new_get_issue_step(
            step_type="GetJiraIssue",
            step_input_type=GetIssueInput,
            annotations={
                "groupId": "jira-lookup",
                "groupLabel": "Jira issue lookup",
                "explanation": "Read one Jira issue by key or numeric ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "JiraIssueLookupPython"

    def get_steps(self) -> dex.StepList[GetIssueInput]:
        """Register the provider Query and completion Step."""
        return dex.StepList.start_step(self.get_issue).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the issue Attribute consumed by Dex."""
        return dex.PersistenceSchema.of(ISSUE)
