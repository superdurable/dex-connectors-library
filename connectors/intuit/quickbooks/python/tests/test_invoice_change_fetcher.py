# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded invoiceChanged scenarios and compare requests, cursors, and events byte for byte."""

import base64
import hashlib
import json
from datetime import timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import encode_trigger_event

from dex_connectors.intuit_quickbooks import (
    Client,
    Config,
    Credentials,
    InvoiceChangeFetcher,
    decode_invoice_changed_event_payload,
)
from dex_connectors.intuit_quickbooks.invoice_change_cursor import (
    MAXIMUM_SEEN_INVOICE_VERSIONS,
    SECOND,
    InvoiceWindow,
    decode_invoice_change_cursor,
    encode_invoice_change_cursor,
)
from examples.invoice_changed.invoice_fake import QuickBooksInvoiceFake

REALM_ID = "9341453050298464"
ACCESS_TOKEN = "trigger-access-token"
CONNECTION = ConnectionRef("quickbooks", "quickbooks-trigger-test")
SCENARIOS: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures/go-invoice-change-cases.json").read_text()
)["scenarios"]


def id_token(realm_id: str) -> str:
    """Return an unsigned ID token naming realm_id, as the Go recorder's test client uses."""
    claims = {"iss": "https://oauth.platform.intuit.com/op/v1", "aud": ["client"], "realmid": realm_id}
    encode = lambda value: base64.urlsafe_b64encode(json.dumps(value).encode()).decode().rstrip("=")  # noqa: E731
    return encode({"alg": "RS256"}) + "." + encode(claims) + ".c2ln"


