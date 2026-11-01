# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_deployment_found as get_deployment_found
from .._generated.mock import get_deployment_found_default as get_deployment_found_default
from .._generated.mock import get_deployment_not_found as get_deployment_not_found
from .._generated.mock import list_deployments_listed as list_deployments_listed
from .._generated.mock import list_deployments_listed_default as list_deployments_listed_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_DEPLOYMENT_DEFINITION as GET_DEPLOYMENT_DEFINITION
from ..client import LIST_DEPLOYMENTS_DEFINITION as LIST_DEPLOYMENTS_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import Deployment as Deployment
from ..models import DeploymentsPage as DeploymentsPage
from ..models import GetDeploymentInput as GetDeploymentInput
from ..models import ListDeploymentsInput as ListDeploymentsInput
from ..models import Pagination as Pagination

__all__ = [
    "Mock",
    "list_deployments_listed",
    "list_deployments_listed_default",
    "get_deployment_found",
    "get_deployment_not_found",
    "get_deployment_found_default",
]

Mock.__doc__ = "Script Vercel Queries, defaulting to their manifest answers."

list_deployments_listed.__doc__ = "Script the successful listed route."
list_deployments_listed_default.__doc__ = "Return the manifest's listDeployments default answer."
get_deployment_found.__doc__ = "Script the successful found route."
get_deployment_not_found.__doc__ = "Script a missing or inaccessible deployment."
get_deployment_found_default.__doc__ = "Return the manifest's getDeployment default answer."
