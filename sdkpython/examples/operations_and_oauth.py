# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable operation, credential, OAuth, and dispatch-decision example."""

import asyncio
from datetime import UTC, datetime, timedelta

import httpx

from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    ConnectionRef,
    CredentialRefreshResult,
    CredentialRefreshState,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    OperationRef,
    Receipt,
    StaticCredentialProvider,
    StepDefaults,
    resolve_credential,
    run_mutation,
)
from dex_connectors.sdk.credentials import SecretString
from dex_connectors.sdk.oauthtoken import (
    ClientAuthenticationMethod,
    ClientCredentials,
    TokenEndpoint,
)
from dex_connectors.sdk.singledispatch import DecisionKind, decide_dispatch


class ExampleRefreshDriver:
    """Inert driver that leaves local example credentials unchanged."""

    def refresh_required(self, state: CredentialRefreshState[SecretString]) -> bool:
        """Report no expiry for this local static example."""
        return False

    async def refresh(
        self, state: CredentialRefreshState[SecretString]
    ) -> CredentialRefreshResult[SecretString]:
        """Return inert replacement material if a host calls refresh."""
        return CredentialRefreshResult(state.credentials, state.now + timedelta(hours=1))


class CreateExampleItem:
    """Mutation whose provider write is represented by an inert local result."""

    def definition(self) -> MutationDefinition:
        """Declare the stable operation and its happy and defect routes."""
        return MutationDefinition(
            OperationRef("sample", "createItem"),
            (
                BranchDefinition("created", "Item created"),
                BranchDefinition("defect", "Connector defect", True),
            ),
            StepDefaults(timedelta(seconds=10), 1),
        )

    def idempotency_key(self, call_id: CallID, input: str) -> IdempotencyKey:
        """Use the stable Call ID as the example provider key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Return one definite, repeatable provider outcome."""
        return MutationAttempt.branch_result(
            "created", input, Receipt(call.id, "sample", datetime.now(UTC))
        )


async def main() -> None:
    """Run the public APIs without a Dex Worker or real provider credentials."""
    call_id = CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
    connection = ConnectionRef("sample", "default")
    call = Call(call_id, connection, OperationRef("sample", "createItem"))
    credentials = StaticCredentialProvider({connection: SecretString("INERT_SECRET")})
    credential = await resolve_credential(credentials, call, ExampleRefreshDriver())
    assert credential.reveal() == "INERT_SECRET"
    result = await run_mutation(CreateExampleItem(), call, "item-1")
    assert result.branch == "created" and result.receipt.idempotency_key == str(call_id)
    assert decide_dispatch(call_id, 1, None).kind == DecisionKind.SEND_ALLOWED

    def token_response(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200, json={"access_token": "INERT_ACCESS", "expires_in": 3600, "token_type": "Bearer"}
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(token_response)) as http_client:
        endpoint = TokenEndpoint("Example", "https://oauth.example/token", http_client)
        oauth_client = ClientCredentials(
            "example-client", SecretString("INERT_CLIENT_SECRET"),
            ClientAuthenticationMethod.CLIENT_SECRET_POST,
        )
        token = await endpoint.exchange_refresh_token(oauth_client, SecretString("INERT_REFRESH"))
        assert token.access_token.reveal() == "INERT_ACCESS"


if __name__ == "__main__":
    asyncio.run(main())
