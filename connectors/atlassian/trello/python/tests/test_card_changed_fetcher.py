# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go connector's cardChanged fixtures and check provider failure classification."""

import hashlib
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid

from dex_connectors.atlassian_trello import (
    CardChangedEvent,
    CardChangedTriggerConfiguration,
    Client,
    Config,
    Credentials,
    validate_card_changed_trigger_configuration,
)
from dex_connectors.atlassian_trello.card_action_cursor import (
    MAXIMUM_SEEN_ACTION_IDS,
    decode_card_action_cursor,
    encode_card_action_cursor,
)
from dex_connectors.atlassian_trello.card_action_fetcher import (
    CardActionFetcher,
    _decode_action,
    _validate_action,
)

from .trello_board_fake import API_KEY, MEMBER_ID, TOKEN, TrelloBoard

FIXTURES = json.loads((Path(__file__).parent / "fixtures" / "go-card-changed-cases.json").read_text())
REFERENCE = ConnectionRef("trello", "trello-cards")


def _context() -> TriggerContext:
    return TriggerContext(TriggerCancellation())


def _fetcher(board: TrelloBoard, board_id: str = "") -> CardActionFetcher:
    credentials = Credentials(SecretString(API_KEY), SecretString(TOKEN))
    client = Client(
        Config(poll_interval_seconds=10),
        StaticCredentialProvider({REFERENCE: credentials}),
        httpx.AsyncClient(transport=board.transport()),
    )
    return CardActionFetcher(client, REFERENCE, CardChangedTriggerConfiguration(board_id or board.board_id))


def _event_json(event: TriggerEvent[CardChangedEvent]) -> dict[str, Any]:
    payload = dex.CodecRegistry().resolve(CardChangedEvent).encode(event.payload)
    return {
        "id": event.id,
        "occurredAt": _go_time(event.occurred_at),
        "payload": json.loads(str(payload.data)),
    }


def _go_time(moment: datetime) -> str:
    """Format as Go's time.Time JSON: RFC 3339 with trailing fraction zeros trimmed."""
    text = moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S")
    if moment.microsecond:
        text += f".{moment.microsecond:06d}".rstrip("0")
    return text + "Z"


def _bulk_documents(bulk: dict[str, Any]) -> list[dict[str, Any]]:
    documents = []
    for index in range(bulk["count"]):
        milliseconds = bulk["startUnixMilli"] + index * bulk["spacingMilli"]
        date = datetime.fromtimestamp(milliseconds / 1000, UTC)
        documents.append(
            {
                "id": f"{milliseconds // 1000:08x}{bulk['firstSequence'] + index:016x}",
                "idMemberCreator": MEMBER_ID,
                "type": "createCard",
                "date": date.strftime("%Y-%m-%dT%H:%M:%S") + f".{milliseconds % 1000:03d}Z",
                "data": {"card": {"id": "6512f0a1c2d3e4f5a6b73001"}},
            }
        )
    return documents


@pytest.mark.parametrize("scenario", FIXTURES["scenarios"], ids=lambda scenario: scenario["name"])
async def test_fetch_scenarios_match_the_go_connector_byte_for_byte(scenario: dict[str, Any]) -> None:
    board = TrelloBoard(board_id=FIXTURES["boardId"])
    documents = scenario.get("actions") or _bulk_documents(scenario["bulk"])
    fetcher = _fetcher(board)
    previous = ""
    for index, step in enumerate(scenario["steps"]):
        while len(board.actions) < step["visibleCount"]:
            board.add_document(documents[len(board.actions)])
        before = len(board.requests())
        if step["operation"] == "seed":
            token = (await fetcher.seed_cursor(_context())).token
            events: list[TriggerEvent[CardChangedEvent]] = []
            has_more_pages = False
        else:
            page = await fetcher.fetch_changes(_context(), Cursor(step.get("cursor", previous)))
            token, events, has_more_pages = page.next_cursor.token, list(page.changes), page.has_more_pages
        requests = [{"path": request.path, **request.query} for request in board.requests()[before:]]
        assert requests == step["requests"], f"step {index} requests"
        assert has_more_pages == step.get("hasMorePages", False), f"step {index} hasMorePages"
        if "nextSha256" in step:
            assert hashlib.sha256(token.encode()).hexdigest() == step["nextSha256"], f"step {index} cursor"
            event_ids = "\n".join(event.id for event in events)
            assert len(events) == step.get("eventCount", 0), f"step {index} event count"
            if events:
                assert hashlib.sha256(event_ids.encode()).hexdigest() == step["eventIdsSha256"]
        else:
            assert token == step["next"], f"step {index} cursor"
            assert [_event_json(event) for event in events] == step.get("events", []), f"step {index} events"
        previous = token


