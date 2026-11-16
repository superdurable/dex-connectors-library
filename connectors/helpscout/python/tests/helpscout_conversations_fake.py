# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful stand-in for the Help Scout conversation read and tag APIs the tagging example calls."""

import json
import re
from dataclasses import dataclass, field

import httpx

# Not a credential: an inert fixture token the fake requires as the bearer.
ACCESS_TOKEN = "fake-helpscout-" + "access-token"
_CONVERSATION_PATH = re.compile(r"/v2/conversations/([0-9]+)(/threads|/tags)?")


@dataclass
class FakeConversation:
    """One conversation's mutable state."""

    conversation_id: int
    mailbox_id: int
    status: str = "active"
    tags: list[str] = field(default_factory=list)


class HelpScoutConversationsFake:
    """Serves GET conversation, GET threads, and PUT tags for the conversations it holds."""

    def __init__(self) -> None:
        self.conversations: dict[int, FakeConversation] = {}
        self.requests: list[tuple[str, str]] = []

    def add(self, conversation_id: int, mailbox_id: int, status: str = "active") -> bytes:
        """Store a conversation and return the webhook body Help Scout would send for it."""
        self.conversations[conversation_id] = FakeConversation(conversation_id, mailbox_id, status)
        return json.dumps(self._wire(self.conversations[conversation_id])).encode()

    def tag_writes(self, conversation_id: int) -> int:
        """Count the PUT tags requests for one conversation."""
        return self.requests.count(("PUT", f"/v2/conversations/{conversation_id}/tags"))

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one provider request."""
        self.requests.append((request.method, request.url.path))
        if request.headers.get("authorization") != f"Bearer {ACCESS_TOKEN}":
            return httpx.Response(401, json={"message": "unauthorized"})
        match = _CONVERSATION_PATH.fullmatch(request.url.path)
        conversation = self.conversations.get(int(match.group(1))) if match else None
        if match is None or conversation is None:
            return httpx.Response(404, json={"message": "not found"})
        suffix = match.group(2)
        if request.method == "GET" and suffix is None:
            return httpx.Response(200, json=self._wire(conversation))
        if request.method == "GET" and suffix == "/threads":
            thread = {
                "id": conversation.conversation_id * 10,
                "type": "customer",
                "status": "active",
                "state": "published",
                "body": "I was charged twice",
                "createdAt": "2026-09-30T11:58:00Z",
            }
            return httpx.Response(200, json={"_embedded": {"threads": [thread]}, "page": {"totalPages": 1}})
        if request.method == "PUT" and suffix == "/tags":
            conversation.tags = list(json.loads(request.content)["tags"])
            return httpx.Response(204)
        return httpx.Response(405)

    @staticmethod
    def _wire(conversation: FakeConversation) -> dict[str, object]:
        return {
            "id": conversation.conversation_id,
            "number": conversation.conversation_id % 1000,
            "threads": 1,
            "type": "email",
            "status": conversation.status,
            "state": "published",
            "subject": "Double charge on order 88213",
            "mailboxId": conversation.mailbox_id,
            "createdAt": "2026-09-30T11:58:00Z",
            "primaryCustomer": {"id": 238604, "type": "customer", "email": "jane@acme.example.com"},
            "tags": [{"id": index + 1, "tag": tag} for index, tag in enumerate(conversation.tags)],
        }
