# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Match eSign's Go-reviewed canonical signature vector."""

from dex_connectors.esign.client import _sign_read_request


def test_sign_read_request_matches_go_vector() -> None:
    """Keep empty Content-MD5 and Date separators in the signed string."""
    assert _sign_read_request("test-secret", "/v3/sign-flow/flow123/detail") == (
        "C9P4BkL3d1BJ0EE390AVvnpUMYhDTix72iBm4yWPiZs="
    )
