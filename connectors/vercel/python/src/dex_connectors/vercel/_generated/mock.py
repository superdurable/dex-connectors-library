# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Deployment, DeploymentsPage, DeploymentsPagePagination, GetDeploymentInput, ListDeploymentsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_DEPLOYMENT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_DEPLOYMENTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_deployments_listed(value: DeploymentsPage, failure: Failure | None = None) -> Case[DeploymentsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_deployments_listed, '__module__', "dex_connectors.vercel.vercelmock")

def list_deployments_provider_rejected(value: DeploymentsPage, failure: Failure | None = None) -> Case[DeploymentsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_deployments_provider_rejected, '__module__', "dex_connectors.vercel.vercelmock")

def list_deployments_invalid_response(value: DeploymentsPage, failure: Failure | None = None) -> Case[DeploymentsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_deployments_invalid_response, '__module__', "dex_connectors.vercel.vercelmock")

def list_deployments_defect(value: DeploymentsPage, failure: Failure | None = None) -> Case[DeploymentsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_deployments_defect, '__module__', "dex_connectors.vercel.vercelmock")

def list_deployments_retry(failure: Failure, after_seconds: float = 0) -> Case[DeploymentsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_deployments_retry, '__module__', "dex_connectors.vercel.vercelmock")

def list_deployments_listed_default() -> Case[DeploymentsPage]:
    return Case.branch_result(LISTED, DeploymentsPage(deployments=(Deployment(uid='dpl_meridian1', name='meridian-web', url='meridian-web.vercel.app', state='READY', created=1761436800000),), pagination=DeploymentsPagePagination(next=None)))

setattr(list_deployments_listed_default, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_found(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_deployment_found, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_not_found(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_deployment_not_found, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_provider_rejected(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_deployment_provider_rejected, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_invalid_response(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_deployment_invalid_response, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_defect(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_deployment_defect, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_retry(failure: Failure, after_seconds: float = 0) -> Case[Deployment]:
    return Case.retry(failure, after_seconds)

setattr(get_deployment_retry, '__module__', "dex_connectors.vercel.vercelmock")

def get_deployment_found_default() -> Case[Deployment]:
    return Case.branch_result(FOUND, Deployment(uid='dpl_meridian1', name='meridian-web', url='meridian-web.vercel.app', state='READY', created=1761436800000))

setattr(get_deployment_found_default, '__module__', "dex_connectors.vercel.vercelmock")


class Mock:

    def __init__(self, connection_name: str = "vercel") -> None:
        self.list_deployments_script: Query[ListDeploymentsInput, DeploymentsPage] = Query(LIST_DEPLOYMENTS_DEFINITION)
        self.list_deployments_script.default(list_deployments_listed_default())
        self.get_deployment_script: Query[GetDeploymentInput, Deployment] = Query(GET_DEPLOYMENT_DEFINITION)
        self.get_deployment_script.default(get_deployment_found_default())
        self._connection = new_connection_with_operations(
            list_deployments=self.list_deployments_script,
            get_deployment=self.get_deployment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.vercel.vercelmock")

__all__ = [
    "Mock",
    "list_deployments_listed",
    "list_deployments_provider_rejected",
    "list_deployments_invalid_response",
    "list_deployments_defect",
    "list_deployments_retry",
    "list_deployments_listed_default",
    "get_deployment_found",
    "get_deployment_not_found",
    "get_deployment_provider_rejected",
    "get_deployment_invalid_response",
    "get_deployment_defect",
    "get_deployment_retry",
    "get_deployment_found_default",
]
