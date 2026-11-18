# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Snowflake-specific parsing and authentication cases outside HTTP replay."""

import base64
import json
from dataclasses import replace
from datetime import UTC, datetime
from types import SimpleNamespace
from typing import cast

import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.hashes import SHA256
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.snowflake import Client, Config, Credentials
from dex_connectors.snowflake._generated.connection import CredentialMaterialError
from dex_connectors.snowflake.operations import _bind_statement, _rejection


def _sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError that the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("Snowflake credential user is required")
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("unreachable")


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
    ("value", "expected"),
    [(150.0, "150"), (100000.0, "100000"), (1000000.0, "1e+06"),
     (0.0001, "0.0001"), (0.00001, "1e-05"), (-0.0, "-0")],
)
def test_real_binding_matches_go_shortest_format(value: float, expected: str) -> None:
    assert _bind_statement("SELECT ?", (value,))["1"]["value"] == expected


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
        ({"type": "timestamp_tz"}, "1616173619.000000000 1440", "2021-03-19T17:06:59Z"),
        ({"type": "timestamp_tz"}, "1616173619.000000000 1380", "2021-03-19T16:06:59-01:00"),
        ({"type": "variant"}, '{"amount":12345678901234567890}', {"amount": 12345678901234567890}),
        ({"type": "variant"}, '{"amount":1.0}', {"amount": 1.0}),
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


@pytest.mark.parametrize(
    "text",
    ['{"price":12345678901234567890.123}', '{"fraction":0.1234567890123456789}',
     '{"price":1E5}', '{"price":-0}', '{"price":1,"price":2}'],
)
def test_variant_rejects_json_that_python_codec_cannot_preserve(text: str) -> None:
    from dex_connectors.snowflake import Column
    from dex_connectors.snowflake.operations import _decode_value

    with pytest.raises(ValueError, match="precision|text|duplicate"):
        _decode_value(Column(name="VALUE", type="variant"), text)


def test_variant_decode_error_names_the_column() -> None:
    """A non-lossy VARIANT rejection identifies the affected result field."""
    from dex_connectors.snowflake import GetStatementResultInput
    from dex_connectors.snowflake.operations import _decode_result

    client = Client(Config(account_identifier="myorg-analytics"), StaticCredentialProvider({}))
    document = {
        "resultSetMetaData": {"format": "jsonv2", "rowType": [{"name": "VARIANT", "type": "variant"}]},
        "data": [['{"decimal":1.50}']],
    }
    input = GetStatementResultInput(statement_handle="00000000-0000-4000-8000-000000000003")
    with pytest.raises(ValueError, match='column "VARIANT"'):
        _decode_result(document, input, client)


def test_result_byte_limit_counts_go_json_string_escapes() -> None:
    from dex_connectors.snowflake import Column, GetStatementResultInput
    from dex_connectors.snowflake.operations import _decode_result

    value = "<>&\u2028\u2029"
    encoded_row = '{"TEXT":"\\u003c\\u003e\\u0026\\u2028\\u2029"}'
    row_limit = 2 + len(encoded_row.encode()) + 1
    input = GetStatementResultInput(
        statement_handle="00000000-0000-4000-8000-000000000003",
        partition=1,
        columns=(Column(name="TEXT", type="text"),),
    )
    complete = _decode_result([[value]], input, cast(Client, SimpleNamespace(config=SimpleNamespace(
        max_rows=1, max_response_bytes=row_limit,
    ))))
    truncated = _decode_result([[value]], input, cast(Client, SimpleNamespace(config=SimpleNamespace(
        max_rows=1, max_response_bytes=row_limit - 1,
    ))))
    assert complete.rows == ({"TEXT": value},)
    assert not complete.truncated
    assert truncated.rows == ()
    assert truncated.truncated


@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialMaterialError("Snowflake credential user is required"), True),
        (_sdk_wrapped_material_error(), True),
        (CredentialBrokerUnavailableError(), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("unlisted"), False),
    ],
)
async def test_credential_failures_select_defect_only_when_terminal(
    error: Exception, is_terminal: bool
) -> None:
    """Only a terminal credential failure selects defect; every other one Retries without sending."""
    from dex_connectors.snowflake import CancelStatementInput, GetStatementResultInput, SubmitStatementInput

    class _FailingCredentials:
        async def resolve(self, call: Call) -> Credentials:
            raise error

    client = Client(Config(account_identifier="myorg-analytics"), _FailingCredentials())
    handle = "00000000-0000-4000-8000-000000000003"
    call = Call(
        CallID(handle),
        ConnectionRef("snowflake", "warehouse"),
        OperationRef("snowflake", "x"),
        IdempotencyKey(handle),
    )
    submitted = await client.submit_statement().invoke(call, SubmitStatementInput(statement="SELECT 1"))
    canceled = await client.cancel_statement().invoke(call, CancelStatementInput(statement_handle=handle))
    read = await client.get_statement_result().invoke(call, GetStatementResultInput(statement_handle=handle))
    for attempt in (submitted, canceled, read):
        if is_terminal:
            assert attempt.branch == "defect"
        else:
            assert attempt.kind == "retry"
            assert attempt.failure is not None and attempt.failure.kind == FailureKind.AVAILABILITY


def _answering_client(status: int, body: object) -> Client:
    reference = ConnectionRef("snowflake", "warehouse")
    credentials = Credentials(
        auth_method_id="programmatic-access-token",
        programmatic_access_token=SecretString("pat-secret-value-0123456789"),
    )
    return Client(
        Config(account_identifier="myorg-analytics"),
        StaticCredentialProvider({reference: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(status, json=body))),
        local_provider_url="http://127.0.0.1",
    )


def _call(attempt: int) -> Call:
    handle = "00000000-0000-4000-8000-000000000003"
    call = Call(
        CallID(handle),
        ConnectionRef("snowflake", "warehouse"),
        OperationRef("snowflake", "x"),
        IdempotencyKey(handle),
    )
    return replace(call, context=SimpleNamespace(attempt=attempt))  # type: ignore[arg-type]


@pytest.mark.parametrize("status", [405, 408, 409, 307])
async def test_writes_retry_unlisted_statuses(status: int) -> None:
    from dex_connectors.snowflake import CancelStatementInput, SubmitStatementInput

    client = _answering_client(status, {})
    submitted = await client.submit_statement().invoke(_call(1), SubmitStatementInput(statement="SELECT 1"))
    canceled = await client.cancel_statement().invoke(
        _call(1), CancelStatementInput(statement_handle="00000000-0000-4000-8000-000000000003")
    )
    assert submitted.kind == "retry" and canceled.kind == "retry"


async def test_a_repeated_cancel_refused_for_a_known_handle_selects_canceled() -> None:
    from dex_connectors.snowflake import CancelStatementInput

    handle = "00000000-0000-4000-8000-000000000003"
    cancel = CancelStatementInput(statement_handle=handle)
    refused = _answering_client(422, {"code": "000605", "sqlState": "01000", "statementHandle": handle})
    repeated = await refused.cancel_statement().invoke(_call(2), cancel)
    assert repeated.branch == "canceled"
    first = await refused.cancel_statement().invoke(_call(1), cancel)
    assert first.branch == "providerRejected"
    unknown = _answering_client(422, {"code": "000709", "sqlState": "02000"})
    missing = await unknown.cancel_statement().invoke(_call(2), cancel)
    assert missing.branch == "providerRejected"
