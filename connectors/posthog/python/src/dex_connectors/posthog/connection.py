# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PostHog connection and secret binding."""

from typing import Any as Any

import httpx as httpx
from dex_connectors.sdk import Call as Call
from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Mutation as Mutation
from dex_connectors.sdk import Query as Query
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey as ConnectionKey
from dex_connectors.sdk.projectconfig import LoadedProject as LoadedProject

from ._generated.connection import Connection as Connection
from ._generated.connection import new_connection as new_connection
from ._generated.connection import new_connection_with_operations as new_connection_with_operations
from ._generated.connection import new_project_connection as new_project_connection
from .client import CAPTURE_EVENT_DEFINITION as CAPTURE_EVENT_DEFINITION
from .client import CONNECTOR_ID as CONNECTOR_ID
from .client import LIST_INSIGHTS_DEFINITION as LIST_INSIGHTS_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import CapturedEvent as CapturedEvent
from .models import CaptureEventInput as CaptureEventInput
from .models import InsightsPage as InsightsPage
from .models import ListInsightsInput as ListInsightsInput

Connection.__doc__ = "Nonserializable PostHog operations bound to a named connection."
new_connection.__doc__ = "Bind PostHog operations to a logical connection reference."
new_connection_with_operations.__doc__ = "Bind mock operations to the same PostHog connection identity."
new_project_connection.__doc__ = "Open project settings and resolve PostHog credentials per attempt."
