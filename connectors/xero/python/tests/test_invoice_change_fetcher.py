# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded invoiceChanged scenarios and compare requests, cursors, and events byte for byte."""

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

from dex_connectors.xero import (
    Client,
    Config,
    Credentials,
    InvoiceChangedTriggerConfiguration,
    InvoiceChangeFetcher,
    decode_invoice_changed_event_payload,
    decode_invoice_changed_trigger_configuration,
)
from dex_connectors.xero.invoice_change_cursor import (
    MAXIMUM_SEEN_INVOICE_CHANGES,
    InvoiceChangeCursor,
    decode_invoice_change_cursor,
    encode_invoice_change_cursor,
    prune_seen_invoice_changes,
)
from dex_connectors.xero.invoice_change_fetcher import seen_invoice_change_key, unix_milliseconds
from dex_connectors.xero.models import Invoice
from examples.invoice_changed.invoice_fake import BASE_TIME_MILLISECONDS, XeroInvoiceFake

# Inert test credentials, matching the Go recorder's test client.
ACCESS_TOKEN = "xero-trigger-" + "token"
OAUTH_ACCESS_TOKEN = "xero-oauth-" + "token"
TENANT_ID = "70784a63-d24b-46a9-a4db-0e70a274b056"
CONNECTION = ConnectionRef("xero", "xero-invoice-changes")
SCENARIOS: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures/go-invoice-change-cases.json").read_text()
)["scenarios"]


