# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List-Unsubscribe target validation, mirroring the Go connector's TestValidateListUnsubscribe table."""

import pytest

from dex_connectors.google_gmail.provider import validate_list_unsubscribe

VALID = [
    ([], False),
    (["https://example.com/unsubscribe?token=abc", "mailto:news@example.com?subject=unsubscribe"], False),
    (["https://example.com/u/1"], True),
    (["HTTPS://Example.com:8443/u"], False),
    (["mailto:news%40example.com"], False),
]

INVALID = [
    (["mailto:news@example.com"], True),
    (["http://example.com/u"], False),
    (["javascript:alert(1)"], False),
    (["https://example.com/u\r\nBcc: victim@example.com"], False),
    (["https://example.com/u\nX: y"], False),
    (["https://example.com/u>, <https://evil.example"], False),
    (["https://example.com/a,b"], False),
    (["https://example.com/a b"], False),
    (["https://example.com/é"], False),
    (["https:///path"], False),
    (["https:example.com/u"], False),
    (["https://example.com:80a/u"], False),
    (["https://exa_mple.com/u"], False),
    (["https://user:pass@example.com/u"], False),
    (["https://example.com\\evil.com/u"], False),
    (["mailto:"], False),
    (["mailto:not-an-address"], False),
    (["mailto:%ZZ@example.com"], False),
    (["mailto:a@example.com%0D%0ABcc:b@example.com"], False),
    ([""], False),
    (["https://example.com/u", "https://example.com/u"], False),
    (["https://example.com/1", "https://example.com/2", "mailto:a@example.com"], False),
    (["https://example.com/" + "a" * 512], False),
]


@pytest.mark.parametrize(("targets", "is_one_click"), VALID)
def test_accepts_valid_targets(targets: list[str], is_one_click: bool) -> None:
    validate_list_unsubscribe(targets, is_one_click)


@pytest.mark.parametrize(("targets", "is_one_click"), INVALID)
def test_rejects_invalid_targets(targets: list[str], is_one_click: bool) -> None:
    with pytest.raises(ValueError):
        validate_list_unsubscribe(targets, is_one_click)
