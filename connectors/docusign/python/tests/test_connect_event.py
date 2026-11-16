# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port the Go and TypeScript Connect decoding and signature tests."""

from datetime import UTC, datetime

import pytest

from dex_connectors.docusign import (
    ConnectMessageMalformedError,
    ConnectSignatureInvalidError,
    EnvelopeCustomField,
    EnvelopeEvent,
    EnvelopeEventReceivedTriggerConfiguration,
    decode_connect_message,
    envelope_event_custom_field_value,
    validate_envelope_event_received_trigger_configuration,
    verify_connect_signature,
)
from tests.connect_fixture import TEST_ACCOUNT_ID, TEST_ENVELOPE_ID, connect_body, connect_signature

RECEIVED_AT = datetime(2026, 10, 5, 8, 0, 0, 500000, tzinfo=UTC)


def decode(body: str):  # type: ignore[no-untyped-def]
    return decode_connect_message(body.encode(), RECEIVED_AT)


def minimal(fields: str) -> str:
    return (
        '{"event":"envelope-completed",'
        + fields
        + '"data":{"accountId":"'
        + TEST_ACCOUNT_ID
        + '","envelopeId":"'
        + TEST_ENVELOPE_ID
        + '"}}'
    )


def test_decodes_a_voided_outcome_as_go_does() -> None:
    event = decode(connect_body("envelope-voided", TEST_ENVELOPE_ID.upper(), "opp-123"))
    assert event is not None
    assert event.id == TEST_ENVELOPE_ID + ":envelope-voided"
    assert event.occurred_at == datetime(2026, 10, 4, 13, 0, 0, 123000, tzinfo=UTC)
    assert event.payload == EnvelopeEvent(
        event="envelope-voided",
        envelope_id=TEST_ENVELOPE_ID,
        account_id=TEST_ACCOUNT_ID,
        generated_at=event.occurred_at,
        status="voided",
        voided_reason="Signer left the company",
        custom_fields=(EnvelopeCustomField("dexSigningRequestId", "opp-123"),),
    )
    assert envelope_event_custom_field_value(event.payload, "dexSigningRequestId") == "opp-123"
    assert envelope_event_custom_field_value(event.payload, "missing") is None


def test_matches_keys_case_insensitively_as_go_encoding_json_does() -> None:
    event = decode(
        '{"Event":"envelope-declined","DATA":{"AccountId":"'
        + TEST_ACCOUNT_ID
        + '","envelopeid":"'
        + TEST_ENVELOPE_ID
        + '","envelopeSummary":{"Status":"Declined"}}}'
    )
    assert event is not None
    assert event.id == TEST_ENVELOPE_ID + ":envelope-declined"
    assert event.payload.status == "declined"
    later = decode(minimal('"EVENT":"envelope-voided",'))
    assert later is not None and later.payload.event == "envelope-voided", "the last matching key wins"


def test_falls_back_to_the_receipt_time_for_a_blank_or_unparsable_generation_time() -> None:
    for fields in ('"generatedDateTime":"yesterday",', '"generatedDateTime":"",', ""):
        event = decode(minimal(fields))
        assert event is not None and event.payload.generated_at == RECEIVED_AT
    offset = decode(minimal('"generatedDateTime":"2026-10-04T13:00:00+02:00",'))
    assert offset is not None and offset.occurred_at == datetime(2026, 10, 4, 11, tzinfo=UTC)


@pytest.mark.parametrize(
    "body",
    [
        "",
        "null",
        "[]",
        "{}",
        "<DocuSignEnvelopeInformation/>",
        '{"event":"envelope-completed"}',
        '{"event":"envelope-completed","data":"x"}',
        '{"event":"envelope-completed","data":null}',
        '{"event":7,"data":{}}',
        '{"event":"envelope-completed","generatedDateTime":NaN,"data":{}}',
        minimal("").replace(TEST_ENVELOPE_ID, "not-a-guid"),
        minimal("").replace('"' + TEST_ACCOUNT_ID + '"', "7"),
        connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp").replace(
            '"textCustomFields":[', '"textCustomFields":[7,'
        ),
        connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp").replace(
            '"status":"completed"', '"status":true'
        ),
        connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp")
        .replace('"customFields":{', '"customFields":[{')
        .replace("}]}}}}", "}]}]}}}"),
    ],
)
def test_refuses_malformed_messages(body: str) -> None:
    with pytest.raises(ConnectMessageMalformedError):
        decode(body)


def test_acknowledges_other_events_unrecorded() -> None:
    assert decode(minimal("").replace("envelope-completed", "envelope-sent")) is None
    assert decode('{"event":"recipient-completed","data":{"envelopeId":"not-a-guid"}}') is None, (
        "identities are checked only for envelope outcomes"
    )
    with pytest.raises(ConnectMessageMalformedError):
        decode('{"event":"recipient-completed","data":{"envelopeId":7}}')


def test_accepts_an_event_without_envelope_data() -> None:
    for summary in ('""', "null", "[]", "7"):
        event = decode(minimal("").replace("}}", ',"envelopeSummary":' + summary + "}}"))
        assert event is not None
        assert event.payload.status is None and event.payload.custom_fields == ()


def test_skips_unnamed_custom_fields_and_keeps_docusign_order() -> None:
    body = connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp-1").replace(
        '"textCustomFields":[',
        '"textCustomFields":[null,{"name":"","value":"x"},{"name":"first","value":null},',
    )
    event = decode(body)
    assert event is not None
    assert event.payload.custom_fields == (
        EnvelopeCustomField("first", ""),
        EnvelopeCustomField("dexSigningRequestId", "opp-1"),
    )


def test_signature_accepts_any_numbered_header_and_refuses_others() -> None:
    body = b'{"event":"envelope-completed"}'
    signature = connect_signature("key", body.decode())
    verify_connect_signature(body, [("x-docusign-signature-01", signature)], "key")
    verify_connect_signature(
        body, [("x-docusign-signature-1", "bogus"), ("x-docusign-signature-2", " " + signature + " ")], "key"
    )
    for headers in (
        [],
        [("x-docusign-signature", signature)],
        [("x-docusign-signature-+1", signature)],
        [("x-docusign-signature-65536", signature)],
        [("x-docusign-signature-1", signature.rstrip("="))],
        [("x-docusign-signature-1", "bogus, " + signature)],
        [("x-docusign-signature-1", connect_signature("other", body.decode()))],
    ):
        with pytest.raises(ConnectSignatureInvalidError):
            verify_connect_signature(body, headers, "key")


def test_configuration_requires_distinct_supported_events() -> None:
    validate_envelope_event_received_trigger_configuration(EnvelopeEventReceivedTriggerConfiguration())
    validate_envelope_event_received_trigger_configuration(
        EnvelopeEventReceivedTriggerConfiguration(("envelope-voided", "envelope-completed"))
    )
    for events in (("envelope-sent",), ("envelope-voided", "envelope-voided")):
        with pytest.raises(ValueError, match="must be distinct values from envelope-completed"):
            validate_envelope_event_received_trigger_configuration(
                EnvelopeEventReceivedTriggerConfiguration(events)
            )
