# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Audience, AudiencesPage, GetPropertyInput, ListAudiencesInput, Property, ReportPage, ReportRow, RunReportInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PROPERTY_DEFINITION, INVALID_RESPONSE, LISTED, LIST_AUDIENCES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, REPORTED, RUN_REPORT_DEFINITION

def run_report_reported(value: ReportPage, failure: Failure | None = None) -> Case[ReportPage]:
    return Case.branch_result(REPORTED, value, failure)

setattr(run_report_reported, '__module__', "dex_connectors.google_analytics.analytics4mock")

def run_report_provider_rejected(value: ReportPage, failure: Failure | None = None) -> Case[ReportPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(run_report_provider_rejected, '__module__', "dex_connectors.google_analytics.analytics4mock")

def run_report_invalid_response(value: ReportPage, failure: Failure | None = None) -> Case[ReportPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(run_report_invalid_response, '__module__', "dex_connectors.google_analytics.analytics4mock")

def run_report_defect(value: ReportPage, failure: Failure | None = None) -> Case[ReportPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(run_report_defect, '__module__', "dex_connectors.google_analytics.analytics4mock")

def run_report_retry(failure: Failure, after_seconds: float = 0) -> Case[ReportPage]:
    return Case.retry(failure, after_seconds)

setattr(run_report_retry, '__module__', "dex_connectors.google_analytics.analytics4mock")

def run_report_reported_default() -> Case[ReportPage]:
    return Case.branch_result(REPORTED, ReportPage(rows=(ReportRow(dimensions=('United States',), metrics=('4820',)),), row_count=1))

setattr(run_report_reported_default, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_found(value: Property, failure: Failure | None = None) -> Case[Property]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_property_found, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_not_found(value: Property, failure: Failure | None = None) -> Case[Property]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_property_not_found, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_provider_rejected(value: Property, failure: Failure | None = None) -> Case[Property]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_property_provider_rejected, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_invalid_response(value: Property, failure: Failure | None = None) -> Case[Property]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_property_invalid_response, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_defect(value: Property, failure: Failure | None = None) -> Case[Property]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_property_defect, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_retry(failure: Failure, after_seconds: float = 0) -> Case[Property]:
    return Case.retry(failure, after_seconds)

setattr(get_property_retry, '__module__', "dex_connectors.google_analytics.analytics4mock")

def get_property_found_default() -> Case[Property]:
    return Case.branch_result(FOUND, Property(name='properties/123456789', display_name='Example Analytics', time_zone='America/Los_Angeles', currency_code='USD'))

setattr(get_property_found_default, '__module__', "dex_connectors.google_analytics.analytics4mock")

def list_audiences_listed(value: AudiencesPage, failure: Failure | None = None) -> Case[AudiencesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_audiences_listed, '__module__', "dex_connectors.google_analytics.analytics4mock")

def list_audiences_provider_rejected(value: AudiencesPage, failure: Failure | None = None) -> Case[AudiencesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_audiences_provider_rejected, '__module__', "dex_connectors.google_analytics.analytics4mock")

def list_audiences_invalid_response(value: AudiencesPage, failure: Failure | None = None) -> Case[AudiencesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_audiences_invalid_response, '__module__', "dex_connectors.google_analytics.analytics4mock")

def list_audiences_defect(value: AudiencesPage, failure: Failure | None = None) -> Case[AudiencesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_audiences_defect, '__module__', "dex_connectors.google_analytics.analytics4mock")

def list_audiences_retry(failure: Failure, after_seconds: float = 0) -> Case[AudiencesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_audiences_retry, '__module__', "dex_connectors.google_analytics.analytics4mock")

def list_audiences_listed_default() -> Case[AudiencesPage]:
    return Case.branch_result(LISTED, AudiencesPage(audiences=(Audience(name='properties/123456789/audiences/42', display_name='Returning users', description='Recently active users'),), next_page_token=''))

setattr(list_audiences_listed_default, '__module__', "dex_connectors.google_analytics.analytics4mock")


class Mock:

    def __init__(self, connection_name: str = "google-analytics-4") -> None:
        self.run_report_script: Query[RunReportInput, ReportPage] = Query(RUN_REPORT_DEFINITION)
        self.run_report_script.default(run_report_reported_default())
        self.get_property_script: Query[GetPropertyInput, Property] = Query(GET_PROPERTY_DEFINITION)
        self.get_property_script.default(get_property_found_default())
        self.list_audiences_script: Query[ListAudiencesInput, AudiencesPage] = Query(LIST_AUDIENCES_DEFINITION)
        self.list_audiences_script.default(list_audiences_listed_default())
        self._connection = new_connection_with_operations(
            run_report=self.run_report_script,
            get_property=self.get_property_script,
            list_audiences=self.list_audiences_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_analytics.analytics4mock")

__all__ = [
    "Mock",
    "run_report_reported",
    "run_report_provider_rejected",
    "run_report_invalid_response",
    "run_report_defect",
    "run_report_retry",
    "run_report_reported_default",
    "get_property_found",
    "get_property_not_found",
    "get_property_provider_rejected",
    "get_property_invalid_response",
    "get_property_defect",
    "get_property_retry",
    "get_property_found_default",
    "list_audiences_listed",
    "list_audiences_provider_rejected",
    "list_audiences_invalid_response",
    "list_audiences_defect",
    "list_audiences_retry",
    "list_audiences_listed_default",
]
