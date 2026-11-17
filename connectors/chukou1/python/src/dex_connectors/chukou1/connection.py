# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Named Chukou1 connection and credential binding."""

from ._generated.connection import Connection as Connection
from ._generated.connection import new_connection as new_connection
from ._generated.connection import new_connection_with_operations as new_connection_with_operations
from ._generated.connection import new_project_connection as new_project_connection

Connection.__doc__ = "Nonserializable Chukou1 operation binding for one named connection."
new_connection.__doc__ = "Bind a Chukou1 client to a logical connection reference."
new_connection_with_operations.__doc__ = "Bind a mock operation to the same connection identity."
new_project_connection.__doc__ = "Load ordinary settings and resolve current credentials per call."
