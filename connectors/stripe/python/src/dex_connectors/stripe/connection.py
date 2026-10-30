# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bind a Stripe provider client to one static Dex connection identity."""

from dataclasses import dataclass

import httpx
from dex_connectors.sdk import ConnectionRef
from dex_connectors.sdk.projectconfig import LoadedProject

from .client import Client
from .project import new_project_client


@dataclass(frozen=True, slots=True)
class Connection:
    """A Stripe client and logical connection bound before Worker registration.

    The caller closes the client after every Step using this connection stops.
    The reference is safe to use in Flow identity and never contains a key.
    """

    client: Client
    reference: ConnectionRef

    def __post_init__(self) -> None:
        """Reject a missing client or a connection for another provider."""
        if self.client is None or self.reference is None:
            raise ValueError("Stripe client and reference are required")
        self.reference.validate()
        if self.reference.provider != "stripe":
            raise ValueError("Stripe connection provider must be stripe")

    async def aclose(self) -> None:
        """Close this connection's client after all provider calls finish."""
        await self.client.aclose()


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind an existing client and Stripe reference for operation Steps.

    The caller retains ownership of the supplied client and closes it after
    the Worker stops.
    """
    return Connection(client, reference)


def new_project_connection(
    project: LoadedProject,
    connection_name: str,
    http_client: httpx.AsyncClient | None = None,
) -> Connection:
    """Open a declared Stripe connection with call-time private credentials.

    Close the returned connection after all Worker calls finish. An injected
    HTTP client remains owned by its caller.
    """
    client = new_project_client(project, connection_name, http_client)
    return new_connection(client, ConnectionRef("stripe", connection_name))
