# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed PagerDuty scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_INCIDENT_DEFINITION,
    LIST_INCIDENTS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetIncidentInput, Incident, IncidentPage, ListIncidentsInput

_DEFAULT_INCIDENT = Incident("P123456", "API latency", "triggered")


def list_incidents_listed(value: IncidentPage, failure: Failure | None = None) -> Case[IncidentPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_incidents_provider_rejected(
    value: IncidentPage, failure: Failure | None = None
) -> Case[IncidentPage]:
    """Script a definite provider rejection of the list."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def list_incidents_default() -> Case[IncidentPage]:
    """Return the manifest's listIncidentsDefault answer."""
    return list_incidents_listed(IncidentPage((_DEFAULT_INCIDENT,), 25, 0, False))


def get_incident_found(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_incident_not_found(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    """Script a missing incident."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_incident_default() -> Case[Incident]:
    """Return the manifest's getIncidentDefault answer."""
    return get_incident_found(_DEFAULT_INCIDENT)


class Mock:
    """Script PagerDuty operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "pagerduty") -> None:
        self.list_incidents_script: Query[ListIncidentsInput, IncidentPage] = Query(LIST_INCIDENTS_DEFINITION)
        self.list_incidents_script.default(list_incidents_default())
        self.get_incident_script: Query[GetIncidentInput, Incident] = Query(GET_INCIDENT_DEFINITION)
        self.get_incident_script.default(get_incident_default())
        self._connection = new_connection_with_operations(
            self.list_incidents_script, self.get_incident_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_incidents_listed",
    "list_incidents_provider_rejected",
    "list_incidents_default",
    "get_incident_found",
    "get_incident_not_found",
    "get_incident_default",
]