def new_fake_client(fake: XeroInvoiceFake, requests: list[str], oauth: bool) -> Client:
    """Build a client whose provider is the in-process fake, recording each request as the Go recorder."""

    def handle(request: httpx.Request) -> httpx.Response:
        line = request.method + " " + request.url.path
        if request.url.query:
            line += "?" + request.url.query.decode()
        line += " ims=" + request.headers.get("If-Modified-Since", "")
        line += " tenant=" + request.headers.get("Xero-Tenant-Id", "")
        requests.append(line)
        return fake.handle(request)

    if oauth:
        config = Config(organisation=TENANT_ID, poll_interval_seconds=30)
        credentials = Credentials(auth_method_id="xero-oauth", access_token=SecretString(OAUTH_ACCESS_TOKEN))
    else:
        config = Config(poll_interval_seconds=30)
        credentials = Credentials(
            auth_method_id="custom-connection",
            client_id="0F1E2D3C4B5A69788796A5B4C3D2E1F0",
            client_secret=SecretString("client-" + "secret"),
            access_token=SecretString(ACCESS_TOKEN),
        )
    return Client(
        config,
        StaticCredentialProvider({CONNECTION: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(handle)),
        local_provider_url="http://127.0.0.1:1",
    )


def apply_provider_step(fake: XeroInvoiceFake, step: dict[str, Any]) -> bool:
    """Apply one recorded provider change; return False for a seed or fetch step."""
    operation = step["op"]
    if operation == "upsert":
        fake.upsert(step["id"], step["number"], step["type"], step["status"], step["total"], step["stepMs"])
    elif operation == "upsertMany":
        for offset in range(step["count"]):
            invoice_id = step["idPrefix"] + f"{offset:04d}"
            fake.upsert(invoice_id, "", "ACCREC", "DRAFT", "1.00", step["stepMs"])
    elif operation == "setStatus":
        fake.set_status(step["id"], step["status"])
    elif operation == "advance":
        fake.advance_clock(step["ms"])
    elif operation == "rateLimit":
        fake.rate_limit_next(step["count"], step["problem"], step["retryAfter"])
    elif operation == "fail":
        fake.fail_next(step["count"], step["status"])
    elif operation == "limitPageSize":
        fake.limit_page_size(step["size"])
    else:
        return False
    return True


def step_configuration(step: dict[str, Any]) -> InvoiceChangedTriggerConfiguration:
    """Decode the step's binding configuration as the project configuration would hold it."""
    return decode_invoice_changed_trigger_configuration(step.get("configuration", {}))


def assert_cursor(step: dict[str, Any], token: str) -> None:
    """Compare a cursor token with the Go recording, or with its digest when it is long."""
    if "cursorOut" in step:
        assert token == step["cursorOut"]
    else:
        assert len(token) == step["cursorOutLength"]
        assert hashlib.sha256(token.encode()).hexdigest() == step["cursorOutSha256"]
    assert len(token.encode()) < 64 << 10


def assert_changes(step: dict[str, Any], changes: list[Any]) -> None:
    """Compare event IDs, times, and stored payloads with the Go recording."""
    if "changeCount" in step:
        assert len(changes) == step["changeCount"]
        ids = "\n".join(change.id for change in changes)
        assert hashlib.sha256(ids.encode()).hexdigest() == step["changeIdsSha256"]
        return
    assert [change.id for change in changes] == [recorded["eventId"] for recorded in step["changes"]]
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
        assert isinstance(error, RateLimitedError), error
        assert error.retry_after == timedelta(seconds=step["retryAfterSeconds"])
    elif expected == "failed":
        assert error is not None and not is_cursor_invalid(error) and not isinstance(error, RateLimitedError)
    else:
        assert error is None, error
    assert error is None or (ACCESS_TOKEN not in str(error) and OAUTH_ACCESS_TOKEN not in str(error))


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_fetcher_matches_go_recording(scenario: dict[str, Any]) -> None:
    """Every request, cursor token, event ID, and stored payload equals the Go connector's."""
    fake = XeroInvoiceFake()
    requests: list[str] = []
    clients = {oauth: new_fake_client(fake, requests, oauth) for oauth in (False, True)}
    outputs: dict[int, str] = {}
    try:
        for index, step in enumerate(scenario["steps"]):
            if apply_provider_step(fake, step):
                continue
            fetcher = InvoiceChangeFetcher(
                clients[step.get("oauth") is True], CONNECTION, step_configuration(step)
            )
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
                    assert has_more_pages == step["hasMorePages"], f"step {index}"
                    assert_changes(step, changes)
    finally:
        for client in clients.values():
            await client.aclose()
            await client._http_client.aclose()


def test_seen_changes_are_pruned_to_the_window_and_bounded() -> None:
    """Keys older than the window leave, duplicates collapse, and the newest bounded set stays."""
    since = BASE_TIME_MILLISECONDS + 3_600_000

    def key(invoice_id: str, milliseconds: int) -> str:
        invoice = Invoice(invoice_id=invoice_id, type="ACCREC", status="DRAFT")
        return seen_invoice_change_key(_with_updated_at(invoice, milliseconds))

    old, recent = key("a", since - 60_000), key("b", since - 1_000)
    assert prune_seen_invoice_changes([recent, old, recent], since) == [recent]
    many = [key("c", since + index) for index in range(MAXIMUM_SEEN_INVOICE_CHANGES + 5)]
    pruned = prune_seen_invoice_changes(many, since)
    assert len(pruned) == MAXIMUM_SEEN_INVOICE_CHANGES and pruned[-1] == many[-1]
    token = encode_invoice_change_cursor(InvoiceChangeCursor("type=;statuses=", "", since, 0, pruned)).token
    assert len(token) < 64 << 10
    assert decode_invoice_change_cursor(token).seen == pruned


def test_cursor_decodes_lone_surrogates_as_go_does() -> None:
    """An escaped lone surrogate decodes as U+FFFD, as Go's encoding/json does."""
    token = '{"filter":"type=;statuses=\\ud800","since":10,"seen":["\\udc00"]}'
    position = decode_invoice_change_cursor(token)
    assert position.filter == "type=;statuses=\ufffd" and position.seen == ["\ufffd"]
    assert "\\ufffd" not in encode_invoice_change_cursor(position).token


def test_cursor_keys_fold_only_as_go_does() -> None:
    """Go folds the long s and Kelvin sign to ASCII but never the dotted or dotless i."""
    position = decode_invoice_change_cursor('{"f\\u0131lter":"a","tenant\\u0130d":"t","\\u017fince":5}')
    assert (position.filter, position.tenant_id, position.since) == ("", "", 5)


@pytest.mark.parametrize("member", ["page", "pageSize", "itemCount", "pageCount"])
def test_invoice_page_rejects_a_pagination_member_go_cannot_decode(member: str) -> None:
    """Go decodes every pagination member into an int, so a string member fails the page."""
    from types import SimpleNamespace
    from typing import cast

    from dex_connectors.xero.client import Exchange
    from dex_connectors.xero.invoice_change_fetcher import _decode_invoice_change_page

    body = json.dumps({"Invoices": [], "pagination": {member: "1"}}).encode()
    with pytest.raises(ValueError, match="not a JSON object with an Invoices list"):
        _decode_invoice_change_page(cast(Exchange, SimpleNamespace(body=body, metadata={})), 1)


def _with_updated_at(invoice: Invoice, milliseconds: int) -> Invoice:
    from dataclasses import replace
    from datetime import UTC, datetime

    updated = datetime(1970, 1, 1, tzinfo=UTC) + timedelta(milliseconds=milliseconds)
    assert unix_milliseconds(updated) == milliseconds
    return replace(invoice, updated_at=updated)
