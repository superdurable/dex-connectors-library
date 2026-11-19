# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ApplicationMetadata, ListApplicationsInput, ListApplicationsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_APPLICATIONS_DEFINITION, PROVIDER_REJECTED

def list_applications_listed(value: ListApplicationsOutput, failure: Failure | None = None) -> Case[ListApplicationsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_applications_listed, '__module__', "dex_connectors.qingflow.qingflowmock")

def list_applications_provider_rejected(value: ListApplicationsOutput, failure: Failure | None = None) -> Case[ListApplicationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_applications_provider_rejected, '__module__', "dex_connectors.qingflow.qingflowmock")

def list_applications_invalid_response(value: ListApplicationsOutput, failure: Failure | None = None) -> Case[ListApplicationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_applications_invalid_response, '__module__', "dex_connectors.qingflow.qingflowmock")

def list_applications_defect(value: ListApplicationsOutput, failure: Failure | None = None) -> Case[ListApplicationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_applications_defect, '__module__', "dex_connectors.qingflow.qingflowmock")

def list_applications_retry(failure: Failure, after_seconds: float = 0) -> Case[ListApplicationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_applications_retry, '__module__', "dex_connectors.qingflow.qingflowmock")

def list_applications_one_application() -> Case[ListApplicationsOutput]:
    return Case.branch_result(LISTED, ListApplicationsOutput(applications=(ApplicationMetadata(app_key='app_example', app_name='Operations'),)))

setattr(list_applications_one_application, '__module__', "dex_connectors.qingflow.qingflowmock")


class Mock:

    def __init__(self, connection_name: str = "qingflow") -> None:
        self.list_applications_script: Query[ListApplicationsInput, ListApplicationsOutput] = Query(LIST_APPLICATIONS_DEFINITION)
        self.list_applications_script.default(list_applications_one_application())
        self._connection = new_connection_with_operations(
            list_applications=self.list_applications_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.qingflow.qingflowmock")

__all__ = [
    "Mock",
    "list_applications_listed",
    "list_applications_provider_rejected",
    "list_applications_invalid_response",
    "list_applications_defect",
    "list_applications_retry",
    "list_applications_one_application",
]
