# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AppMetadata, ListAppsPageInput, ListAppsPageOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_APPS_PAGE_DEFINITION, PROVIDER_REJECTED

def list_apps_page_listed(value: ListAppsPageOutput, failure: Failure | None = None) -> Case[ListAppsPageOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_apps_page_listed, '__module__', "dex_connectors.fanruan.fanruanmock")

def list_apps_page_provider_rejected(value: ListAppsPageOutput, failure: Failure | None = None) -> Case[ListAppsPageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_apps_page_provider_rejected, '__module__', "dex_connectors.fanruan.fanruanmock")

def list_apps_page_invalid_response(value: ListAppsPageOutput, failure: Failure | None = None) -> Case[ListAppsPageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_apps_page_invalid_response, '__module__', "dex_connectors.fanruan.fanruanmock")

def list_apps_page_defect(value: ListAppsPageOutput, failure: Failure | None = None) -> Case[ListAppsPageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_apps_page_defect, '__module__', "dex_connectors.fanruan.fanruanmock")

def list_apps_page_retry(failure: Failure, after_seconds: float = 0) -> Case[ListAppsPageOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_apps_page_retry, '__module__', "dex_connectors.fanruan.fanruanmock")

def list_apps_page_one_app() -> Case[ListAppsPageOutput]:
    return Case.branch_result(LISTED, ListAppsPageOutput(apps=(AppMetadata(id='5d5a5bbb850a6d0604ab119', name='示例应用'),), next_skip=0))

setattr(list_apps_page_one_app, '__module__', "dex_connectors.fanruan.fanruanmock")


class Mock:

    def __init__(self, connection_name: str = "fanruan") -> None:
        self.list_apps_page_script: Query[ListAppsPageInput, ListAppsPageOutput] = Query(LIST_APPS_PAGE_DEFINITION)
        self.list_apps_page_script.default(list_apps_page_one_app())
        self._connection = new_connection_with_operations(
            list_apps_page=self.list_apps_page_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.fanruan.fanruanmock")

__all__ = [
    "Mock",
    "list_apps_page_listed",
    "list_apps_page_provider_rejected",
    "list_apps_page_invalid_response",
    "list_apps_page_defect",
    "list_apps_page_retry",
    "list_apps_page_one_app",
]
