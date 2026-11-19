# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccessibleProject, ListAccessibleProjectsInput, ListAccessibleProjectsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_ACCESSIBLE_PROJECTS_DEFINITION, PROVIDER_REJECTED

def list_accessible_projects_found(value: ListAccessibleProjectsOutput, failure: Failure | None = None) -> Case[ListAccessibleProjectsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_accessible_projects_found, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")

def list_accessible_projects_provider_rejected(value: ListAccessibleProjectsOutput, failure: Failure | None = None) -> Case[ListAccessibleProjectsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_accessible_projects_provider_rejected, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")

def list_accessible_projects_invalid_response(value: ListAccessibleProjectsOutput, failure: Failure | None = None) -> Case[ListAccessibleProjectsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_accessible_projects_invalid_response, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")

def list_accessible_projects_defect(value: ListAccessibleProjectsOutput, failure: Failure | None = None) -> Case[ListAccessibleProjectsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_accessible_projects_defect, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")

def list_accessible_projects_retry(failure: Failure, after_seconds: float = 0) -> Case[ListAccessibleProjectsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_accessible_projects_retry, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")

def list_accessible_projects_two_projects() -> Case[ListAccessibleProjectsOutput]:
    return Case.branch_result(FOUND, ListAccessibleProjectsOutput(projects=(AccessibleProject(id='06f1cbbaf280106b2f14c00313a9d065', name='af-south-1', enabled=True, domain_id='d78cbac186b744899480f25bd022f000'), AccessibleProject(id='065a7c66da0010992ff7c0031e5a5e7d', name='ap-southeast-1', enabled=True, domain_id='d78cbac186b744899480f25bd022f000'))))

setattr(list_accessible_projects_two_projects, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")


class Mock:

    def __init__(self, connection_name: str = "huaweicloud") -> None:
        self.list_accessible_projects_script: Query[ListAccessibleProjectsInput, ListAccessibleProjectsOutput] = Query(LIST_ACCESSIBLE_PROJECTS_DEFINITION)
        self.list_accessible_projects_script.default(list_accessible_projects_two_projects())
        self._connection = new_connection_with_operations(
            list_accessible_projects=self.list_accessible_projects_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.huaweicloud.huaweicloudmock")

__all__ = [
    "Mock",
    "list_accessible_projects_found",
    "list_accessible_projects_provider_rejected",
    "list_accessible_projects_invalid_response",
    "list_accessible_projects_defect",
    "list_accessible_projects_retry",
    "list_accessible_projects_two_projects",
]
