# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Freshdesk operation and project connection bindings."""

from ._generated.connection import (
    Connection,
    new_connection,
    new_connection_with_operations,
    new_project_connection,
)

__all__ = ["Connection", "new_connection", "new_connection_with_operations", "new_project_connection"]
