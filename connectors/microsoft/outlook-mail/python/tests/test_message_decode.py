# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reject lossy or non-Go Graph response numbers."""

import pytest

from dex_connectors.microsoft_outlook_mail.message import decode_attachment_list, json_object


@pytest.mark.parametrize("size", ["5.0", "1e3", "NaN", "Infinity", "-Infinity"])
def test_attachment_size_requires_go_integer(size: str) -> None:
    """Go's int64 decoder rejects floats, exponents, and nonfinite numbers."""
    response = ('{"value":[{"id":"AAMkABC","size":' + size + "}]}").encode()
    with pytest.raises(ValueError):
        decode_attachment_list(response)


def test_large_integer_is_not_rounded() -> None:
    """Preserve the exact int64 value when decoding an attachment."""
    attachments, truncated = decode_attachment_list(b'{"value":[{"id":"AAMkABC","size":9007199254740993}]}')
    assert attachments[0].size_bytes == 9007199254740993
    assert not truncated


def test_json_object_rejects_nan_outside_a_known_field() -> None:
    """Reject nonfinite JSON anywhere before building a connector result."""
    with pytest.raises(ValueError):
        json_object(b'{"value":[],"unexpected":NaN}')
