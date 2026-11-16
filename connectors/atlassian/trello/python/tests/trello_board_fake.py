# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process Trello board serving its action history and cards, as the Go trelloboardfake does.

GET /1/boards/{board}/actions honors filter, limit, since (exclusive), and before, newest first, and GET
/1/cards/{card} returns a card. It requires the test key and token in Trello's OAuth header.
"""

import json
import threading
import time
from dataclasses import dataclass, field
from typing import Any

import httpx

from dex_connectors.atlassian_trello.card_action_cursor import parse_go_time

API_KEY = "fake-trello-key"
TOKEN = "fake-trello-token"
MEMBER_ID = "5a1e4c7b2f0d3e6a9b8c7d6e"
TODO_LIST_ID = "5b6893f01cb3228998cf62a1"
DONE_LIST_ID = "5b6893f01cb3228998cf62a2"
_LIST_NAMES = {TODO_LIST_ID: "To do", DONE_LIST_ID: "Done"}


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One request the board answered: its path, decoded query, and status."""

    path: str
    query: dict[str, str]
    status: int


@dataclass(slots=True)
class _Action:
    document: dict[str, Any]
    action_id: str
    action_type: str
    occurred_nanoseconds: int


@dataclass
class TrelloBoard:
    """One fake board; safe to drive from a test while the connector polls it."""

    board_id: str = ""
    sequence: int = field(default_factory=time.time_ns)
    actions: list[_Action] = field(default_factory=list)
    cards: dict[str, dict[str, Any]] = field(default_factory=dict)
    pending_rate_limits: int = 0
    should_reject_next_cursor: bool = False
    recorded: list[RecordedRequest] = field(default_factory=list)
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def __post_init__(self) -> None:
        if not self.board_id:
            self.board_id = self.new_object_id(time.time())

    def transport(self) -> httpx.MockTransport:
        """Return a transport that answers the connector's requests from this board."""
        return httpx.MockTransport(self._handle)

    def new_object_id(self, created_seconds: float) -> str:
        """Mimic Trello's Mongo IDs: four bytes of creation seconds, then a counter."""
        self.sequence += 1
        return f"{int(created_seconds) & 0xFFFFFFFF:08x}{self.sequence & 0xFFFFFFFFFFFFFFFF:016x}"

    def add_document(self, document: dict[str, Any]) -> None:
        """Append one raw action document, as the Go fixtures record them."""
        moment = parse_go_time(document["date"])
        assert moment is not None
        with self._lock:
            self.actions.append(_Action(document, document["id"], document["type"], moment.unix_nanoseconds))

    def create_card(self, name: str) -> str:
        """Add a card to the To do list and record createCard; returns the card ID."""
        now = time.time()
        card_id = self.new_object_id(now)
        number = len(self.cards) + 1
        card = {
            "id": card_id,
            "shortLink": f"Sh{number:06d}",
            "idShort": number,
            "name": name,
            "desc": "",
            "idBoard": self.board_id,
            "idList": TODO_LIST_ID,
            "closed": False,
            "pos": 16384,
        }
        self.cards[card_id] = card
        self._record("createCard", now, card, {"list": _list(TODO_LIST_ID)})
        return card_id

    def move_card(self, card_id: str) -> None:
        """Move a card to Done and record updateCard with listBefore and listAfter."""
        card = self.cards[card_id]
        before = card["idList"]
        card["idList"] = DONE_LIST_ID
        self._record(
            "updateCard",
            time.time(),
            card,
            {"listBefore": _list(before), "listAfter": _list(DONE_LIST_ID), "old": {"idList": before}},
        )

    def rename_card(self, card_id: str, name: str) -> None:
        """Rename a card and record updateCard with the old name."""
        card = self.cards[card_id]
        old = card["name"]
        card["name"] = name
        self._record("updateCard", time.time(), card, {"old": {"name": old}})

    def comment_on_card(self, card_id: str, text: str) -> None:
        """Record commentCard with text."""
        self._record("commentCard", time.time(), self.cards[card_id], {"text": text})

    def delete_card(self, card_id: str) -> None:
        """Remove a card and record deleteCard, which keeps only its ID, short ID, and list."""
        card = self.cards.pop(card_id)
        self._record(
            "deleteCard",
            time.time(),
            {"id": card_id, "idShort": card["idShort"], "idList": card["idList"]},
            {},
        )

    def rate_limit_next_action_requests(self, count: int) -> None:
        """Answer the next count action requests with 429 API_TOKEN_LIMIT_EXCEEDED."""
        self.pending_rate_limits = count

    def reject_next_cursor(self) -> None:
        """Answer the next action request that sends since with 400 invalid value for since."""
        self.should_reject_next_cursor = True

    def requests(self) -> list[RecordedRequest]:
        """Return the requests answered so far."""
        with self._lock:
            return list(self.recorded)

    def _record(self, action_type: str, seconds: float, card: dict[str, Any], extra: dict[str, Any]) -> None:
        nanoseconds = int(seconds * 1000) * 1_000_000
        data: dict[str, Any] = {
            "board": {"id": self.board_id, "name": "Support queue", "shortLink": "Supp0rtQ"},
            "card": {
                key: card[key] for key in ("id", "name", "idShort", "shortLink", "idList") if key in card
            },
        }
        data.update(extra)
        date = time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(nanoseconds // 1_000_000_000))
        date += f".{nanoseconds // 1_000_000 % 1000:03d}Z"
        document = {
            "id": self.new_object_id(seconds),
            "idMemberCreator": MEMBER_ID,
            "type": action_type,
            "date": date,
            "data": data,
            "memberCreator": {"id": MEMBER_ID, "fullName": "Grace Hopper", "username": "gracehopper"},
        }
        self.add_document(document)

    def _handle(self, request: httpx.Request) -> httpx.Response:
        expected = f'OAuth oauth_consumer_key="{API_KEY}", oauth_token="{TOKEN}"'
        query = dict(request.url.params)
        response = self._respond(request, query, expected)
        with self._lock:
            self.recorded.append(RecordedRequest(request.url.path, query, response.status_code))
        return response

    def _respond(self, request: httpx.Request, query: dict[str, str], expected: str) -> httpx.Response:
        if request.headers.get("Authorization") != expected:
            return httpx.Response(401, text="invalid key")
        segments = request.url.path.removeprefix("/1/").split("/")
        if (
            request.method == "GET"
            and len(segments) == 3
            and segments[0] == "boards"
            and segments[2] == "actions"
        ):
            return self._serve_actions(segments[1], query)
        if request.method == "GET" and len(segments) == 2 and segments[0] == "cards":
            card = self.cards.get(segments[1])
            if card is None:
                return httpx.Response(404, text="The requested resource was not found.")
            return httpx.Response(200, json=card)
        return httpx.Response(404, text="Not Found")

    def _serve_actions(self, board_id: str, query: dict[str, str]) -> httpx.Response:
        if board_id != self.board_id:
            return httpx.Response(404, text="The requested resource was not found.")
        if self.pending_rate_limits > 0:
            self.pending_rate_limits -= 1
            return httpx.Response(
                429, json={"error": "API_TOKEN_LIMIT_EXCEEDED", "message": "Rate limit exceeded"}
            )
        if self.should_reject_next_cursor and "since" in query:
            self.should_reject_next_cursor = False
            return httpx.Response(400, text="invalid value for since")
        limit = int(query["limit"])
        filters = query["filter"].split(",")
        since = parse_go_time(query["since"]) if "since" in query else None
        before = query.get("before")
        with self._lock:
            listed = [
                action
                for action in self.actions
                if action.action_type in filters
                and (since is None or action.occurred_nanoseconds > since.unix_nanoseconds)
                and (before is None or action.action_id < before)
            ]
        listed.sort(key=lambda action: (action.occurred_nanoseconds, action.action_id), reverse=True)
        return httpx.Response(200, content=json.dumps([action.document for action in listed[:limit]]))


def _list(list_id: str) -> dict[str, str]:
    return {"id": list_id, "name": _LIST_NAMES[list_id]}
