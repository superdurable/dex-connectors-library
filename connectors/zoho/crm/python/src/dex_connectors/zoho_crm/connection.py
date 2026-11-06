# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Generated Zoho CRM connection and OAuth project binding."""

import asyncio

import httpx
from dex_connectors.sdk.projectconfig import LoadedProject

from ._generated.connection import (
    Connection,
    decode_credentials,
    decode_shared_credentials,
    encode_credentials,
    new_connection,
    new_connection_with_operations,
)
from ._generated.connection import new_project_connection as _generated_new_project_connection
from .client import _ProjectHTTPClient


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Bind generated OAuth wiring with a redirect-safe, owned default HTTP client."""
    if http_client is not None:
        return _generated_new_project_connection(project, connection_name, http_client)
    owned_client = _ProjectHTTPClient(timeout=10, follow_redirects=False)
    try:
        return _generated_new_project_connection(project, connection_name, owned_client)
    except Exception:
        # Construction failed before the connection can own this client.
        try:
            asyncio.get_running_loop().create_task(owned_client.aclose())
        except RuntimeError:
            asyncio.run(owned_client.aclose())
        raise


__all__ = [
    "Connection",
    "decode_credentials",
    "decode_shared_credentials",
    "encode_credentials",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
]
