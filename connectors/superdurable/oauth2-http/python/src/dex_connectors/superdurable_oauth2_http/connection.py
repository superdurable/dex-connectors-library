# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest generated connection construction."""

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    Mutation,
    MutationAttempt,
    MutationDefinition,
)
from dex_connectors.sdk.projectconfig import LoadedProject

from ._generated.connection import Connection as _GeneratedConnection
from ._generated.connection import new_project_connection as _new_generated_project_connection
from ._generated.definitions import POST_NON_IDEMPOTENT_DEFINITION
from .client import Client
from .models import PostNonIdempotentInput, SendRequestInput, SendRequestOutput

__all__ = ["Connection", "new_connection", "new_connection_with_operations", "new_project_connection"]

_UNBOUND_MESSAGE = "OAuth 2.0 HTTP connection was built without the postNonIdempotent operation"


class Connection(_GeneratedConnection):
    """Bind the sendRequest and postNonIdempotent operations to one static Dex connection name.

    The constructor keeps the released `Connection(reference, send_request, client)` call shape.
    `post_non_idempotent` is keyword-only and optional; without it, `post_non_idempotent()` and a
    `new_post_non_idempotent_step` built on this connection raise ValueError. A connection that
    owns `client` closes it in `aclose`.
    """

    __slots__ = ("_has_post_non_idempotent",)

    def __init__(
        self,
        reference: ConnectionRef,
        send_request: Mutation[SendRequestInput, SendRequestOutput],
        client: Client | None = None,
        *,
        post_non_idempotent: Mutation[PostNonIdempotentInput, SendRequestOutput] | None = None,
    ) -> None:
        """Validate the operation identities and bind them to `reference`."""
        bound_post_non_idempotent: Mutation[PostNonIdempotentInput, SendRequestOutput] = (
            post_non_idempotent if post_non_idempotent is not None else _UnboundPostNonIdempotent()
        )
        super().__init__(reference, send_request, bound_post_non_idempotent, client)
        self._has_post_non_idempotent = post_non_idempotent is not None

    def post_non_idempotent(self) -> Mutation[PostNonIdempotentInput, SendRequestOutput]:
        """Return the single-dispatch POST operation, or raise ValueError when it was not bound."""
        if not self._has_post_non_idempotent:
            raise ValueError(_UNBOUND_MESSAGE)
        return super().post_non_idempotent()


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client's operations to a logical connection reference; the connection owns `client`."""
    if client is None:
        raise ValueError("OAuth 2.0 HTTP client is required")
    return Connection(
        reference, client.send_request(), client, post_non_idempotent=client.post_non_idempotent()
    )


def new_connection_with_operations(
    send_request: Mutation[SendRequestInput, SendRequestOutput],
    reference: ConnectionRef,
    *,
    post_non_idempotent: Mutation[PostNonIdempotentInput, SendRequestOutput] | None = None,
) -> Connection:
    """Bind test or mock operations to an OAuth 2.0 HTTP connection name.

    `post_non_idempotent` is optional so earlier `(send_request, reference)` callers keep
    working; without it, a `new_post_non_idempotent_step` built on this connection raises
    ValueError.
    """
    return Connection(reference, send_request, post_non_idempotent=post_non_idempotent)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open a declared project connection whose credentials resolve and refresh per call.

    Raises ValueError for a missing or invalid project connection, or when `http_client` is
    given, because the private token endpoint requires the connector's own HTTP client.
    """
    generated = _new_generated_project_connection(project, connection_name, http_client)
    return Connection(
        generated.reference,
        generated.send_request(),
        generated._client,
        post_non_idempotent=generated.post_non_idempotent(),
    )


class _UnboundPostNonIdempotent:
    """Satisfy the generated identity check; the owning connection never exposes it."""

    def definition(self) -> MutationDefinition:
        return POST_NON_IDEMPOTENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostNonIdempotentInput) -> IdempotencyKey:
        raise ValueError(_UNBOUND_MESSAGE)

    async def invoke(self, call: Call, input: PostNonIdempotentInput) -> MutationAttempt[SendRequestOutput]:
        raise ValueError(_UNBOUND_MESSAGE)
