# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Zendesk Support ticket operations and the ticketChanged poll Trigger."""

from .client import DEFAULT_INCREMENTAL_EXPORT_REQUESTS_PER_MINUTE, Client
from .connection import (
    Connection,
    new_connection,
    new_connection_with_operations,
    new_project_connection,
)
from .models import *  # noqa: F403
from .steps import *  # noqa: F403
from .triggers import *  # noqa: F403
from .triggers import __all__ as _trigger_exports

__all__ = [
    "DEFAULT_INCREMENTAL_EXPORT_REQUESTS_PER_MINUTE",
    "Client",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    *_trigger_exports,
]
