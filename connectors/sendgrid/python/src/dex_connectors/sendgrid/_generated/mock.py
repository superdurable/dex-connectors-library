# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetStatsInput, ListTemplatesInput, StatsDay, StatsOutput, Template, TemplatesOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_STATS_DEFINITION, INVALID_RESPONSE, LISTED, LIST_TEMPLATES_DEFINITION, PROVIDER_REJECTED

def get_stats_found(value: StatsOutput, failure: Failure | None = None) -> Case[StatsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_stats_found, '__module__', "dex_connectors.sendgrid.sendgridmock")

def get_stats_provider_rejected(value: StatsOutput, failure: Failure | None = None) -> Case[StatsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_stats_provider_rejected, '__module__', "dex_connectors.sendgrid.sendgridmock")

def get_stats_invalid_response(value: StatsOutput, failure: Failure | None = None) -> Case[StatsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_stats_invalid_response, '__module__', "dex_connectors.sendgrid.sendgridmock")

def get_stats_defect(value: StatsOutput, failure: Failure | None = None) -> Case[StatsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_stats_defect, '__module__', "dex_connectors.sendgrid.sendgridmock")

def get_stats_retry(failure: Failure, after_seconds: float = 0) -> Case[StatsOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_stats_retry, '__module__', "dex_connectors.sendgrid.sendgridmock")

def get_stats_found_default() -> Case[StatsOutput]:
    return Case.branch_result(FOUND, StatsOutput(days=(StatsDay(date='2026-10-27', requests=8, delivered=7),)))

setattr(get_stats_found_default, '__module__', "dex_connectors.sendgrid.sendgridmock")

def list_templates_listed(value: TemplatesOutput, failure: Failure | None = None) -> Case[TemplatesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_templates_listed, '__module__', "dex_connectors.sendgrid.sendgridmock")

def list_templates_provider_rejected(value: TemplatesOutput, failure: Failure | None = None) -> Case[TemplatesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_templates_provider_rejected, '__module__', "dex_connectors.sendgrid.sendgridmock")

def list_templates_invalid_response(value: TemplatesOutput, failure: Failure | None = None) -> Case[TemplatesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_templates_invalid_response, '__module__', "dex_connectors.sendgrid.sendgridmock")

def list_templates_defect(value: TemplatesOutput, failure: Failure | None = None) -> Case[TemplatesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_templates_defect, '__module__', "dex_connectors.sendgrid.sendgridmock")

def list_templates_retry(failure: Failure, after_seconds: float = 0) -> Case[TemplatesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_templates_retry, '__module__', "dex_connectors.sendgrid.sendgridmock")

def list_templates_listed_default() -> Case[TemplatesOutput]:
    return Case.branch_result(LISTED, TemplatesOutput(templates=(Template(id='d-meridian', name='Meridian receipt', generation='dynamic'),), count=1))

setattr(list_templates_listed_default, '__module__', "dex_connectors.sendgrid.sendgridmock")


class Mock:

    def __init__(self, connection_name: str = "sendgrid") -> None:
        self.get_stats_script: Query[GetStatsInput, StatsOutput] = Query(GET_STATS_DEFINITION)
        self.get_stats_script.default(get_stats_found_default())
        self.list_templates_script: Query[ListTemplatesInput, TemplatesOutput] = Query(LIST_TEMPLATES_DEFINITION)
        self.list_templates_script.default(list_templates_listed_default())
        self._connection = new_connection_with_operations(
            get_stats=self.get_stats_script,
            list_templates=self.list_templates_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.sendgrid.sendgridmock")

__all__ = [
    "Mock",
    "get_stats_found",
    "get_stats_provider_rejected",
    "get_stats_invalid_response",
    "get_stats_defect",
    "get_stats_retry",
    "get_stats_found_default",
    "list_templates_listed",
    "list_templates_provider_rejected",
    "list_templates_invalid_response",
    "list_templates_defect",
    "list_templates_retry",
    "list_templates_listed_default",
]
