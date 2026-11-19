# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public Tuya named-connection bindings."""

from ._generated.connection import Connection as Connection
from ._generated.connection import new_connection as new_connection
from ._generated.connection import new_connection_with_operations as new_connection_with_operations
from ._generated.connection import new_project_connection as new_project_connection

Connection.__doc__ = "Named Tuya connection with private provider dependencies."
new_connection.__doc__ = "Bind a Tuya client to a connection reference."
new_connection_with_operations.__doc__ = "Bind a test Query to a connection reference."
new_project_connection.__doc__ = "Load Tuya settings and resolve private provider credentials per call."
