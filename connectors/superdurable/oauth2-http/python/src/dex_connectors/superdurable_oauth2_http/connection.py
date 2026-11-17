# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest generated connection construction."""

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    Mutation,
    MutationAttempt,
    MutationDefinition,
)

from ._generated.connection import (
    Connection,
    new_connection,
    new_project_connection,
)
from ._generated.definitions import POST_NON_IDEMPOTENT_DEFINITION
from .models import PostNonIdempotentInput, SendRequestInput, SendRequestOutput

__all__ = ["Connection", "new_connection", "new_connection_with_operations", "new_project_connection"]

_UNBOUND_MESSAGE = "OAuth 2.0 HTTP connection was built without the postNonIdempotent operation"


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
    if post_non_idempotent is None:
        return _ConnectionWithoutPostNonIdempotent(reference, send_request)
    return Connection(reference, send_request, post_non_idempotent)


class _ConnectionWithoutPostNonIdempotent(Connection):
    __slots__ = ()

    def __init__(
        self, reference: ConnectionRef, send_request: Mutation[SendRequestInput, SendRequestOutput]
    ) -> None:
        super().__init__(reference, send_request, _UnboundPostNonIdempotent())

    def post_non_idempotent(self) -> Mutation[PostNonIdempotentInput, SendRequestOutput]:
        raise ValueError(_UNBOUND_MESSAGE)


class _UnboundPostNonIdempotent:
    """Satisfy the generated identity check; the owning connection never exposes it."""

    def definition(self) -> MutationDefinition:
        return POST_NON_IDEMPOTENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostNonIdempotentInput) -> IdempotencyKey:
        raise ValueError(_UNBOUND_MESSAGE)

    async def invoke(self, call: Call, input: PostNonIdempotentInput) -> MutationAttempt[SendRequestOutput]:
        raise ValueError(_UNBOUND_MESSAGE)
