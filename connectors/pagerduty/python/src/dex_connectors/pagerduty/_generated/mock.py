# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetIncidentInput, Incident, IncidentPage, ListIncidentsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_INCIDENT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_INCIDENTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_incidents_listed(value: IncidentPage, failure: Failure | None = None) -> Case[IncidentPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_incidents_listed, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def list_incidents_provider_rejected(value: IncidentPage, failure: Failure | None = None) -> Case[IncidentPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_incidents_provider_rejected, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def list_incidents_invalid_response(value: IncidentPage, failure: Failure | None = None) -> Case[IncidentPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_incidents_invalid_response, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def list_incidents_defect(value: IncidentPage, failure: Failure | None = None) -> Case[IncidentPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_incidents_defect, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def list_incidents_retry(failure: Failure, after_seconds: float = 0) -> Case[IncidentPage]:
    return Case.retry(failure, after_seconds)

setattr(list_incidents_retry, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def list_incidents_default() -> Case[IncidentPage]:
    return Case.branch_result(LISTED, IncidentPage(incidents=(Incident(id='P123456', title='API latency', status='triggered'),), limit=25, offset=0, more=False))

setattr(list_incidents_default, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_found(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_incident_found, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_not_found(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_incident_not_found, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_provider_rejected(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_incident_provider_rejected, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_invalid_response(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_incident_invalid_response, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_defect(value: Incident, failure: Failure | None = None) -> Case[Incident]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_incident_defect, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_retry(failure: Failure, after_seconds: float = 0) -> Case[Incident]:
    return Case.retry(failure, after_seconds)

setattr(get_incident_retry, '__module__', "dex_connectors.pagerduty.pagerdutymock")

def get_incident_default() -> Case[Incident]:
    return Case.branch_result(FOUND, Incident(id='P123456', title='API latency', status='triggered'))

setattr(get_incident_default, '__module__', "dex_connectors.pagerduty.pagerdutymock")


class Mock:

    def __init__(self, connection_name: str = "pagerduty") -> None:
        self.list_incidents_script: Query[ListIncidentsInput, IncidentPage] = Query(LIST_INCIDENTS_DEFINITION)
        self.list_incidents_script.default(list_incidents_default())
        self.get_incident_script: Query[GetIncidentInput, Incident] = Query(GET_INCIDENT_DEFINITION)
        self.get_incident_script.default(get_incident_default())
        self._connection = new_connection_with_operations(
            list_incidents=self.list_incidents_script,
            get_incident=self.get_incident_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pagerduty.pagerdutymock")

__all__ = [
    "Mock",
    "list_incidents_listed",
    "list_incidents_provider_rejected",
    "list_incidents_invalid_response",
    "list_incidents_defect",
    "list_incidents_retry",
    "list_incidents_default",
    "get_incident_found",
    "get_incident_not_found",
    "get_incident_provider_rejected",
    "get_incident_invalid_response",
    "get_incident_defect",
    "get_incident_retry",
    "get_incident_default",
]
