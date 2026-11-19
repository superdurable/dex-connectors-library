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
from dex_connectors.discord._generated.connection import CredentialMaterialError

REFERENCE = ConnectionRef("discord", "discord")


def _sdk_decoding_failure() -> ValueError:
    """Build the SDK project provider's wrapper around a malformed stored credential."""
    try:
        try:
            raise CredentialMaterialError("Discord credential bot_token is required")
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "branch"),
    [
        (ReauthorizationRequiredError(), "providerRejected"),
        (ObjectNotFoundError(), "defect"),
        (ConnectionNotSelectedError(), "defect"),
        (CredentialBrokerRejectedError(), "defect"),
        (CredentialMaterialError("Discord credential bot_token is required"), "defect"),
        (_sdk_decoding_failure(), "defect"),
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
