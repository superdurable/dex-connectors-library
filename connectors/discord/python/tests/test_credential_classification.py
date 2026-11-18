# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One credential classification: only listed failures are terminal; the rest retry."""

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef, ReauthorizationRequiredError
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.discord import Client, Config, Credentials

REFERENCE = ConnectionRef("discord", "discord")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "branch"),
    [
        (ReauthorizationRequiredError(), "providerRejected"),
        (ObjectNotFoundError(), "defect"),
        (ConnectionNotSelectedError(), "defect"),
        (CredentialBrokerRejectedError(), "defect"),
        (CredentialBrokerUnavailableError(), "retry"),
        (ValueError("connection is not configured"), "retry"),
        (RuntimeError("unlisted"), "retry"),
    ],
)
async def test_credential_resolution_classification(error: Exception, branch: str) -> None:
    class FailingCredentials:
        async def resolve(self, call: Call) -> Credentials:
            raise error

    client = Client(Config(), FailingCredentials())  # type: ignore[arg-type]
    call = Call(CallID("credential-classification"), REFERENCE, OperationRef("discord", "createMessage"))
    token, failure, selected = await client.resolve_token(call, "createMessage")
    assert token is None and failure is not None
    assert selected == branch
