# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable GitLab lookup Flow using the getProject Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.gitlab import Connection, GetProjectInput, Project, new_get_project_step

FLOW_TYPE = "GitLabLookup"
CONNECTION_NAME = "gitlab"
_OPERATION_STEP_TYPE = "GetProjectGitLab"
_REQUEST = dex.Attribute("gitlab-lookup-request", GetProjectInput)
_OUTPUT = dex.Attribute("gitlab-lookup-output", Project)


def map_to_operation_input(request: GetProjectInput) -> GetProjectInput:
    """Read the requested project."""
    return request


# dex:group group-id:gitlab group-label:"GitLab"
# dex:explanation text:"Start the GitLab lookup with its typed request."
class _StartLookup(dex.Step[GetProjectInput]):
    def get_step_type(self) -> str:
        return "StartGitLabLookup"

    def execute(self, context: dex.Context, input: GetProjectInput) -> dex.StepDecision:
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:gitlab group-label:"GitLab"
# dex:explanation text:"Complete with the GitLab result."
class _CompleteLookup(dex.Step[QueryResult[Project]]):
    def get_step_type(self) -> str:
        return "CompleteGitLabLookup"

    def execute(self, context: dex.Context, input: QueryResult[Project]) -> dex.StepDecision:
        _OUTPUT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GitLabLookupFlow(dex.Flow[GetProjectInput]):
    """Read one GitLab project through the static gitlab connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("GitLab lookup requires the static gitlab connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._list = new_get_project_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=GetProjectInput,
            annotations={
                "groupId": "gitlab",
                "groupLabel": "GitLab",
                "explanation": "Read one GitLab project by numeric ID or namespace path.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetProjectInput]:
        """Register the start, provider list, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and project."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTPUT)
