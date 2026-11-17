# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public connection bindings generated from the Baidu Maps manifest."""

from ._generated.connection import Connection as Connection
from ._generated.connection import new_connection as new_connection
from ._generated.connection import new_connection_with_operations as new_connection_with_operations
from ._generated.connection import new_project_connection as new_project_connection

Connection.__doc__ = "Baidu Maps transport bound to a named logical connection."
new_connection.__doc__ = "Bind a Baidu Maps client to an explicit reference."
new_connection_with_operations.__doc__ = "Bind a fake geocoding Query for tests."
new_project_connection.__doc__ = "Load settings and resolve the server AK for each call."
