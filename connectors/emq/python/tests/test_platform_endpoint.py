# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reject unsafe Platform API endpoints before any credentialed request."""

import pytest
from dex_connectors.sdk import StaticCredentialProvider

from dex_connectors.emq import Client, Config


@pytest.mark.parametrize(
    "endpoint",
    [
        "",
        "http://example.com/public_api/v1",
        "https://example.com/public_api/v1/",
        "https://key:private-secret@example.com/public_api/v1",
        "https://example.com/public_api/v1?q=private-secret",
        "https://example.com/other",
    ],
)
def test_invalid_platform_endpoint_is_rejected_without_secret_leak(endpoint: str) -> None:
    """Require a scoped HTTPS Platform endpoint with no URL credentials or query."""
    with pytest.raises(ValueError) as error:
        Client(Config(platform_endpoint=endpoint), StaticCredentialProvider({}))
    assert "private-secret" not in str(error.value)
