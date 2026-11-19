# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListProjectsInput, ListProjectsOutput, Project
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_PROJECTS_DEFINITION, PROVIDER_REJECTED

def list_projects_found(value: ListProjectsOutput, failure: Failure | None = None) -> Case[ListProjectsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_projects_found, '__module__', "dex_connectors.worktile.worktilemock")

def list_projects_provider_rejected(value: ListProjectsOutput, failure: Failure | None = None) -> Case[ListProjectsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_projects_provider_rejected, '__module__', "dex_connectors.worktile.worktilemock")

def list_projects_invalid_response(value: ListProjectsOutput, failure: Failure | None = None) -> Case[ListProjectsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_projects_invalid_response, '__module__', "dex_connectors.worktile.worktilemock")

def list_projects_defect(value: ListProjectsOutput, failure: Failure | None = None) -> Case[ListProjectsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_projects_defect, '__module__', "dex_connectors.worktile.worktilemock")

def list_projects_retry(failure: Failure, after_seconds: float = 0) -> Case[ListProjectsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_projects_retry, '__module__', "dex_connectors.worktile.worktilemock")

def list_projects_two_projects() -> Case[ListProjectsOutput]:
    return Case.branch_result(FOUND, ListProjectsOutput(page_index=0, page_size=10, has_more=False, projects=(Project(id='5b98e38cdf09ce57a2bf7151', name='Example Project', identifier='ABC', visibility=1), Project(id='5b98e38cdf09ce57a2bf7152', name='Private Project', identifier='XYZ', visibility=2))))

setattr(list_projects_two_projects, '__module__', "dex_connectors.worktile.worktilemock")


class Mock:

    def __init__(self, connection_name: str = "worktile") -> None:
        self.list_projects_script: Query[ListProjectsInput, ListProjectsOutput] = Query(LIST_PROJECTS_DEFINITION)
        self.list_projects_script.default(list_projects_two_projects())
        self._connection = new_connection_with_operations(
            list_projects=self.list_projects_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.worktile.worktilemock")

__all__ = [
    "Mock",
    "list_projects_found",
    "list_projects_provider_rejected",
    "list_projects_invalid_response",
    "list_projects_defect",
    "list_projects_retry",
    "list_projects_two_projects",
]
