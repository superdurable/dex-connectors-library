# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListMemberProjectsInput, ListMemberProjectsOutput, MemberProject
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_MEMBER_PROJECTS_DEFINITION, PROVIDER_REJECTED

def list_member_projects_listed(value: ListMemberProjectsOutput, failure: Failure | None = None) -> Case[ListMemberProjectsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_member_projects_listed, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")

def list_member_projects_provider_rejected(value: ListMemberProjectsOutput, failure: Failure | None = None) -> Case[ListMemberProjectsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_member_projects_provider_rejected, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")

def list_member_projects_invalid_response(value: ListMemberProjectsOutput, failure: Failure | None = None) -> Case[ListMemberProjectsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_member_projects_invalid_response, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")

def list_member_projects_defect(value: ListMemberProjectsOutput, failure: Failure | None = None) -> Case[ListMemberProjectsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_member_projects_defect, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")

def list_member_projects_retry(failure: Failure, after_seconds: float = 0) -> Case[ListMemberProjectsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_member_projects_retry, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")

def list_member_projects_sample_member_projects() -> Case[ListMemberProjectsOutput]:
    return Case.branch_result(LISTED, ListMemberProjectsOutput(projects=(MemberProject(id=42, name='Example Project', path='example-group/example-project'),), next_page=0))

setattr(list_member_projects_sample_member_projects, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")


class Mock:

    def __init__(self, connection_name: str = "jihugitlab") -> None:
        self.list_member_projects_script: Query[ListMemberProjectsInput, ListMemberProjectsOutput] = Query(LIST_MEMBER_PROJECTS_DEFINITION)
        self.list_member_projects_script.default(list_member_projects_sample_member_projects())
        self._connection = new_connection_with_operations(
            list_member_projects=self.list_member_projects_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jihugitlab.jihugitlabmock")

__all__ = [
    "Mock",
    "list_member_projects_listed",
    "list_member_projects_provider_rejected",
    "list_member_projects_invalid_response",
    "list_member_projects_defect",
    "list_member_projects_retry",
    "list_member_projects_sample_member_projects",
]
