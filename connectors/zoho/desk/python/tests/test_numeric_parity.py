# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Go's strict JSON number and strconv.Atoi behavior."""

import pytest

from dex_connectors.zoho_desk.operations import _count, _id


def test_go_count_string_forms() -> None:
    assert _count("+1") == 1
    assert _count("-0") == 0
    assert _count("9223372036854775807") == 9223372036854775807


@pytest.mark.parametrize(
    "value", ["5.0", "1e3", "-1", "9223372036854775808", 5.0, float("nan"), float("inf")]
)
def test_go_count_rejects_noninteger_or_out_of_range(value: object) -> None:
    with pytest.raises(ValueError):
        _count(value)


def test_go_id_keeps_exact_20_digit_number() -> None:
    assert _id(9007199254740993) == "9007199254740993"
