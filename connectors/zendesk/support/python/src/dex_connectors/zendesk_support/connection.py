# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bind the generated Zendesk Support connection and project credentials."""

from ._generated.connection import (
    Connection,
    new_connection,
    new_connection_with_operations,
    new_project_connection,
)

__all__ = ["Connection", "new_connection", "new_connection_with_operations", "new_project_connection"]
