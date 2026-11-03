# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the shared text-generation Query against a local Chat Completions mock."""

import asyncio
from datetime import timedelta
from typing import Any, cast

import httpx

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    QueryDefinition,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.textgen import (
    Message,
    MessageRole,
    TextGenerationQuery,
    TextGenerationQueryConfig,
    TextGenerationRequest,
    text_generation_branch_definitions,
)
from dex_connectors.sdk.textgen.openaichat import Profile, new_wire_format


class ExampleContext:
    """Minimal heartbeat surface used outside a Dex Worker for local preview."""

    async def heartbeat(self, value: object) -> None:
        """Accept inert heartbeats while the mocked exchange runs."""


async def main() -> None:
    """Send one request and require the generated branch."""
    operation = OperationRef("example-textgen", "generateText")
    definition = QueryDefinition(
        operation, text_generation_branch_definitions(), StepDefaults(timedelta(minutes=1), 1)
    )

    async def resolve_credential(call: Call) -> SecretString:
        return SecretString("local-test-key")

    def provider(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "id": "response-1",
                "model": "example-model",
                "choices": [{"index": 0, "message": {"content": "Hello!"}, "finish_reason": "stop"}],
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(provider)) as client:
        query = TextGenerationQuery(
            TextGenerationQueryConfig(
                definition,
                new_wire_format(Profile("example-textgen")),
                "https://provider.example/v1",
                "example-model",
                20,
                resolve_credential,
                65536,
                4096,
                client,
            )
        )
        call = Call(
            CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
            ConnectionRef("example-textgen", "demo"),
            operation,
            context=cast(Any, ExampleContext()),
        )
        result = await query.invoke(call, TextGenerationRequest((Message(MessageRole.USER, "Hello"),)))
        if result.branch != "generated" or result.value is None:
            raise RuntimeError("text generation did not complete")
        print(result.value.text)


if __name__ == "__main__":
    asyncio.run(main())
