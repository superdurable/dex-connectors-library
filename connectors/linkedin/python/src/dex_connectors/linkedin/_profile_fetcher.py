# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The profileChanged fetcher: each UserInfo profile compared with the last one the cursor holds."""

from dataclasses import replace
from datetime import timedelta

import httpx
from dex_connectors.sdk import (
    ConnectionRef,
    ReauthorizationRequiredError,
    TriggerContext,
    TriggerEvent,
    TriggerRef,
)
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.providerhttp import ProviderTransportError, parse_retry_after

from ._profile_change import new_profile_changed_trigger_event, profile_claims_from_user_info, profile_hash
from ._profile_cursor import (
    PROFILE_CURSOR_VERSION,
    ProfileCursor,
    ProfileCursorInvalidError,
    decode_profile_cursor,
    encode_profile_cursor,
)
from .client import Client, Credentials, _decode_user_info, _UserInfoTooLargeError
from .models import ProfileChangedEvent, ProfileClaims


class ProfileFetcher:
    """Compares each UserInfo profile with the last; LinkedIn has no profile change feed."""

    def __init__(self, client: Client, connection: ConnectionRef, trigger: TriggerRef) -> None:
        self._client = client
        self._call = new_trigger_credential_call(connection, trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Read the current profile and keep it as the baseline without delivering it."""
        profile = await self._read_profile(context)
        return encode_profile_cursor(
            ProfileCursor(PROFILE_CURSOR_VERSION, profile.subject, profile_hash(profile), 0, profile)
        )

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[ProfileChangedEvent]]:
        """Return one change when the profile hash moved; another member or a bad cursor reseeds."""
        try:
            position = decode_profile_cursor(cursor.token)
        except ProfileCursorInvalidError:
            position = None
        if position is None or not position.is_usable():
            raise mark_cursor_invalid(RuntimeError("LinkedIn profile cursor is not decodable"))
        profile = await self._read_profile(context)
        if profile.subject != position.subject:
            raise mark_cursor_invalid(
                RuntimeError("LinkedIn connection now belongs to another member than the stored profile")
            )
        current_hash = profile_hash(profile)
        # An unchanged hash still stores the latest picture URL, whose signed query LinkedIn may renew.
        following = replace(position, profile=profile)
        changes: tuple[TriggerEvent[ProfileChangedEvent], ...] = ()
        if current_hash != position.hash:
            changes = (
                new_profile_changed_trigger_event(
                    position.hash,
                    current_hash,
                    position.sequence,
                    position.profile,
                    profile,
                    self._client.now(),
                ),
            )
            following = replace(following, hash=current_hash, sequence=position.sequence + 1)
        return ChangePage(changes, encode_profile_cursor(following))

    async def _read_profile(self, context: TriggerContext) -> ProfileClaims:
        """Send one UserInfo request and map LinkedIn's statuses to the poll kit's signals."""
        await wait_for_poll_request(context)
        credentials = await self._resolve_credentials()
        try:
            exchange = await self._client._send_user_info(self._call, credentials)
        except _UserInfoTooLargeError:
            raise RuntimeError("LinkedIn UserInfo response exceeds the configured size limit") from None
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            context.cancellation.raise_if_cancelled()
            raise RuntimeError("LinkedIn UserInfo request could not reach LinkedIn") from None
        context.cancellation.raise_if_cancelled()
        status = exchange.status
        if status == 429:
            # LinkedIn limits are daily; without Retry-After the runner backs off exponentially.
            retry_after = parse_retry_after(exchange.headers.get("Retry-After", ""), self._client.now())
            raise mark_rate_limited(
                RuntimeError("LinkedIn rate limit was reached"), timedelta(seconds=retry_after)
            )
        if status == 401:
            raise RuntimeError("LinkedIn authorization is invalid or revoked; reconnect the connection")
        if status == 403:
            raise RuntimeError("LinkedIn authorization is forbidden or lacks the openid or profile scope")
        if not 200 <= status < 300:
            raise RuntimeError(f"LinkedIn UserInfo request returned HTTP {status}")
        try:
            claims = _decode_user_info(exchange.body)
        except ValueError:
            raise RuntimeError("LinkedIn returned an invalid UserInfo response") from None
        profile = profile_claims_from_user_info(claims)
        if profile is None:
            raise RuntimeError("LinkedIn returned a UserInfo response without a valid sub claim")
        return profile

    async def _resolve_credentials(self) -> Credentials:
        try:
            credentials = await self._client.credentials.resolve(self._call)
        except ReauthorizationRequiredError:
            raise RuntimeError(
                "LinkedIn connection requires reauthorization; reconnect the connection"
            ) from None
        except Exception:
            raise RuntimeError("LinkedIn Trigger credentials are unavailable") from None
        if not isinstance(credentials, Credentials) or not credentials.access_token.reveal():
            raise RuntimeError("LinkedIn Trigger credentials are unavailable")
        return credentials
