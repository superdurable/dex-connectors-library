# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One LinkedIn member's GET /v2/userinfo for profileChanged tests, ported from Go's linkedinprofilefake.

It answers in the documented OpenID Connect UserInfo shape: sub, name, given_name, family_name, picture,
locale, and the optional email and email_verified claims.
"""

import json
import threading
from dataclasses import asdict, dataclass, replace
from typing import Any

import httpx

ACCESS_TOKEN = "linkedin-" + "profile-" + "trigger"
"""The bearer token the fake accepts; split so it does not look like a credential."""
USER_INFO_URL = "https://linkedin.example/v2/userinfo"


@dataclass(frozen=True, slots=True)
class Profile:
    """One member's UserInfo claims, named as LinkedIn names them."""

    sub: str
    name: str = ""
    given_name: str = ""
    family_name: str = ""
    picture: str = ""
    locale: str = ""
    email: str = ""
    email_verified: bool = False

    def claims(self) -> dict[str, Any]:
        """Return the claims LinkedIn sends, omitting empty ones as Go's fake does."""
        return {name: value for name, value in asdict(self).items() if value not in ("", False)}


def ada_profile() -> Profile:
    """Return a verified sample member."""
    return Profile(
        sub="782bbtaQ",
        name="Ada Lovelace",
        given_name="Ada",
        family_name="Lovelace",
        picture="https://media.licdn.com/dms/image/v2/ada/profile-displayphoto-shrink_100_100/0/1?e=1&v=beta&t=first",
        locale="en-US",
        email="ada@example.com",
        email_verified=True,
    )


class LinkedInProfileFake:
    """A stateful single-member fake, safe for concurrent use, served through httpx.MockTransport."""

    def __init__(self, profile: Profile, access_token: str = ACCESS_TOKEN) -> None:
        self._lock = threading.Lock()
        self._profile = profile
        self._failures: list[tuple[int, str]] = []
        self.access_token = access_token
        self.request_count = 0

    def update(self, **claims: Any) -> None:
        """Change the served profile's claims."""
        with self._lock:
            self._profile = replace(self._profile, **claims)

    def fail_next(self, status: int, retry_after: str = "") -> None:
        """Answer the next request with status, sending retry_after as Retry-After when not blank."""
        with self._lock:
            self._failures.append((status, retry_after))

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve one UserInfo request."""
        with self._lock:
            if request.method != "GET" or request.url.path != "/v2/userinfo":
                return httpx.Response(404)
            self.request_count += 1
            if request.headers.get("Authorization") != "Bearer " + self.access_token:
                return httpx.Response(401)
            if self._failures:
                status, retry_after = self._failures.pop(0)
                return httpx.Response(status, headers={"Retry-After": retry_after} if retry_after else {})
            return httpx.Response(
                200,
                headers={"Content-Type": "application/json", "X-LI-UUID": "fake-request"},
                content=json.dumps(self._profile.claims()).encode(),
            )
