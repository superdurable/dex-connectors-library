# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Deployment, ListDeploymentsInput, ListDeploymentsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DEPLOYMENTS_DEFINITION, PROVIDER_REJECTED

def list_deployments_listed(value: ListDeploymentsOutput, failure: Failure | None = None) -> Case[ListDeploymentsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_deployments_listed, '__module__', "dex_connectors.emq.emqmock")

def list_deployments_provider_rejected(value: ListDeploymentsOutput, failure: Failure | None = None) -> Case[ListDeploymentsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_deployments_provider_rejected, '__module__', "dex_connectors.emq.emqmock")

def list_deployments_invalid_response(value: ListDeploymentsOutput, failure: Failure | None = None) -> Case[ListDeploymentsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_deployments_invalid_response, '__module__', "dex_connectors.emq.emqmock")

def list_deployments_defect(value: ListDeploymentsOutput, failure: Failure | None = None) -> Case[ListDeploymentsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_deployments_defect, '__module__', "dex_connectors.emq.emqmock")

def list_deployments_retry(failure: Failure, after_seconds: float = 0) -> Case[ListDeploymentsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_deployments_retry, '__module__', "dex_connectors.emq.emqmock")

def list_deployments_two_deployments() -> Case[ListDeploymentsOutput]:
    return Case.branch_result(LISTED, ListDeploymentsOutput(deployments=(Deployment(deployment_id='mdfba81f', deployment_name='deployment-mdfba81f', deployment_type='serverless', project_name='default', region='N. Virginia (us-east-1)', status='stopped', version='v5', platform='AWS', connections=1000, tps=1000, created_at='2024-11-21 02:51'), Deployment(deployment_id='b1f77113', deployment_name='deployment-b1f77113', deployment_type='dedicatedFlex', project_name='test', region='N. Virginia (us-east-1)', status='running', version='v5', platform='AWS', connections=1000, tps=1000, created_at='2025-04-17 08:36'))))

setattr(list_deployments_two_deployments, '__module__', "dex_connectors.emq.emqmock")


class Mock:

    def __init__(self, connection_name: str = "emq") -> None:
        self.list_deployments_script: Query[ListDeploymentsInput, ListDeploymentsOutput] = Query(LIST_DEPLOYMENTS_DEFINITION)
        self.list_deployments_script.default(list_deployments_two_deployments())
        self._connection = new_connection_with_operations(
            list_deployments=self.list_deployments_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.emq.emqmock")

__all__ = [
    "Mock",
    "list_deployments_listed",
    "list_deployments_provider_rejected",
    "list_deployments_invalid_response",
    "list_deployments_defect",
    "list_deployments_retry",
    "list_deployments_two_deployments",
]
