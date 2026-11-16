# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process Hiver API stand-in for the conversationReceived Trigger, as Go's internal/hiverfake.

It serves the inbox and conversation lists with bearer authentication, limit and opaque next_page tokens,
newest-first or oldest-first order, page tokens that expire with a 400, scripted 429 answers with
Retry-After, and one conversation read for the example Flow's getConversation Step.
"""

import json
import re
import threading
from typing import Any

import httpx

API_KEY = "hiver-fake-api-key"
"""The bearer token every request must carry."""
API_BASE_URL = "https://hiver.example/v1"
"""The `api_base_url` that routes a Client to this fake through `transport`."""
_PAGE_TOKEN = re.compile(r"^e(\d+)\.o(\d+)$")


class HiverConversationsFake:
    """Serves one or more shared inboxes; pass `transport` to the Client's httpx.AsyncClient."""

    def __init__(self, inbox_id: str, email: str) -> None:
        self._lock = threading.Lock()
        self._inboxes: dict[str, tuple[str, list[str]]] = {inbox_id: (email, [])}
        self._is_oldest_first = False
        self._token_epoch = 0
        self._rate_limits: list[int] = []
        self.conversation_list_requests = 0
        """Counts conversation-list requests, including rejected ones."""
        self.page_tokens_used: list[str] = []
        """The next_page value of every conversation-list request, "" for a first page."""
        self.transport = httpx.MockTransport(self.handle)

    def add_inbox(self, inbox_id: str, email: str) -> None:
        """Add another shared inbox with no conversations."""
        with self._lock:
            self._inboxes[inbox_id] = (email, [])

    def add_conversations(self, inbox_id: str, *conversation_ids: str) -> None:
        """Append conversations, which newest-first order lists before older ones."""
        with self._lock:
            self._inboxes[inbox_id][1].extend(conversation_ids)

    def delete_conversation(self, inbox_id: str, conversation_id: str) -> None:
        """Remove a conversation from the list."""
        with self._lock:
            conversations = self._inboxes[inbox_id][1]
            conversations[:] = [identifier for identifier in conversations if identifier != conversation_id]

    def list_oldest_first(self) -> None:
        """List the oldest conversation first, so new ones land on the last page."""
        with self._lock:
            self._is_oldest_first = True

    def expire_page_tokens(self) -> None:
        """Make every next_page token issued so far answer 400."""
        with self._lock:
            self._token_epoch += 1

    def rate_limit_next(self, retry_after_seconds: int) -> None:
        """Answer the next list request with 429 and, when positive, Retry-After seconds."""
        with self._lock:
            self._rate_limits.append(retry_after_seconds)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve one request."""
        if request.method != "GET" or request.headers.get("Authorization") != "Bearer " + API_KEY:
            return _json(401, {"errors": [{"message": "unauthorized"}]})
        parts = request.url.path.removeprefix("/v1/").strip("/").split("/")
        with self._lock:
            if parts == ["inboxes"]:
                results = [
                    {"id": key, "display_name": "Support", "email": email, "channel_type": "email"}
                    for key, (email, _) in self._inboxes.items()
                ]
                return _json(200, {"data": {"results": results, "pagination": {"next_page": None}}})
            if len(parts) == 3 and parts[0] == "inboxes" and parts[2] == "conversations":
                return self._serve_conversations(request, parts[1])
            if len(parts) == 4 and parts[0] == "inboxes" and parts[2] == "conversations":
                return self._serve_conversation(parts[1], parts[3])
        return _json(404, {"Message": "not found"})

    def _serve_conversations(self, request: httpx.Request, inbox_id: str) -> httpx.Response:
        self.conversation_list_requests += 1
        page_token = request.url.params.get("next_page", "")
        self.page_tokens_used.append(page_token)
        if self._rate_limits:
            retry_after = self._rate_limits.pop(0)
            headers = {"Retry-After": str(retry_after)} if retry_after > 0 else {}
            return _json(429, {"Message": "Too Many Requests"}, headers)
        if inbox_id not in self._inboxes:
            return _json(404, {"Message": "inbox not found"})
        try:
            limit = int(request.url.params.get("limit", ""))
        except ValueError:
            limit = 0
        if not 10 <= limit <= 100:
            return _json(400, {"Message": "limit must be 10 to 100"})
        offset = 0
        if page_token:
            match = _PAGE_TOKEN.fullmatch(page_token)
            if match is None or int(match.group(1)) != self._token_epoch:
                return _json(400, {"errors": [{"message": "invalid page token"}]})
            offset = int(match.group(2))
        ordered = list(self._inboxes[inbox_id][1])
        if not self._is_oldest_first:
            ordered.reverse()
        end = min(offset + limit, len(ordered))
        results = [_conversation_json(identifier) for identifier in ordered[min(offset, len(ordered)) : end]]
        next_page = f"e{self._token_epoch}.o{end}" if end < len(ordered) else None
        return _json(200, {"data": {"results": results, "pagination": {"next_page": next_page}}})

    def _serve_conversation(self, inbox_id: str, conversation_id: str) -> httpx.Response:
        if inbox_id not in self._inboxes or conversation_id not in self._inboxes[inbox_id][1]:
            return _json(404, {"Message": "conversation not found"})
        conversation = _conversation_json(conversation_id)
        conversation["message_ids"] = [
            {"hiver_message_id": "8" + conversation_id, "gmail_message_id": "19cfm" + conversation_id}
        ]
        return _json(200, {"data": [conversation]})


def _conversation_json(conversation_id: str) -> dict[str, Any]:
    return {
        "id": conversation_id,
        "status": "open",
        "tag_ids": [],
        "gmail_thread_id": "19cf" + conversation_id,
        "private_permalink": "https://app.hiverhq.com/conversation/" + conversation_id,
    }


def _json(status: int, body: Any, headers: dict[str, str] | None = None) -> httpx.Response:
    return httpx.Response(
        status,
        content=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", **(headers or {})},
    )
