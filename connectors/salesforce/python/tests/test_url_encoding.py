# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go net/url-derived query and path encoding cases."""

import pytest

from dex_connectors.salesforce.client import go_path_escape, go_query
from dex_connectors.salesforce.models import SOQLValue, UpsertRecordByExternalIDInput
from dex_connectors.salesforce.operations import (
    UpsertRecordByExternalIDOperation,
    _decode_record,
    _render_binding,
)


@pytest.mark.parametrize(
    ("values", "expected"),
    [
        ({"a ~*!'()+": "A ~*!'()+"}, "a+~%2A%21%27%28%29%2B=A+~%2A%21%27%28%29%2B"),
        ({"z": "two", "a": "one"}, "a=one&z=two"),
        (
            {"q": "SELECT Id FROM Contact WHERE Name = 'O\\'Brien'"},
            "q=SELECT+Id+FROM+Contact+WHERE+Name+%3D+%27O%5C%27Brien%27",
        ),
    ],
)
def test_go_query(values: dict[str, str], expected: str) -> None:
    """Keep Go's sorted form encoding, literal tilde, and escaped punctuation."""
    assert go_query(values) == expected


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("a ~*!'()+/@:$&=,?", "a%20~%2A%21%27%28%29+%2F@:$&=%2C%3F"),
        ("ERP/88213", "ERP%2F88213"),
        ("é", "%C3%A9"),
    ],
)
def test_go_path_escape(value: str, expected: str) -> None:
    """Keep Go url.PathEscape's path segment rules."""
    assert go_path_escape(value) == expected


def test_falsy_non_string_id_is_invalid_response() -> None:
    with pytest.raises(ValueError, match="record Id"):
        _decode_record({"attributes": {"type": "Contact"}, "Id": 0})


def test_year_one_datetime_can_cross_into_year_zero() -> None:
    assert _render_binding(SOQLValue("dateTime", "0001-01-01T00:00:00+01:00")) == (
        "0000-12-31T23:00:00Z"
    )


@pytest.mark.asyncio
async def test_external_id_rejects_c1_control_without_request() -> None:
    operation = UpsertRecordByExternalIDOperation(object())  # type: ignore[arg-type]
    result = await operation.invoke(
        None,  # type: ignore[arg-type]
        UpsertRecordByExternalIDInput("Contact", "ERP_Id__c", "bad\u0085value", {"LastName": "Doe"}),
    )
    assert result.branch == "defect"
