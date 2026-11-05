# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep Go's nanosecond timestamp precision across Python model boundaries."""

from datetime import UTC, datetime

from dex import CodecRegistry

from dex_connectors.google_forms.conversion import _time
from dex_connectors.google_forms.models import FormResponse, ListResponsesInput
from dex_connectors.google_forms.operations import _utc_timestamp


def test_nine_digit_timestamp_round_trips_through_dex_codec() -> None:
    source = "2026-09-30T02:15:00.123456789-07:00"
    input_value = ListResponsesInput(form_id="form", submitted_after=source)
    input_codec = CodecRegistry().resolve(ListResponsesInput)
    encoded = input_codec.encode(input_value)
    assert source in encoded.data
    assert input_codec.decode(encoded).submitted_after == source

    response = FormResponse(created_at=source, last_submitted_at=source)
    response_codec = CodecRegistry().resolve(FormResponse)
    assert response_codec.decode(response_codec.encode(response)).created_at == source


def test_provider_and_filter_timestamps_keep_nanoseconds_in_utc() -> None:
    source = "2026-09-30T02:15:00.123456789-07:00"
    expected = "2026-09-30T09:15:00.123456789Z"
    assert _utc_timestamp(source) == expected
    assert _time(source) == expected
    assert ListResponsesInput(submitted_after=datetime(2026, 9, 30, 9, 15, tzinfo=UTC)).submitted_after == (
        "2026-09-30T09:15:00Z"
    )
