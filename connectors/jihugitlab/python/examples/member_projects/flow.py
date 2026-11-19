# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable JiHuGitLab project-page Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jihugitlab import (
    Connection,
    ListMemberProjectsInput,
    ListMemberProjectsOutput,
    new_list_member_projects_step,
)

CONNECTION_NAME = "jihugitlab"
_RESULT = dex.Attribute("jihugitlab-project-page", ListMemberProjectsOutput)


def map_to_operation_input(request: ListMemberProjectsInput) -> ListMemberProjectsInput:
    """Forward the requested project page."""
    return request


# dex:group group-id:jihugitlab group-label:"JiHuGitLab"
# dex:explanation text:"Start one JiHuGitLab project-page query."
class _StartMemberProjects(dex.Step[ListMemberProjectsInput]):
    def execute(self, context: dex.Context, input: ListMemberProjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallJiHuGitLab"), input)


# dex:group group-id:jihugitlab group-label:"JiHuGitLab"
# dex:explanation text:"Persist project identifiers and pagination before completion."
class _CompleteMemberProjects(dex.Step[QueryResult[ListMemberProjectsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListMemberProjectsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class MemberProjectsFlow(dex.Flow[ListMemberProjectsInput]):
    """List one member-project page through the static JiHuGitLab connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("MemberProjectsFlow requires the static JiHuGitLab connection")
        self._start = _StartMemberProjects()
        self._complete = _CompleteMemberProjects()
        self._call = new_list_member_projects_step(
            step_type="CallJiHuGitLab",
            step_input_type=ListMemberProjectsInput,
            annotations={
                "groupId": "jihugitlab",
                "groupLabel": "JiHuGitLab",
                "explanation": "Read project IDs, names, and paths for member projects.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable project-page Flow identity."""
        return "JiHuGitLabMemberProjects"

    def get_steps(self) -> dex.StepList[ListMemberProjectsInput]:
        """Register the project-page Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist project IDs, names, and pagination only."""
        return dex.PersistenceSchema.of(_RESULT)
