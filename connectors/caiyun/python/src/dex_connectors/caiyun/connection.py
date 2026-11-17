# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public named Caiyun connection bindings."""

from ._generated.connection import Connection as Connection
from ._generated.connection import new_connection as new_connection
from ._generated.connection import new_connection_with_operations as new_connection_with_operations
from ._generated.connection import new_project_connection as new_project_connection

Connection.__doc__ = "Caiyun transport bound to a named connection."
new_connection.__doc__ = "Bind a Caiyun client to a connection reference."
new_connection_with_operations.__doc__ = "Bind a fake Caiyun Query for tests."
new_project_connection.__doc__ = "Load Caiyun settings and credentials from a project."
