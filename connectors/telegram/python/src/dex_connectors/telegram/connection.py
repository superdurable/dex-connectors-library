# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Telegram Bot connection and credential binding."""

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
from .client import CONNECTOR_ID as CONNECTOR_ID
from .client import GET_CHAT_DEFINITION as GET_CHAT_DEFINITION
from .client import SEND_MESSAGE_DEFINITION as SEND_MESSAGE_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import Chat as Chat
from .models import GetChatInput as GetChatInput
from .models import SendMessageInput as SendMessageInput
from .models import SentMessage as SentMessage

Connection.__doc__ = "Nonserializable Telegram Bot operations bound to a named connection."
new_connection.__doc__ = "Bind Telegram operations to a logical connection reference."
new_connection_with_operations.__doc__ = "Bind mock operations to the same Telegram connection identity."
new_project_connection.__doc__ = "Open project settings and resolve the Bot token per call."
