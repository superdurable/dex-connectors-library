# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListProjectsInput, ListTimeEntriesInput, Project, ProjectPage, TimeEntry, TimeEntryPage, TimeEntryProject
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_PROJECTS_DEFINITION, LIST_TIME_ENTRIES_DEFINITION, PROVIDER_REJECTED

def list_projects_listed(value: ProjectPage, failure: Failure | None = None) -> Case[ProjectPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_projects_listed, '__module__', "dex_connectors.harvest.harvestmock")

def list_projects_provider_rejected(value: ProjectPage, failure: Failure | None = None) -> Case[ProjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_projects_provider_rejected, '__module__', "dex_connectors.harvest.harvestmock")

def list_projects_invalid_response(value: ProjectPage, failure: Failure | None = None) -> Case[ProjectPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_projects_invalid_response, '__module__', "dex_connectors.harvest.harvestmock")

def list_projects_defect(value: ProjectPage, failure: Failure | None = None) -> Case[ProjectPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_projects_defect, '__module__', "dex_connectors.harvest.harvestmock")

def list_projects_retry(failure: Failure, after_seconds: float = 0) -> Case[ProjectPage]:
    return Case.retry(failure, after_seconds)

setattr(list_projects_retry, '__module__', "dex_connectors.harvest.harvestmock")

def list_projects_default_answer() -> Case[ProjectPage]:
    return Case.branch_result(LISTED, ProjectPage(projects=(Project(id=3001, name='Meridian Website', is_active=True),)))

setattr(list_projects_default_answer, '__module__', "dex_connectors.harvest.harvestmock")

def list_projects_invalid_key() -> Case[ProjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, ProjectPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listProjects", "Harvest rejected the token."))

setattr(list_projects_invalid_key, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_listed(value: TimeEntryPage, failure: Failure | None = None) -> Case[TimeEntryPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_time_entries_listed, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_provider_rejected(value: TimeEntryPage, failure: Failure | None = None) -> Case[TimeEntryPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_time_entries_provider_rejected, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_invalid_response(value: TimeEntryPage, failure: Failure | None = None) -> Case[TimeEntryPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_time_entries_invalid_response, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_defect(value: TimeEntryPage, failure: Failure | None = None) -> Case[TimeEntryPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_time_entries_defect, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_retry(failure: Failure, after_seconds: float = 0) -> Case[TimeEntryPage]:
    return Case.retry(failure, after_seconds)

setattr(list_time_entries_retry, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_default_answer() -> Case[TimeEntryPage]:
    return Case.branch_result(LISTED, TimeEntryPage(time_entries=(TimeEntry(id=4001, project=TimeEntryProject(id=3001), hours=2.5, spent_date='2026-10-28'),)))

setattr(list_time_entries_default_answer, '__module__', "dex_connectors.harvest.harvestmock")

def list_time_entries_invalid_key() -> Case[TimeEntryPage]:
    return Case.branch_result(PROVIDER_REJECTED, TimeEntryPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listTimeEntries", "Harvest rejected the token."))

setattr(list_time_entries_invalid_key, '__module__', "dex_connectors.harvest.harvestmock")


class Mock:

    def __init__(self, connection_name: str = "harvest") -> None:
        self.list_projects_script: Query[ListProjectsInput, ProjectPage] = Query(LIST_PROJECTS_DEFINITION)
        self.list_projects_script.default(list_projects_default_answer())
        self.list_time_entries_script: Query[ListTimeEntriesInput, TimeEntryPage] = Query(LIST_TIME_ENTRIES_DEFINITION)
        self.list_time_entries_script.default(list_time_entries_default_answer())
        self._connection = new_connection_with_operations(
            list_projects=self.list_projects_script,
            list_time_entries=self.list_time_entries_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.harvest.harvestmock")

__all__ = [
    "Mock",
    "list_projects_listed",
    "list_projects_provider_rejected",
    "list_projects_invalid_response",
    "list_projects_defect",
    "list_projects_retry",
    "list_projects_default_answer",
    "list_projects_invalid_key",
    "list_time_entries_listed",
    "list_time_entries_provider_rejected",
    "list_time_entries_invalid_response",
    "list_time_entries_defect",
    "list_time_entries_retry",
    "list_time_entries_default_answer",
    "list_time_entries_invalid_key",
]
