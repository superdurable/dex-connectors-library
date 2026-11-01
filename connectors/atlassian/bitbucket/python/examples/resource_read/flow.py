# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Bitbucket resource read Flow using the listRepositories Query."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.atlassian_bitbucket import (
    Connection,
    ListRepositoriesInput,
    RepositoryPage,
    new_list_repositories_step,
)

FLOW_TYPE = "BitbucketResourceRead"
CONNECTION_NAME = "bitbucket"
_READ_STEP_TYPE = "ReadBitbucketResource"


@dataclass(frozen=True, slots=True)
class ResourceReadRequest:
    """Bitbucket workspace whose repositories are read."""

    workspace: str = ""


@dataclass(frozen=True, slots=True)
class ResourceReadOutcome:
    """The repository page Bitbucket returned."""

    value: RepositoryPage = field(default_factory=RepositoryPage)


_OUTCOME = dex.Attribute("bitbucket-resource-read-outcome", ResourceReadOutcome)


def map_to_operation_input(request: ResourceReadRequest) -> ListRepositoriesInput:
    """Read the requested workspace's repositories."""
    return ListRepositoriesInput(request.workspace)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Validate the request before reading the provider."
class _PrepareRequest(dex.Step[ResourceReadRequest]):
    def get_step_type(self) -> str:
        return "PrepareResourceRead"

    def execute(self, context: dex.Context, input: ResourceReadRequest) -> dex.StepDecision:
        if not input.workspace:
            return dex.force_fail("Workspace is required")
        return dex.go_to(dex.StepRef(_READ_STEP_TYPE), input)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Record the repository page and complete the Flow."
class _CompleteRead(dex.Step[QueryResult[RepositoryPage]]):
    def get_step_type(self) -> str:
        return "CompleteResourceRead"

    def execute(self, context: dex.Context, input: QueryResult[RepositoryPage]) -> dex.StepDecision:
        outcome = ResourceReadOutcome(input.value)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class ResourceReadFlow(dex.Flow[ResourceReadRequest]):
    """Read a Bitbucket workspace's repositories through the static bitbucket connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("resource read requires the static bitbucket connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRead()
        self._read = new_list_repositories_step(
            step_type=_READ_STEP_TYPE,
            step_input_type=ResourceReadRequest,
            annotations={
                "groupId": "bitbucket",
                "groupLabel": "Bitbucket",
                "explanation": "Read the requested Bitbucket resource.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ResourceReadRequest]:
        """Register request preparation, the Bitbucket read, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the repository page outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
