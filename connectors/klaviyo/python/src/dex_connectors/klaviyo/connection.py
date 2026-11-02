# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Klaviyo connection bindings from the manifest contract."""

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
from .client import CREATE_EVENT_DEFINITION as CREATE_EVENT_DEFINITION
from .client import GET_PROFILE_DEFINITION as GET_PROFILE_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import CreateEventInput as CreateEventInput
from .models import EventAcceptance as EventAcceptance
from .models import GetProfileInput as GetProfileInput
from .models import Profile as Profile

Connection.__doc__ = "Nonserializable Klaviyo operation bindings for a named project connection."
Connection.create_event.__doc__ = "Return the event Mutation used by an application Step."
Connection.get_profile.__doc__ = "Return the profile Query used by an application Step."
Connection.aclose.__doc__ = "Close the owned provider transport after Worker shutdown."
new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = (
    "Open declared ordinary settings and resolve the current private key per call."
)
Connection.__repr__.__doc__ = "Keep provider transport and credentials out of logs."
Connection.__reduce__.__doc__ = "Prevent a live connection from entering durable Flow state."
