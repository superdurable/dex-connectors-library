# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import list_deploys_listed as list_deploys_listed
from .._generated.mock import list_deploys_listed_default as list_deploys_listed_default
from .._generated.mock import list_deploys_not_found as list_deploys_not_found
from .._generated.mock import list_sites_listed as list_sites_listed
from .._generated.mock import list_sites_listed_default as list_sites_listed_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import LIST_DEPLOYS_DEFINITION as LIST_DEPLOYS_DEFINITION
from ..client import LIST_SITES_DEFINITION as LIST_SITES_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import Deploy as Deploy
from ..models import DeploysPage as DeploysPage
from ..models import ListDeploysInput as ListDeploysInput
from ..models import ListSitesInput as ListSitesInput
from ..models import Site as Site
from ..models import SitesPage as SitesPage

__all__ = [
    "Mock",
    "list_sites_listed",
    "list_sites_listed_default",
    "list_deploys_listed",
    "list_deploys_not_found",
    "list_deploys_listed_default",
]

Mock.__doc__ = "Script Netlify Queries, defaulting to their manifest answers."

list_sites_listed.__doc__ = "Script the successful listed route."
list_sites_listed_default.__doc__ = "Return the manifest's listSites listedDefault answer."
list_deploys_listed.__doc__ = "Script the successful listed route."
list_deploys_not_found.__doc__ = "Script a missing or inaccessible site."
list_deploys_listed_default.__doc__ = "Return the manifest's listDeploys listedDefault answer."
