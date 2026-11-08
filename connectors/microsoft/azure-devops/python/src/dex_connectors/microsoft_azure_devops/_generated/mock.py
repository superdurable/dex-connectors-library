# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetWorkItemInput, ListProjectsInput, ProjectMetadata, Projects, WorkItemMetadata
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_WORK_ITEM_DEFINITION, INVALID_RESPONSE, LISTED, LIST_PROJECTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_projects_listed(value: Projects, failure: Failure | None = None) -> Case[Projects]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_projects_listed, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def list_projects_provider_rejected(value: Projects, failure: Failure | None = None) -> Case[Projects]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_projects_provider_rejected, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def list_projects_invalid_response(value: Projects, failure: Failure | None = None) -> Case[Projects]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_projects_invalid_response, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def list_projects_defect(value: Projects, failure: Failure | None = None) -> Case[Projects]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_projects_defect, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def list_projects_retry(failure: Failure, after_seconds: float = 0) -> Case[Projects]:
    return Case.retry(failure, after_seconds)

setattr(list_projects_retry, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def list_projects_listed_default() -> Case[Projects]:
    return Case.branch_result(LISTED, Projects(projects=(ProjectMetadata(id='eb6e4656-77fc-42a1-9181-4c6d8e9da5d1', name='Example', state='wellFormed'),), top=20, skip=0))

setattr(list_projects_listed_default, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_found(value: WorkItemMetadata, failure: Failure | None = None) -> Case[WorkItemMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_work_item_found, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_not_found(value: WorkItemMetadata, failure: Failure | None = None) -> Case[WorkItemMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_work_item_not_found, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_provider_rejected(value: WorkItemMetadata, failure: Failure | None = None) -> Case[WorkItemMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_work_item_provider_rejected, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_invalid_response(value: WorkItemMetadata, failure: Failure | None = None) -> Case[WorkItemMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_work_item_invalid_response, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_defect(value: WorkItemMetadata, failure: Failure | None = None) -> Case[WorkItemMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_work_item_defect, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_retry(failure: Failure, after_seconds: float = 0) -> Case[WorkItemMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_work_item_retry, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

def get_work_item_found_default() -> Case[WorkItemMetadata]:
    return Case.branch_result(FOUND, WorkItemMetadata(id=144, title='Example task', state='Active', work_item_type='Task'))

setattr(get_work_item_found_default, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")


class Mock:

    def __init__(self, connection_name: str = "azure-devops") -> None:
        self.list_projects_script: Query[ListProjectsInput, Projects] = Query(LIST_PROJECTS_DEFINITION)
        self.list_projects_script.default(list_projects_listed_default())
        self.get_work_item_script: Query[GetWorkItemInput, WorkItemMetadata] = Query(GET_WORK_ITEM_DEFINITION)
        self.get_work_item_script.default(get_work_item_found_default())
        self._connection = new_connection_with_operations(
            list_projects=self.list_projects_script,
            get_work_item=self.get_work_item_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_azure_devops.azuredevopsmock")

__all__ = [
    "Mock",
    "list_projects_listed",
    "list_projects_provider_rejected",
    "list_projects_invalid_response",
    "list_projects_defect",
    "list_projects_retry",
    "list_projects_listed_default",
    "get_work_item_found",
    "get_work_item_not_found",
    "get_work_item_provider_rejected",
    "get_work_item_invalid_response",
    "get_work_item_defect",
    "get_work_item_retry",
    "get_work_item_found_default",
]
