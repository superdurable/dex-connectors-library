# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Smartsheet connection and project credential binding."""

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
from .client import GET_SHEET_DEFINITION as GET_SHEET_DEFINITION
from .client import LIST_SHEETS_DEFINITION as LIST_SHEETS_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import GetSheetInput as GetSheetInput
from .models import ListSheetsInput as ListSheetsInput
from .models import Sheet as Sheet
from .models import SheetPage as SheetPage

Connection.__doc__ = "Nonserializable Smartsheet operation bindings for a named project connection."

new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = (
    "Open declared ordinary settings and resolve the current private key per call."
)
