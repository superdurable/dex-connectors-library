# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Harvest scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    LIST_PROJECTS_DEFINITION,
    LIST_TIME_ENTRIES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import (
    ListProjectsInput,
    ListTimeEntriesInput,
    Project,
    ProjectPage,
    TimeEntry,
    TimeEntryPage,
    TimeEntryProject,
)


def list_projects_listed(value: ProjectPage, failure: Failure | None = None) -> Case[ProjectPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_projects_default_answer() -> Case[ProjectPage]:
    """Return the manifest's listProjects defaultAnswer."""
    return list_projects_listed(ProjectPage((Project(3001, "Meridian Website", True),)))


def list_projects_invalid_key() -> Case[ProjectPage]:
    """Return the manifest's listProjects invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        ProjectPage(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listProjects", "Harvest rejected the token."),
    )


def list_time_entries_listed(value: TimeEntryPage, failure: Failure | None = None) -> Case[TimeEntryPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_time_entries_default_answer() -> Case[TimeEntryPage]:
    """Return the manifest's listTimeEntries defaultAnswer."""
    return list_time_entries_listed(
        TimeEntryPage((TimeEntry(4001, TimeEntryProject(3001), 2.5, "2026-10-28"),))
    )


def list_time_entries_invalid_key() -> Case[TimeEntryPage]:
    """Return the manifest's listTimeEntries invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        TimeEntryPage(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listTimeEntries", "Harvest rejected the token."),
    )


class Mock:
    """Script Harvest Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "harvest") -> None:
        self.list_projects_script: Query[ListProjectsInput, ProjectPage] = Query(LIST_PROJECTS_DEFINITION)
        self.list_projects_script.default(list_projects_default_answer())
        self.list_time_entries_script: Query[ListTimeEntriesInput, TimeEntryPage] = Query(
            LIST_TIME_ENTRIES_DEFINITION
        )
        self.list_time_entries_script.default(list_time_entries_default_answer())
        self._connection = new_connection_with_operations(
            self.list_projects_script,
            self.list_time_entries_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_projects_listed",
    "list_projects_default_answer",
    "list_projects_invalid_key",
    "list_time_entries_listed",
    "list_time_entries_default_answer",
    "list_time_entries_invalid_key",
]
