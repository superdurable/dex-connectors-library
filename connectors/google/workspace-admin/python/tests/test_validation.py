# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Directory wire validation beyond the common conformance paths."""

import pytest

from dex_connectors.google_workspace_admin.client import path_escape, query_string
from dex_connectors.google_workspace_admin.operations import _bare_email, _user


@pytest.mark.parametrize(
    ("address", "accepted"),
    [
        ("ada@example.com", True),
        ("é@exämple.com", True),
        ("ada@localhost", True),
        ("ada..test@example.com", False),
        ("ada@b..com", False),
        ("ada@[127.0.0.1]", False),
        ("Ada <ada@example.com>", False),
    ],
)
def test_bare_addresses_match_directory_rules(address: str, accepted: bool) -> None:
    """Allow international atoms while excluding malformed or decorated addresses."""
    assert _bare_email(address) is accepted


def test_path_and_query_use_go_escape_sets() -> None:
    """Preserve Go's distinct path and query escaping for reserved characters."""
    assert path_escape("a!b/c~d$&+=:@") == "a%21b%2Fc~d$&+=:@"
    assert query_string({"z": "a b~*", "a": "x/y"}) == "a=x%2Fy&z=a+b%7E%2A"


@pytest.mark.parametrize("field", ["id", "primaryEmail", "suspended", "aliases"])
def test_wrong_typed_directory_user_fields_fail(field: str) -> None:
    """Reject values Go's JSON decoder would reject before constructing a User."""
    resource: dict[str, object] = {"id": "1001", "primaryEmail": "ada@example.com"}
    resource[field] = 9
    with pytest.raises(ValueError):
        _user(resource)
