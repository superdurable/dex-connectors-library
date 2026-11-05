# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Google-compatible encoding and strict bounded response decoding."""

import httpx
from dex_connectors.sdk import FailureKind

from dex_connectors.google_spreadsheet.client import ProviderResponse, encode_query, escape_path
from dex_connectors.google_spreadsheet.operations import _decode_values, _retry_delay


def test_path_and_query_encoding_match_go_url_package() -> None:
    """Preserve Go's distinct PathEscape and url.Values.Encode safe sets."""
    assert escape_path("a! '()*+,;?/~$&+=:@") == "a%21%20%27%28%29%2A+%2C%3B%3F%2F~$&+=:@"
    assert encode_query({"z~": "a b*~", "a": "+"}) == "a=%2B&z~=a+b%2A~"


def test_values_decoder_rejects_wrong_types_and_keeps_null_collections() -> None:
    """Wrong-typed provider fields are protocol failures; JSON null is a Go zero value."""
    decoded, kind = _decode_values(ProviderResponse(200, body=b'{"values":"not rows"}'), 10)
    assert decoded is None and kind == FailureKind.PROTOCOL
    decoded, kind = _decode_values(ProviderResponse(200, body=b'{"values":[null,[null,true,3]]}'), 10)
    assert kind is None and decoded is not None
    assert decoded.values == ((), ("<nil>", "true", "3"))


def test_retry_after_requires_nonnegative_integer() -> None:
    """Google accepts Go integer delta-seconds, including a leading plus sign."""
    assert _retry_delay(ProviderResponse(429, httpx.Headers({"Retry-After": "+3"}))) == (3, True)
    assert _retry_delay(ProviderResponse(429, httpx.Headers({"Retry-After": "Wed, 21 Oct 2015"}))) == (
        0,
        False,
    )
