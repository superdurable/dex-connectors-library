# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Supabase connection bindings from the manifest contract."""

from ._generated.connection import (
    Connection as Connection,
)
from ._generated.connection import (
    new_connection as new_connection,
)
from ._generated.connection import (
    new_connection_with_operations as new_connection_with_operations,
)
from ._generated.connection import (
    new_project_connection as new_project_connection,
)

Connection.__doc__ = "Nonserializable Supabase operation bindings for a named project connection."
Connection.upsert_rows.__doc__ = "Return the row upsert Mutation used by an application Step."
Connection.select_rows.__doc__ = "Return the row selection Query used by an application Step."
Connection.aclose.__doc__ = "Close the owned provider transport after Worker shutdown."
new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = "Open declared ordinary settings and resolve the current API key per call."
Connection.__repr__.__doc__ = "Keep provider transport and credentials out of logs."
Connection.__reduce__.__doc__ = "Prevent a live connection from entering durable Flow state."
