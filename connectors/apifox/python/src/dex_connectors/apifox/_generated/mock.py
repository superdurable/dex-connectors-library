# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetProjectAPISummaryInput, GetProjectAPISummaryOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PROJECT_API_SUMMARY_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_project_api_summary_found(value: GetProjectAPISummaryOutput, failure: Failure | None = None) -> Case[GetProjectAPISummaryOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_project_api_summary_found, '__module__', "dex_connectors.apifox.apifoxmock")

def get_project_api_summary_provider_rejected(value: GetProjectAPISummaryOutput, failure: Failure | None = None) -> Case[GetProjectAPISummaryOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_project_api_summary_provider_rejected, '__module__', "dex_connectors.apifox.apifoxmock")

def get_project_api_summary_invalid_response(value: GetProjectAPISummaryOutput, failure: Failure | None = None) -> Case[GetProjectAPISummaryOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_project_api_summary_invalid_response, '__module__', "dex_connectors.apifox.apifoxmock")

def get_project_api_summary_defect(value: GetProjectAPISummaryOutput, failure: Failure | None = None) -> Case[GetProjectAPISummaryOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_project_api_summary_defect, '__module__', "dex_connectors.apifox.apifoxmock")

def get_project_api_summary_retry(failure: Failure, after_seconds: float = 0) -> Case[GetProjectAPISummaryOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_project_api_summary_retry, '__module__', "dex_connectors.apifox.apifoxmock")

def get_project_api_summary_one_endpoint() -> Case[GetProjectAPISummaryOutput]:
    return Case.branch_result(FOUND, GetProjectAPISummaryOutput(title='Example API', version='1.0.0', endpoint_count=1))

setattr(get_project_api_summary_one_endpoint, '__module__', "dex_connectors.apifox.apifoxmock")

def get_project_api_summary_empty_project() -> Case[GetProjectAPISummaryOutput]:
    return Case.branch_result(FOUND, GetProjectAPISummaryOutput(title='Empty API', version='1.0.0', endpoint_count=0))

setattr(get_project_api_summary_empty_project, '__module__', "dex_connectors.apifox.apifoxmock")


class Mock:

    def __init__(self, connection_name: str = "apifox") -> None:
        self.get_project_api_summary_script: Query[GetProjectAPISummaryInput, GetProjectAPISummaryOutput] = Query(GET_PROJECT_API_SUMMARY_DEFINITION)
        self.get_project_api_summary_script.default(get_project_api_summary_one_endpoint())
        self._connection = new_connection_with_operations(
            get_project_api_summary=self.get_project_api_summary_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.apifox.apifoxmock")

__all__ = [
    "Mock",
    "get_project_api_summary_found",
    "get_project_api_summary_provider_rejected",
    "get_project_api_summary_invalid_response",
    "get_project_api_summary_defect",
    "get_project_api_summary_retry",
    "get_project_api_summary_one_endpoint",
    "get_project_api_summary_empty_project",
]
