# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go encoding/json emulation checked against Go-encoded fixtures shared with the TypeScript SDK."""

import base64
import hashlib
import json
import math
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest

from dex_connectors.sdk.projectconfig import (
    InvalidRawJSONError,
    encode_go_json_value,
    parse_trigger_payload_json,
)
from dex_connectors.sdk.projectconfig._raw_json import _compact_go_raw_json, _encode_go_string

FIXTURES = Path(__file__).parent / "fixtures"
GO_FIXTURES = (
    "go-json-encoding-cases.json",
    "go-trigger-inbox-after-remove.json",
    "go-trigger-inbox-decoding-cases.json",
    "go-trigger-inbox-four-events.json",
    "go-trigger-inbox-three-events.json",
)


def test_go_fixtures_match_the_typescript_copies() -> None:
    typescript = Path(__file__).resolve().parents[2] / "sdkts" / "test" / "fixtures"
    for name in GO_FIXTURES:
        python_digest = hashlib.sha256((FIXTURES / name).read_bytes()).hexdigest()
        assert python_digest == hashlib.sha256((typescript / name).read_bytes()).hexdigest(), name


def test_encodes_payloads_exactly_as_go_marshals_them() -> None:
    cases = {
        case["name"]: base64.b64decode(case["output"]).decode()
        for case in json.loads((FIXTURES / "go-json-encoding-cases.json").read_bytes())
    }
    values: dict[str, Any] = {
        "sorted map keys": {
            "zeta": 1,
            "alpha": {"b": True, "a": None},
            "é": "accent",
            "Z": "upper",
            "a": ["x", 2.5],
        },
        "floats": [1e21, 1.5e-7, 0.1, -0.0, 100.0, 1e-6, 123456789012345680000.0, 1e20, -2.5e-10, 5e-324],
        "strings": {
            "html": "<b>&</b>",
            "separators": "  ",
            "controls": "\b\f\n\r\t\u0001\u001f\u007f",
            "emoji": "\U0001f600",
            "quote": '"\\/',
        },
        "nested arrays": [[], {}, [{"y": 2, "x": 1}]],
        "faithful floats": [
            1e23,
            1e21,
            1.7976931348623157e308,
            5e-324,
            123456789012345680000.0,
            2e300,
            1e22,
            0.1,
            -1e23,
        ],
    }
    assert set(values) == set(cases)
    for name, value in values.items():
        assert encode_go_json_value(value).decode() == cases[name], name
    # Every float Go writes parses back to the same value, so a Go-written event never sticks.
    reparsed = parse_trigger_payload_json(cases["faithful floats"])
    assert [float(number) for number in reparsed] == values["faithful floats"]


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        (1e20, "100000000000000000000"),
        (1.5e-7, "1.5e-7"),
        (1e-6, "0.000001"),
        (-0.0, "-0"),
        (1e21, "1e+21"),
        (5e-324, "5e-324"),
        (0.1, "0.1"),
        (100.0, "100"),
        (-2.5e-10, "-2.5e-10"),
        (123.456, "123.456"),
    ],
)
def test_formats_floats_as_go_does(value: float, expected: str) -> None:
    assert encode_go_json_value(value).decode() == expected


def test_encodes_scalars_times_and_wire_objects_and_refuses_the_rest() -> None:
    class Wire:
        def to_wire(self) -> dict[str, int]:
            return {"kept": 1}

    when = datetime(2025, 10, 12, 0, 0, 0, 120000, tzinfo=UTC)
    encoded = encode_go_json_value(
        {"big": 9007199254740993, "flag": True, "when": when, "value": Wire(), "t": (1,)}
    )
    assert (
        encoded
        == b'{"big":9007199254740993,"flag":true,"t":[1],"value":{"kept":1},"when":"2025-10-12T00:00:00.12Z"}'
    )
    for refused in (math.nan, math.inf, {1: "x"}, {1, 2}, object(), datetime(2025, 1, 1)):
        with pytest.raises(InvalidRawJSONError):
            encode_go_json_value(refused)
    cycle: dict[str, Any] = {}
    cycle["self"] = cycle
    with pytest.raises(InvalidRawJSONError, match="cycle"):
        encode_go_json_value(cycle)

    class SelfReturning:
        def to_wire(self) -> object:
            return self

    with pytest.raises(InvalidRawJSONError, match="cycle"):
        encode_go_json_value({"value": SelfReturning()})
    assert _encode_go_string("a\ud800b") == '"a�b"'


