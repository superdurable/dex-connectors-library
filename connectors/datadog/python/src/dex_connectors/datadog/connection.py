# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Datadog connection and project credential binding."""

from typing import Any as Any

import httpx as httpx
from dex_connectors.sdk import Call as Call
from dex_connectors.sdk import ConnectionRef as ConnectionRef
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
from .client import LIST_MONITORS_DEFINITION as LIST_MONITORS_DEFINITION
from .client import QUERY_METRICS_DEFINITION as QUERY_METRICS_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import ListMonitorsInput as ListMonitorsInput
from .models import MetricsQuery as MetricsQuery
from .models import MonitorsPage as MonitorsPage
from .models import QueryMetricsInput as QueryMetricsInput

Connection.__doc__ = "Nonserializable Datadog operation bindings for a named project connection."

new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = (
    "Open declared ordinary settings and resolve the current API and application keys per call."
)