def new_fake_client(
    fake: QuickBooksInvoiceFake, requests: list[str], environment: str = "production"
) -> Client:
    """Build a client whose provider is the in-process invoice fake, recording each request."""

    def handle(request: httpx.Request) -> httpx.Response:
        requests.append(request.url.raw_path.decode())
        return fake.handle(request)

    credentials = Credentials(
        "client",
        SecretString("secret"),
        SecretString(ACCESS_TOKEN),
        SecretString(""),
        SecretString(id_token(REALM_ID)),
    )
    return Client(
        Config(realm_id="", environment=environment),
        StaticCredentialProvider({CONNECTION: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(handle)),
        local_provider_url="http://127.0.0.1:1",
    )


def apply_provider_step(fake: QuickBooksInvoiceFake, step: dict[str, Any]) -> bool:
    """Apply one recorded provider change; return False for a seed or fetch step."""
    operation = step["op"]
    if operation == "create":
        for _ in range(step.get("repeat", 1)):
            fake.create(step["docNumber"], step["customerId"], step["amount"])
    elif operation == "createBackdated":
        age = timedelta(seconds=step["ageSeconds"])
        fake.create_backdated(step["docNumber"], step["customerId"], step["amount"], age)
    elif operation == "pay":
        fake.pay(step["id"])
    elif operation == "holdClock":
        fake.hold_clock()
    elif operation == "releaseClock":
        fake.release_clock()
    elif operation == "throttle":
        fake.throttle(step["count"], step["retryAfter"])
    elif operation == "rejectWatermarks":
        fake.reject_watermark_queries(step["rejects"])
    else:
        return False
    return True


def assert_cursor(step: dict[str, Any], token: str) -> None:
    """Compare a cursor token with the Go recording, or with its digest when it is long."""
    if "cursorOut" in step:
        assert token == step["cursorOut"]
    else:
        assert len(token) == step["cursorOutLength"]
        assert hashlib.sha256(token.encode()).hexdigest() == step["cursorOutSha256"]
    assert len(token.encode()) < 64 << 10


def assert_changes(step: dict[str, Any], changes: list[Any]) -> None:
    """Compare event IDs, kinds, times, and stored payloads with the Go recording."""
    if "changeCount" in step:
        assert len(changes) == step["changeCount"]
        ids = "\n".join(change.id for change in changes)
        kinds = "\n".join(change.payload.change_kind for change in changes)
        assert hashlib.sha256(ids.encode()).hexdigest() == step["changeIdsSha256"]
        assert hashlib.sha256(kinds.encode()).hexdigest() == step["changeKindsSha256"]
        return
    assert [change.id for change in changes] == [recorded["id"] for recorded in step["changes"]]
    for change, recorded in zip(changes, step["changes"], strict=True):
        stored = json.loads(encode_trigger_event(change))
        assert stored["occurredAt"] == recorded["occurredAt"]
        assert stored["payload"] == recorded["payload"]
        payload = json.dumps(recorded["payload"], separators=(",", ":"))
        assert decode_invoice_changed_event_payload(payload, payload.encode()) == change.payload


def assert_error(step: dict[str, Any], error: BaseException | None) -> None:
    """Compare the raised error's class with the Go recording."""
    expected = step.get("error")
    if expected == "cursorInvalid":
        assert is_cursor_invalid(error), error
    elif expected == "rateLimited":
        assert isinstance(error, RateLimitedError)
        assert error.retry_after == timedelta(seconds=step["retryAfterSeconds"])
    elif expected == "failed":
        assert error is not None and not is_cursor_invalid(error) and not isinstance(error, RateLimitedError)
    else:
        assert error is None, error
    assert error is None or ACCESS_TOKEN not in str(error)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_fetcher_matches_go_recording(scenario: dict[str, Any]) -> None:
    """Every request, cursor token, event ID, and stored payload equals the Go connector's."""
    fake = QuickBooksInvoiceFake(REALM_ID)
    requests: list[str] = []
    client = new_fake_client(fake, requests)
    fetcher = InvoiceChangeFetcher(client, CONNECTION)
    outputs: dict[int, str] = {}
    try:
        for index, step in enumerate(scenario["steps"]):
            if apply_provider_step(fake, step):
                continue
            error: BaseException | None = None
            token, changes, has_more_pages = "", [], False
            try:
                if step["op"] == "seed":
                    token = (await fetcher.seed_cursor(TriggerContext())).token
                else:
                    cursor_in = (
                        outputs[step["cursorFromStep"]] if "cursorFromStep" in step else step["cursorIn"]
                    )
                    page = await fetcher.fetch_changes(TriggerContext(), Cursor(cursor_in))
                    token, changes, has_more_pages = (
                        page.next_cursor.token,
                        list(page.changes),
                        page.has_more_pages,
                    )
            except Exception as raised:
                error = raised
            assert requests == step["requests"], f"step {index}"
            requests.clear()
            assert_error(step, error)
            if error is None:
                assert_cursor(step, token)
                outputs[index] = token
                if step["op"] == "fetch":
                    assert has_more_pages == step["hasMorePages"]
                    assert_changes(step, changes)
    finally:
        await client.aclose()


def test_window_keeps_a_bounded_cursor_when_one_second_overflows() -> None:
    """A single second with more versions than the bound moves the window past it, as in Go."""
    window = InvoiceWindow(since=1000 * SECOND, from_=970 * SECOND)
    page = [(f"{index}:0", 1001 * SECOND) for index in range(MAXIMUM_SEEN_INVOICE_VERSIONS + 1)]
    following = window.advance(page, False)
    assert following.since == following.from_ == 1002 * SECOND and not following.seen
    token = encode_invoice_change_cursor(REALM_ID, "production", following).token
    assert decode_invoice_change_cursor(token)[1] == InvoiceWindow(1002 * SECOND, 1002 * SECOND)


def test_cursor_escapes_strings_as_go_does() -> None:
    """Go's json.Marshal escapes <, >, and & in the stored version keys."""
    window = InvoiceWindow(since=10 * SECOND, from_=0, seen={"1:<&>": 10 * SECOND})
    token = encode_invoice_change_cursor(REALM_ID, "production", window).token
    assert '"seen":["10/1:\\u003c\\u0026\\u003e"]' in token
    assert decode_invoice_change_cursor(token)[1].seen == {"1:<&>": 10 * SECOND}


# Recorded from Go's time.Parse(time.RFC3339): its layout fallback takes a one-digit hour and zone offsets
# of up to 24 hours or 60 minutes, and year 0.
GO_WATERMARKS = {
    "2026-01-01T1:00:00Z": "2026-01-01T01:00:00Z",
    "2026-01-01T01:00:00+24:00": "2025-12-31T01:00:00Z",
    "2026-01-01T01:00:00+23:60": "2025-12-31T01:00:00Z",
    "2026-01-01T01:00:00-24:00": "2026-01-02T01:00:00Z",
    "2026-01-01T01:00:00,5Z": "2026-01-01T01:00:00Z",
    "0000-02-29T12:00:00Z": "0000-02-29T12:00:00Z",
}


def watermark_token(watermark: str) -> str:
    """Encode a cursor whose bounds are both watermark."""
    return json.dumps(
        {"realmId": REALM_ID, "environment": "production", "since": watermark, "from": watermark}
    )


@pytest.mark.parametrize(("watermark", "encoded"), GO_WATERMARKS.items())
def test_cursor_decodes_watermarks_as_go_parses_them(watermark: str, encoded: str) -> None:
    """A watermark Go accepts decodes and re-encodes in Go's form."""
    position, window = decode_invoice_change_cursor(watermark_token(watermark))
    reencoded = json.loads(
        encode_invoice_change_cursor(position.realm_id, position.environment, window).token
    )
    assert (reencoded["since"], reencoded["from"]) == (encoded, encoded)


@pytest.mark.parametrize(
    "watermark",
    ["2026-01-01T01:00:00z", "2026-01-01T24:00:00Z", "2026-02-29T00:00:00Z", "2026-01-01T01:00:00+25:00"],
)
def test_cursor_rejects_watermarks_go_rejects(watermark: str) -> None:
    """A watermark Go's time.Parse rejects invalidates the cursor."""
    with pytest.raises(ValueError, match="invalid watermark"):
        decode_invoice_change_cursor(watermark_token(watermark))


def test_cursor_matches_field_names_and_strings_as_go_does() -> None:
    """Keys fold as Go's simple folding does, and an escaped lone surrogate decodes as U+FFFD."""
    token = (
        '{"realmId":"' + REALM_ID + '","environment":"production","since":"1970-01-01T00:00:10Z",'
        '"from":"1970-01-01T00:00:00Z","\u017fEEN":["10/1:\\ud800","10/1:\\udc00"],"o\ufb00set":7}'
    )
    position, window = decode_invoice_change_cursor(token)
    assert window.seen == {"1:\ufffd": 10 * SECOND} and window.offset == 0
    assert position.seen == ["10/1:\ufffd", "10/1:\ufffd"]