@dataclass(frozen=True, slots=True)
class _GeneratedSession:
    """Shaped like a generated connector model, with Dex codec field metadata."""

    session_id: str = field(metadata={"dex_json_name": "sessionId"})
    amount_total: int = field(
        default=0, metadata={"dex_json_name": "amountTotal", "dex_json_omit_default": True}
    )
    expires_at: datetime | None = field(
        default=None,
        metadata={"dex_json_name": "expiresAt", "dex_json_omit_default": True, "dex_json_format": "rfc3339"},
    )


@dataclass(frozen=True, slots=True)
class _GeneratedEvent:
    type: str
    session: _GeneratedSession
    ratio: float = 0.5


def test_encodes_dataclasses_through_the_dex_codec() -> None:
    when = datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC)
    encoded = encode_go_json_value(
        _GeneratedEvent("checkout.session.completed", _GeneratedSession("cs_1", 1200, when), 1e-7)
    )
    assert encoded == (
        b'{"ratio":1e-7,"session":{"amountTotal":1200,"expiresAt":"2026-01-02T03:04:05Z",'
        b'"sessionId":"cs_1"},"type":"checkout.session.completed"}'
    )
    assert encode_go_json_value([_GeneratedSession("cs_2")]) == b'[{"sessionId":"cs_2"}]'
    with pytest.raises(InvalidRawJSONError, match="cannot encode a _GeneratedEvent"):
        encode_go_json_value(_GeneratedEvent("x", _GeneratedSession("cs_3", expires_at=datetime(2026, 1, 1))))


def _nested(depth: int) -> list[Any]:
    value: list[Any] = []
    for _ in range(depth - 1):
        value = [value]
    return value


def test_nesting_is_bounded_by_go_depth_without_recursion() -> None:
    for depth in (9999, 10_000):
        encoded = encode_go_json_value(_nested(depth))
        assert len(encoded) == depth * 2
        assert len(_compact_go_raw_json(encoded)) == depth * 2
        assert isinstance(parse_trigger_payload_json(encoded), list)
    with pytest.raises(InvalidRawJSONError, match="nested too deeply"):
        encode_go_json_value(_nested(10_001))
    too_deep = b"[" * 10_001 + b"]" * 10_001
    with pytest.raises(InvalidRawJSONError):
        _compact_go_raw_json(too_deep)
    with pytest.raises(InvalidRawJSONError):
        parse_trigger_payload_json(too_deep)


def test_parses_numbers_keeping_integers_exact_and_refusing_overflow() -> None:
    for refused in ("1e400", "-1e400", "[1.5e400]", '{"a":2e300000}'):
        with pytest.raises(InvalidRawJSONError, match="outside the double range"):
            parse_trigger_payload_json(refused)
    assert parse_trigger_payload_json("9007199254740993") == 9007199254740993
    exact_cases: list[tuple[str, float]] = [
        ("9007199254740993e0", 9007199254740992.0),
        ("0.1", 0.1),
        ("1e-400", 0.0),
        ("4e-325", 0.0),
        ("5e-324", 5e-324),
        ("1e+23", 1e23),
        ("1.7976931348623157e+308", 1.7976931348623157e308),
    ]
    for literal, expected in exact_cases:
        parsed = parse_trigger_payload_json(literal)
        assert isinstance(parsed, float) and parsed == expected, literal
    for negative_zero in ("-1e-400", "-0.0"):
        parsed = parse_trigger_payload_json(negative_zero)
        assert parsed == 0.0 and math.copysign(1.0, parsed) < 0
    with pytest.raises(InvalidRawJSONError):
        parse_trigger_payload_json("[1,]")
    assert parse_trigger_payload_json('{"a":1,"a":2}') == {"a": 2}


def test_decodes_strings_and_keys_with_go_rules() -> None:
    raw = (
        b'{"k\\ud800":"x\\ud800y","t":"a'
        + bytes([0xE2, 0x82])
        + b'z","pair":"\\ud83d\\ude00","low":"\\udc00","s":["\\ud800\\u0041"]}'
    )
    assert parse_trigger_payload_json(raw) == {
        "k�": "x�y",
        "t": "a��z",
        "pair": "\U0001f600",
        "low": "�",
        "s": ["�A"],
    }
    assert parse_trigger_payload_json('"a\ud800"') == "a�"


def test_compacts_raw_json_verbatim_except_whitespace_and_html_escapes() -> None:
    assert (
        _compact_go_raw_json(b'{ "b" : [1.0e3, "<x>\xe2\x80\xa8"] }')
        == b'{"b":[1.0e3,"\\u003cx\\u003e\\u2028"]}'
    )
    for invalid in (b"", b"{} {}", b'{"a":}', b'"\x01"', b"01"):
        with pytest.raises(InvalidRawJSONError):
            _compact_go_raw_json(invalid)
