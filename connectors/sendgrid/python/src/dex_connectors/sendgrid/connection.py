# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SendGrid connection and project credential binding."""

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
from .client import GET_STATS_DEFINITION as GET_STATS_DEFINITION
from .client import LIST_TEMPLATES_DEFINITION as LIST_TEMPLATES_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import GetStatsInput as GetStatsInput
from .models import ListTemplatesInput as ListTemplatesInput
from .models import StatsOutput as StatsOutput
from .models import TemplatesOutput as TemplatesOutput