@pytest.mark.parametrize("case", FIXTURES["cursorCases"], ids=lambda case: case["token"][:40])
def test_cursor_tokens_decode_and_encode_as_go_does(case: dict[str, Any]) -> None:
    try:
        position = decode_card_action_cursor(case["token"], FIXTURES["boardId"])
    except ValueError:
        assert not case["valid"]
        return
    assert case["valid"]
    assert encode_card_action_cursor(position).token == case["encoded"]


@pytest.mark.parametrize("case", FIXTURES["actionCases"], ids=lambda case: json.dumps(case["raw"])[:40])
def test_actions_decode_and_validate_as_go_does(case: dict[str, Any]) -> None:
    try:
        decoded = _decode_action(json.loads(json.dumps(case["raw"])))
        action = _validate_action(decoded)
    except (ValueError, RuntimeError):
        assert not case["valid"]
        return
    assert case["valid"]
    event = TriggerEvent("trello-action-" + action.action_id.lower(), action.event.occurred_at, action.event)
    assert _event_json(event) == case["event"]


async def test_rate_limit_pauses_one_trello_window_and_names_the_error_code() -> None:
    board = TrelloBoard()
    board.create_card("Existing card")
    fetcher = _fetcher(board)
    seed = await fetcher.seed_cursor(_context())
    board.rate_limit_next_action_requests(1)
    with pytest.raises(RateLimitedError) as raised:
        await fetcher.fetch_changes(_context(), seed)
    assert raised.value.retry_after == timedelta(seconds=10), "Trello sends no Retry-After"
    assert "API_TOKEN_LIMIT_EXCEEDED" in str(raised.value)


async def test_a_rejected_since_reseeds_and_a_missing_board_backs_off() -> None:
    board = TrelloBoard()
    board.create_card("Existing card")
    fetcher = _fetcher(board)
    seed = await fetcher.seed_cursor(_context())
    board.reject_next_cursor()
    with pytest.raises(Exception) as raised:
        await fetcher.fetch_changes(_context(), seed)
    assert is_cursor_invalid(raised.value)
    missing = _fetcher(board, "5b6893f01cb3228998cf629e")
    with pytest.raises(Exception) as missing_raised:
        await missing.seed_cursor(_context())
    assert not is_cursor_invalid(missing_raised.value)
    assert not isinstance(missing_raised.value, RateLimitedError)


async def test_unavailable_credentials_fail_the_poll_without_secrets() -> None:
    board = TrelloBoard()
    client = Client(Config(), StaticCredentialProvider({}), httpx.AsyncClient(transport=board.transport()))
    fetcher = CardActionFetcher(client, REFERENCE, CardChangedTriggerConfiguration(board.board_id))
    with pytest.raises(RuntimeError, match="credentials are unavailable"):
        await fetcher.seed_cursor(_context())
    assert board.requests() == []


async def test_a_live_board_delivers_each_action_once_oldest_first() -> None:
    board = TrelloBoard()
    existing = board.create_card("Existing card")
    fetcher = _fetcher(board)
    seed = await fetcher.seed_cursor(_context())
    created = board.create_card("Replace badge reader")
    board.move_card(created)
    board.comment_on_card(created, "Ordered the part.")
    board.delete_card(existing)
    page = await fetcher.fetch_changes(_context(), seed)
    assert [event.payload.action_type for event in page.changes] == [
        "createCard",
        "updateCard",
        "commentCard",
        "deleteCard",
    ]
    assert page.changes[1].payload.changed_fields == ("idList",)
    assert page.changes[2].payload.comment_text == "Ordered the part."
    again = await fetcher.fetch_changes(_context(), page.next_cursor)
    assert again.changes == [] or list(again.changes) == []
    assert again.next_cursor == page.next_cursor, "an empty poll leaves the cursor where it was"
    position = json.loads(page.next_cursor.token)
    assert len(position.get("seenActionIds", [])) <= MAXIMUM_SEEN_ACTION_IDS


def test_configuration_accepts_only_a_board_id_or_short_link() -> None:
    for valid in ("5b6893f01cb3228998cf629e", "AbCd1234"):
        validate_card_changed_trigger_configuration(CardChangedTriggerConfiguration(valid))
    for invalid in ("", " AbCd1234", "https://trello.com/b/AbCd1234/support", "AbCd123"):
        with pytest.raises(ValueError):
            validate_card_changed_trigger_configuration(CardChangedTriggerConfiguration(invalid))
