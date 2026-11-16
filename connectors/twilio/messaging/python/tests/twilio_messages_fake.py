# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A credential-safe stand-in for the Twilio Messages list API, ported from the Go internal/twiliofake."""

import base64
import hashlib
import json
import time
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from urllib.parse import parse_qs, quote_plus, urlsplit

import httpx

API_VERSION_PATH = "/2010-04-01"


@dataclass
class _FakeMessage:
    sid: str
    account_sid: str
    from_: str
    to: str
    body: str
    status: str
    direction: str
    sent_at: datetime

    def resource(self) -> dict[str, object]:
        dated = self.sent_at.strftime("%a, %d %b %Y %H:%M:%S +0000")
        return {
            "sid": self.sid,
            "account_sid": self.account_sid,
            "from": self.from_,
            "to": self.to,
            "body": self.body,
            "status": self.status,
            "direction": self.direction,
            "num_segments": "1",
            "num_media": "0",
            "date_created": dated,
            "date_updated": dated,
            "date_sent": dated,
        }


class TwilioMessagesFake:
    """A fake account whose Messages list serves DateSent> and DateSent< (inclusive, to the second), To,
    PageSize, and next_page_uri with a PageToken, newest first. It accepts only its own Basic credential.
    """

    def __init__(self, account_sid: str, username: str, password: str) -> None:
        self.account_sid = account_sid
        self._expected_authorization = "Basic " + base64.b64encode(f"{username}:{password}".encode()).decode()
        # The nonce keeps SIDs, and so Flow IDs, unique across test runs against one Dex server.
        self._nonce = str(time.time_ns())
        self._messages: list[_FakeMessage] = []
        self._throttles: list[int] = []
        self._next = 0
        self._is_date_only = False
        self.list_requests: list[dict[str, str]] = []

    def transport(self) -> httpx.MockTransport:
        """Return an httpx transport that serves this fake."""
        return httpx.MockTransport(self._serve)

    def receive(self, from_: str, to: str, body: str, sent_at: datetime, status: str = "") -> str:
        """Add an inbound message; status is received unless the caller passes receiving."""
        return self._add(from_, to, body, sent_at, status or "received", "inbound")

    def send(self, from_: str, to: str, body: str, sent_at: datetime) -> str:
        """Add an outbound-api message, which the Trigger must ignore."""
        return self._add(from_, to, body, sent_at, "delivered", "outbound-api")

    def set_status(self, message_sid: str, status: str) -> None:
        """Change a message's status, as Twilio does when processing finishes."""
        for message in self._messages:
            if message.sid == message_sid:
                message.status = status

    def use_date_only_filters(self) -> None:
        """Compare DateSent bounds as whole GMT dates, the precision Twilio's reference documents."""
        self._is_date_only = True

    def throttle_next_list(self, retry_after_seconds: int) -> None:
        """Answer the next list request with 429 and Retry-After seconds; zero omits the header."""
        self._throttles.append(retry_after_seconds)

    def _add(self, from_: str, to: str, body: str, sent_at: datetime, status: str, direction: str) -> str:
        self._next += 1
        digest = hashlib.sha256(f"{self._nonce}{self.account_sid}{self._next}{to}{body}".encode()).hexdigest()
        moment = sent_at.astimezone(UTC).replace(microsecond=0)
        message = _FakeMessage(
            "SM" + digest[:32], self.account_sid, from_, to, body, status, direction, moment
        )
        self._messages.append(message)
        return message.sid

    def _serve(self, request: httpx.Request) -> httpx.Response:
        list_path = f"{API_VERSION_PATH}/Accounts/{self.account_sid}/Messages.json"
        message_prefix = list_path.removesuffix(".json") + "/"
        if request.headers.get("Authorization") != self._expected_authorization:
            return _error(401, 20003)
        path = request.url.path
        if request.method == "GET" and path.startswith(message_prefix):
            return self._serve_message(path.removeprefix(message_prefix).removesuffix(".json"))
        if request.method != "GET" or path != list_path:
            return _error(404, 20404)
        query = {name: values[-1] for name, values in parse_qs(urlsplit(str(request.url)).query).items()}
        self.list_requests.append(query)
        if self._throttles:
            retry_after = self._throttles.pop(0)
            response = _error(429, 20429)
            if retry_after > 0:
                response.headers["Retry-After"] = str(retry_after)
            return response
        matched = self._filter(query)
        if matched is None:
            return _error(400, 20001)
        page_size = int(query.get("PageSize", "50"))
        offset = int(query.get("PageToken", "PA0").removeprefix("PA"))
        end = min(offset + page_size, len(matched))
        next_page_uri = None
        if end < len(matched):
            following = dict(query, PageToken=f"PA{end}", Page=str(end // page_size))
            encoded = "&".join(
                quote_plus(name) + "=" + quote_plus(following[name]) for name in sorted(following)
            )
            next_page_uri = list_path + "?" + encoded
        page = {
            "messages": [message.resource() for message in matched[offset:end]],
            "page_size": page_size,
            "next_page_uri": next_page_uri,
        }
        return httpx.Response(200, json=page)

    def _serve_message(self, message_sid: str) -> httpx.Response:
        for message in self._messages:
            if message.sid == message_sid:
                return httpx.Response(200, json=message.resource())
        return _error(404, 20404)

    def _filter(self, query: dict[str, str]) -> list[_FakeMessage] | None:
        bounds: dict[str, datetime] = {}
        for name in ("DateSent>", "DateSent<"):
            if name in query:
                try:
                    parsed = datetime.strptime(query[name], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=UTC)
                except ValueError:
                    return None
                if self._is_date_only:
                    parsed = parsed.replace(hour=0, minute=0, second=0)
                    if name == "DateSent<":
                        parsed += timedelta(days=1, seconds=-1)
                bounds[name] = parsed
        matched = [
            message
            for message in self._messages
            if (not query.get("To") or message.to == query["To"])
            and ("DateSent>" not in bounds or message.sent_at >= bounds["DateSent>"])
            and ("DateSent<" not in bounds or message.sent_at <= bounds["DateSent<"])
        ]
        matched.sort(key=lambda message: (message.sent_at, message.sid), reverse=True)
        return matched


def _error(status: int, code: int) -> httpx.Response:
    return httpx.Response(status, content=json.dumps({"code": code, "status": status}).encode())
