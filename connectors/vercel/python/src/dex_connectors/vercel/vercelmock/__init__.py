# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Vercel scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_DEPLOYMENT_DEFINITION,
    LIST_DEPLOYMENTS_DEFINITION,
    LISTED,
    NOT_FOUND,
)
from ..connection import Connection, new_connection_with_operations
from ..models import Deployment, DeploymentsPage, GetDeploymentInput, ListDeploymentsInput, Pagination

_DEPLOYMENT = Deployment(
    "dpl_meridian1", "", "meridian-web", "meridian-web.vercel.app", "READY", "", 1761436800000
)


def list_deployments_listed(value: DeploymentsPage, failure: Failure | None = None) -> Case[DeploymentsPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_deployments_listed_default() -> Case[DeploymentsPage]:
    """Return the manifest's listDeployments default answer."""
    return list_deployments_listed(DeploymentsPage((_DEPLOYMENT,), Pagination(None)))


def get_deployment_found(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_deployment_not_found(value: Deployment, failure: Failure | None = None) -> Case[Deployment]:
    """Script a missing or inaccessible deployment."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_deployment_found_default() -> Case[Deployment]:
    """Return the manifest's getDeployment default answer."""
    return get_deployment_found(_DEPLOYMENT)


class Mock:
    """Script Vercel Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "vercel") -> None:
        self.list_deployments_script: Query[ListDeploymentsInput, DeploymentsPage] = Query(
            LIST_DEPLOYMENTS_DEFINITION
        )
        self.list_deployments_script.default(list_deployments_listed_default())
        self.get_deployment_script: Query[GetDeploymentInput, Deployment] = Query(GET_DEPLOYMENT_DEFINITION)
        self.get_deployment_script.default(get_deployment_found_default())
        self._connection = new_connection_with_operations(
            self.list_deployments_script,
            self.get_deployment_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_deployments_listed",
    "list_deployments_listed_default",
    "get_deployment_found",
    "get_deployment_not_found",
    "get_deployment_found_default",
]
