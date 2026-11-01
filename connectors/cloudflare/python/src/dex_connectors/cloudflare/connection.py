# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cloudflare connection bindings from the manifest contract."""

from typing import Any as Any

import httpx as httpx
from dex_connectors.sdk import Call as Call
from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Mutation as Mutation
from dex_connectors.sdk import Query as Query
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey as ConnectionKey
from dex_connectors.sdk.projectconfig import LoadedProject as LoadedProject

from ._generated.connection import (
    Connection as Connection,
)
from ._generated.connection import (
    new_connection as new_connection,
)
from ._generated.connection import (
    new_connection_with_operations as new_connection_with_operations,
)
from ._generated.connection import (
    new_project_connection as new_project_connection,
)
from .client import CONNECTOR_ID as CONNECTOR_ID
from .client import LIST_ZONES_DEFINITION as LIST_ZONES_DEFINITION
from .client import PURGE_CACHE_BY_URLS_DEFINITION as PURGE_CACHE_BY_URLS_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import CachePurge as CachePurge
from .models import ListZonesInput as ListZonesInput
from .models import PurgeCacheByURLsInput as PurgeCacheByURLsInput
from .models import ZonesPage as ZonesPage

Connection.__doc__ = "Nonserializable Cloudflare operation bindings for a named project connection."
Connection.purge_cache_by_urls.__doc__ = "Return the URL cache purge Mutation used by an application Step."
Connection.list_zones.__doc__ = "Return the zone listing Query used by an application Step."
Connection.aclose.__doc__ = "Close the owned provider transport after Worker shutdown."
new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = "Open declared ordinary settings and resolve the current API token per call."
Connection.__repr__.__doc__ = "Keep provider transport and credentials out of logs."
Connection.__reduce__.__doc__ = "Prevent a live connection from entering durable Flow state."
