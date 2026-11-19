# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public connection bindings generated from the Aqara manifest."""

from ._generated.connection import Connection as Connection
from ._generated.connection import new_connection as new_connection
from ._generated.connection import new_connection_with_operations as new_connection_with_operations
from ._generated.connection import new_project_connection as new_project_connection

Connection.__doc__ = "Aqara transport bound to a named logical connection."
new_connection.__doc__ = "Bind an Aqara client to an explicit reference."
new_connection_with_operations.__doc__ = "Bind a fake device Query for tests."
new_project_connection.__doc__ = "Load settings and resolve credentials per call."
