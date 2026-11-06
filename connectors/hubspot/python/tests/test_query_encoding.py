# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check HubSpot query encoding against Go url.Values.Encode."""

import pytest

from dex_connectors.hubspot.operations import _encode_query


@pytest.mark.parametrize(
    "values, expected",
    [
        ({"z": "a b~*+!'()", "a": "slash/", "x": "é"},
         "a=slash%2F&x=%C3%A9&z=a+b~%2A%2B%21%27%28%29"),
        ({"properties": "email,first name~*", "archived": "false"},
         "archived=false&properties=email%2Cfirst+name~%2A"),
    ],
)
def test_query_encoding_matches_go(values: dict[str, str], expected: str) -> None:
    """Keep sorted keys and Go QueryEscape's unreserved and reserved bytes."""
    assert _encode_query(values) == expected
