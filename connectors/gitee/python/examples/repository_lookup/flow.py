# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Gitee repository through a statically named connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.gitee import (
    Connection,
    GetRepositoryInput,
    GetRepositoryOutput,
    new_get_repository_step,
)

CONNECTION_NAME = "gitee"
repository_result = dex.Attribute("gitee-repository-python", GetRepositoryOutput)


# dex:group group-id:repository group-label:"Repository"
# dex:explanation text:"Persist repository metadata and complete the Flow."
class RecordRepository(dex.Step[QueryResult[GetRepositoryOutput]]):
    """Persist the public repository metadata after provider success."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "RecordGiteeRepositoryPython"

    def execute(self, context: dex.Context, input: QueryResult[GetRepositoryOutput]) -> dex.StepDecision:
        """Store only the repository metadata, never the access token."""
        repository_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class RepositoryLookupFlow(dex.Flow[GetRepositoryInput]):
    """Read a repository's public metadata using the named Gitee connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("repository Flow requires the static Gitee connection")
        self.record = RecordRepository()
        self.lookup = new_get_repository_step(
            step_type="GetGiteeRepositoryPython",
            step_input_type=GetRepositoryInput,
            annotations={
                "groupId": "repository",
                "groupLabel": "Repository",
                "explanation": "Read the selected Gitee repository metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the stable repository Flow identity."""
        return "GiteeRepositoryLookupPython"

    def get_steps(self) -> dex.StepList[GetRepositoryInput]:
        """Register the provider and completion Steps."""
        return dex.StepList.start_step(self.lookup).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable repository metadata."""
        return dex.PersistenceSchema.of(repository_result)
