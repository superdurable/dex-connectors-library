# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One credential classification: only listed failures are terminal; the rest retry."""

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    OperationRef,
    ReauthorizationRequiredError,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)

import dex_connectors.zoom as zoom
from dex_connectors.zoom._generated.connection import CredentialMaterialError

CONNECTION = ConnectionRef("zoom", "zoom-test")


def sdk_wrapped_material_error() -> ValueError:
    """Build the error the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("Zoom credential access_token is required")
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialBrokerUnavailableError(), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("unlisted"), False),
        (CredentialMaterialError("Zoom credential access_token is required"), True),
        (sdk_wrapped_material_error(), True),
    ],
)
async def test_credential_resolution_classification(error: Exception, is_terminal: bool) -> None:
    class FailingCredentials:
        async def resolve(self, call: Call) -> zoom.Credentials:
            raise error

    config = zoom.Config(endpoint="https://zoom.example/v2")
    client = zoom.Client(config, FailingCredentials())  # type: ignore[arg-type]
    call_id = CallID("d095d4cd-8f7d-53db-926e-50307d935d1f")
    call = Call(call_id, CONNECTION, OperationRef("zoom", "createMeeting"))
    credentials, problem, is_retryable = await client.resolve(call, "createMeeting")
    assert credentials is None and problem is not None
    assert is_retryable is not is_terminal
    assert problem.kind == (FailureKind.AUTHENTICATION if is_terminal else FailureKind.AVAILABILITY)
