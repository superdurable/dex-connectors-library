# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Snowflake-specific parsing and authentication cases outside HTTP replay."""

import base64
import json
from datetime import UTC, datetime

import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.hashes import SHA256
from dex_connectors.sdk import ConnectionRef, FailureKind, SecretString, StaticCredentialProvider

from dex_connectors.snowflake import Client, Config, Credentials
from dex_connectors.snowflake.operations import _bind_statement, _rejection


@pytest.mark.parametrize(
    ("statement", "parameters", "count"),
    [
        ("SELECT ?", ("a",), 1),
        ("SELECT '?' AS LITERAL, ? -- ?", ("a",), 1),
        ("SELECT $$?$$, /* ? */ ?", ("a",), 1),
    ],
)
def test_parameter_binding_skips_quoted_and_commented_question_marks(
    statement: str,
    parameters: tuple[str, ...],
    count: int,
) -> None:
    """Only SQL placeholders receive provider bindings."""
    bindings = _bind_statement(statement, parameters)
    assert len(bindings) == count
    assert bindings["1"] == {"type": "TEXT", "value": "a"}


def test_unterminated_statement_and_nonfinite_parameter_fail_locally() -> None:
    """Reject malformed local input before it could reach Snowflake."""
    with pytest.raises(ValueError, match="unterminated"):
        _bind_statement("SELECT 'unterminated", ())
    with pytest.raises(ValueError, match="finite"):
        _bind_statement("SELECT ?", (float("nan"),))


@pytest.mark.parametrize(
    ("status", "document", "kind"),
    [
        (401, {}, FailureKind.AUTHENTICATION),
        (403, {}, FailureKind.AUTHORIZATION),
        (422, {"code": "000709", "sqlState": "02000"}, FailureKind.NOT_FOUND),
        (422, {"code": "000604", "sqlState": "57014"}, FailureKind.AVAILABILITY),
        (422, {"sqlState": "42501"}, FailureKind.AUTHORIZATION),
        (422, {"sqlState": "23505"}, FailureKind.CONFLICT),
        (422, {"sqlState": "42000"}, FailureKind.VALIDATION),
    ],
)
def test_provider_rejection_classification(status: int, document: object, kind: FailureKind) -> None:
    """Match Go's terminal credential and SQLSTATE decisions."""
    assert _rejection("submitStatement", status, document).kind == kind


def test_key_pair_jwt_has_snowflake_claims_and_valid_signature() -> None:
    """Sign the Snowflake issuer, subject, fingerprint, and bounded lifetime."""
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    encoded = key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    ).decode()
    reference = ConnectionRef("snowflake", "warehouse")
    client = Client(
        Config(account_identifier="myorg-analytics"),
        StaticCredentialProvider(
            {reference: Credentials(user="dex_service", private_key=SecretString(encoded))}
        ),
        now=lambda: datetime(2026, 9, 30, 12, 0, tzinfo=UTC),
    )
    token = client._sign_key_pair_jwt(Credentials(user="dex_service", private_key=SecretString(encoded)))
    header, claims, signature = token.split(".")
    payload = json.loads(base64.urlsafe_b64decode(claims + "=="))
    assert payload["sub"] == "MYORG-ANALYTICS.DEX_SERVICE"
    assert payload["iss"].startswith(payload["sub"] + ".SHA256:")
    assert payload["exp"] - payload["iat"] == 59 * 60
    key.public_key().verify(
        base64.urlsafe_b64decode(signature + "=="),
        (header + "." + claims).encode(),
        padding.PKCS1v15(),
        SHA256(),
    )


@pytest.mark.parametrize(
    ("column", "text", "expected"),
    [
        ({"type": "fixed", "precision": 9}, "-42", -42),
        ({"type": "fixed", "precision": 38}, "9007199254740993", "9007199254740993"),
        ({"type": "fixed", "precision": 12, "scale": 2}, "1200.00", "1200.00"),
        ({"type": "real"}, "NaN", "NaN"),
        ({"type": "decfloat"}, "1.23e-40", "1.23e-40"),
        ({"type": "binary"}, "0001ff", "AAH/"),
        ({"type": "date"}, "18262", "2020-01-01"),
        ({"type": "time"}, "82919.500000000", "23:01:59.5"),
        ({"type": "timestamp_ntz"}, "1611871777.123456789", "2021-01-28T22:09:37.123456789"),
        ({"type": "timestamp_ntz"}, "-1.500000000", "1969-12-31T23:59:58.5"),
        ({"type": "timestamp_ltz"}, "1616173619.000000000", "2021-03-19T17:06:59Z"),
        ({"type": "timestamp_tz"}, "1616173619.000000000 1500", "2021-03-19T18:06:59+01:00"),
        ({"type": "variant"}, '{"amount":12345678901234567890}', {"amount": 12345678901234567890}),
    ],
)
def test_jsonv2_value_conversion(column: dict[str, object], text: str, expected: object) -> None:
    """Preserve Go's exact numeric and nanosecond timestamp representation."""
    from dex_connectors.snowflake import Column
    from dex_connectors.snowflake.operations import _decode_value

    precision = column.get("precision", 0)
    scale = column.get("scale", 0)
    assert isinstance(precision, int) and isinstance(scale, int)
    described = Column(name="VALUE", type=str(column["type"]), precision=precision, scale=scale)
    assert _decode_value(described, text) == expected
