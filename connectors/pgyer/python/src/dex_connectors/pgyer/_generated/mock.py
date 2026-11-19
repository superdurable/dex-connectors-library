# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListUploadedAppsInput, UploadedApp, UploadedAppPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_UPLOADED_APPS_DEFINITION, PROVIDER_REJECTED

def list_uploaded_apps_listed(value: UploadedAppPage, failure: Failure | None = None) -> Case[UploadedAppPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_uploaded_apps_listed, '__module__', "dex_connectors.pgyer.pgyermock")

def list_uploaded_apps_provider_rejected(value: UploadedAppPage, failure: Failure | None = None) -> Case[UploadedAppPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_uploaded_apps_provider_rejected, '__module__', "dex_connectors.pgyer.pgyermock")

def list_uploaded_apps_invalid_response(value: UploadedAppPage, failure: Failure | None = None) -> Case[UploadedAppPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_uploaded_apps_invalid_response, '__module__', "dex_connectors.pgyer.pgyermock")

def list_uploaded_apps_defect(value: UploadedAppPage, failure: Failure | None = None) -> Case[UploadedAppPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_uploaded_apps_defect, '__module__', "dex_connectors.pgyer.pgyermock")

def list_uploaded_apps_retry(failure: Failure, after_seconds: float = 0) -> Case[UploadedAppPage]:
    return Case.retry(failure, after_seconds)

setattr(list_uploaded_apps_retry, '__module__', "dex_connectors.pgyer.pgyermock")

def list_uploaded_apps_listed_app() -> Case[UploadedAppPage]:
    return Case.branch_result(LISTED, UploadedAppPage(apps=(UploadedApp(app_key='x1y2z3', build_key='a1b2c3d4e5f6', name='Example App', build_type=2, version='1.1', created_at='2025-01-01 10:00:00'),)))

setattr(list_uploaded_apps_listed_app, '__module__', "dex_connectors.pgyer.pgyermock")


class Mock:

    def __init__(self, connection_name: str = "pgyer") -> None:
        self.list_uploaded_apps_script: Query[ListUploadedAppsInput, UploadedAppPage] = Query(LIST_UPLOADED_APPS_DEFINITION)
        self.list_uploaded_apps_script.default(list_uploaded_apps_listed_app())
        self._connection = new_connection_with_operations(
            list_uploaded_apps=self.list_uploaded_apps_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pgyer.pgyermock")

__all__ = [
    "Mock",
    "list_uploaded_apps_listed",
    "list_uploaded_apps_provider_rejected",
    "list_uploaded_apps_invalid_response",
    "list_uploaded_apps_defect",
    "list_uploaded_apps_retry",
    "list_uploaded_apps_listed_app",
]
