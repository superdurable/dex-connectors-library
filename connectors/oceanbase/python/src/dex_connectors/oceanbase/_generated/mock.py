# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import InstanceInfo, ListInstancesInput, ListInstancesOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_INSTANCES_DEFINITION, PROVIDER_REJECTED

def list_instances_listed(value: ListInstancesOutput, failure: Failure | None = None) -> Case[ListInstancesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_instances_listed, '__module__', "dex_connectors.oceanbase.oceanbasemock")

def list_instances_provider_rejected(value: ListInstancesOutput, failure: Failure | None = None) -> Case[ListInstancesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_instances_provider_rejected, '__module__', "dex_connectors.oceanbase.oceanbasemock")

def list_instances_invalid_response(value: ListInstancesOutput, failure: Failure | None = None) -> Case[ListInstancesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_instances_invalid_response, '__module__', "dex_connectors.oceanbase.oceanbasemock")

def list_instances_defect(value: ListInstancesOutput, failure: Failure | None = None) -> Case[ListInstancesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_instances_defect, '__module__', "dex_connectors.oceanbase.oceanbasemock")

def list_instances_retry(failure: Failure, after_seconds: float = 0) -> Case[ListInstancesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_instances_retry, '__module__', "dex_connectors.oceanbase.oceanbasemock")

def list_instances_one_instance() -> Case[ListInstancesOutput]:
    return Case.branch_result(LISTED, ListInstancesOutput(items=(InstanceInfo(id='obcluster-example', name='example', state='ONLINE', region='ap-guangzhou'),), total=1, page_number=1, page_size=10))

setattr(list_instances_one_instance, '__module__', "dex_connectors.oceanbase.oceanbasemock")


class Mock:

    def __init__(self, connection_name: str = "oceanbase") -> None:
        self.list_instances_script: Query[ListInstancesInput, ListInstancesOutput] = Query(LIST_INSTANCES_DEFINITION)
        self.list_instances_script.default(list_instances_one_instance())
        self._connection = new_connection_with_operations(
            list_instances=self.list_instances_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.oceanbase.oceanbasemock")

__all__ = [
    "Mock",
    "list_instances_listed",
    "list_instances_provider_rejected",
    "list_instances_invalid_response",
    "list_instances_defect",
    "list_instances_retry",
    "list_instances_one_instance",
]
