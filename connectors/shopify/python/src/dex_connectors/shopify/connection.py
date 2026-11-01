# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Shopify connection bindings from the manifest contract."""

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

Connection.__doc__ = "Nonserializable Shopify operation bindings for a named project connection."
Connection.list_orders.__doc__ = "Return the order page Query used by an application Step."
Connection.get_order.__doc__ = "Return the order lookup Query used by an application Step."
Connection.aclose.__doc__ = "Close the owned provider transport after Worker shutdown."
new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = (
    "Open declared ordinary settings and resolve the current access token per call."
)
Connection.__repr__.__doc__ = "Keep provider transport and credentials out of logs."
Connection.__reduce__.__doc__ = "Prevent a live connection from entering durable Flow state."
