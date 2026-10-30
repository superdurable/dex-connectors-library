# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Open a Gmail provider from a loaded Dex project connection."""

import httpx
from dex_connectors.sdk import ConnectionRef
from dex_connectors.sdk.projectconfig import LoadedProject
from dex_connectors.sdk.projectconfig.connections import ConnectionKey
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider

from .connection import Connection, new_connection
from .models import Config
from .project_credentials import decode_credentials, encode_credentials
from .provider import GmailProvider


def new_project_provider(
    project: LoadedProject,
    connection_name: str,
    *,
    http_client: httpx.AsyncClient | None = None,
) -> GmailProvider:
    """Use pinned Gmail settings and renewable call-time credentials from Dex Web."""
    if project is None or not connection_name.strip():
        raise ValueError("loaded project and static Gmail connection name are required")
    entry = next(
        (
            item
            for item in project.configuration.connections
            if item.connector_id == "gmail" and item.connection_name == connection_name
        ),
        None,
    )
    if entry is None or entry.provider != "google":
        raise ValueError("Gmail project connection is not configured")
    key = ConnectionKey("gmail", connection_name)
    credential_source = RefreshingCredentialProvider(
        project.connections,
        key,
        decode_credentials,
        encode_credentials,
        lambda material: decode_credentials(material, shared=True),
    )
    return GmailProvider(
        Config.from_project_settings(entry.configuration), credential_source, http_client=http_client
    )


def new_project_connection(
    project: LoadedProject,
    connection_name: str,
    *,
    http_client: httpx.AsyncClient | None = None,
) -> Connection:
    """Open a named Dex Web Gmail connection with project-backed credentials."""
    client = new_project_provider(project, connection_name, http_client=http_client)
    return new_connection(client, ConnectionRef("google", connection_name))


__all__ = ["new_project_provider", "new_project_connection"]
