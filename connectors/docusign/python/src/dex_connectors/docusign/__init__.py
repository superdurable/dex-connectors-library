# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native DocuSign eSignature operations and the envelopeEventReceived Connect Trigger."""

from .client import Client, CombinedDocumentStore
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import *  # noqa: F403
from .steps import *  # noqa: F403
from .triggers import *  # noqa: F403
from .triggers import __all__ as _trigger_names

__all__ = [
    "Client",
    "CombinedDocumentStore",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    *_trigger_names,
]
