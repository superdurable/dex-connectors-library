# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Go's strict JSON number and strconv.Atoi behavior."""

import pytest

from dex_connectors.zoho_desk.operations import _count, _flag, _id, _truncate


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


@pytest.mark.parametrize("value", [1, 1.0, 0, 0.0])
def test_go_boolean_rejects_json_numbers(value: object) -> None:
    with pytest.raises(ValueError, match="flag is not a boolean"):
        _flag(value)


def test_go_utf8_replaces_lone_surrogate_before_truncation() -> None:
    assert _truncate("before\ud800after") == ("before\ufffdafter", False)
