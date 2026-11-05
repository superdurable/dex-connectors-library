# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Spotify playlist HTTP client and Go-compatible Query classification."""

import ipaddress
import json
import re
from datetime import UTC, datetime
from urllib.parse import urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    ACCESS_DENIED,
    AUTHORIZATION_REVOKED,
    DEFECT,
    INVALID_RESPONSE,
    LIST_PLAYLIST_TRACKS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ._generated.timestamps import parse_go_datetime
from .models import ListPlaylistTracksInput, PlaylistAlbum, PlaylistArtist, PlaylistTrack, PlaylistTrackPage

_SPOTIFY_ID = re.compile(r"[A-Za-z0-9]{22}\Z")


def _encode_query(values: dict[str, str]) -> str:
    """Match Go url.Values.Encode for sorted query pairs."""
    return urlencode(sorted(values.items()))


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "spotify", "listPlaylistTracks", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PlaylistTrackPage]:
    return QueryAttempt.branch_result(branch, PlaylistTrackPage(), Receipt(), _failure(kind, message))


def _is_loopback(host: str | None) -> bool:
    if host is None:
        return False
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


class Client:
    """Send bounded playlist requests with a call-time Spotify credential source.

    The client takes ownership of an injected HTTP client and closes it in
    ``aclose``. The generated project connection shares this transport with
    its OAuth refresh driver.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Spotify configuration and credentials are required")
        config = Config(
            config.endpoint or Config().endpoint,
            config.timeout_seconds or Config().timeout_seconds,
            config.max_response_bytes or Config().max_response_bytes,
        )
        endpoint = urlsplit(config.endpoint)
        if (
            not endpoint.scheme
            or not endpoint.hostname
            or (endpoint.scheme != "https" and not _is_loopback(endpoint.hostname))
        ):
            raise ValueError("Spotify endpoint must be HTTPS or loopback")
        if (
            endpoint.username is not None
            or endpoint.password is not None
            or endpoint.query
            or endpoint.fragment
        ):
            raise ValueError("Spotify endpoint cannot contain user info, query, or fragment")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Spotify timeout and maxResponseBytes must be positive")
        self.config = config
        self.endpoint = config.endpoint.rstrip("/")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_playlist_tracks(self) -> "ListPlaylistTracksOperation":
        """Return the playlist Query bound to this client."""
        return ListPlaylistTracksOperation(self)

    async def aclose(self) -> None:
        """Close the provider and OAuth transport after Worker shutdown."""
        await self.http_client.aclose()


class ListPlaylistTracksOperation:
    """List one Spotify playlist page per Dex Execute attempt."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Spotify client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Spotify operation contract."""
        return LIST_PLAYLIST_TRACKS_DEFINITION

    async def invoke(self, call: Call, input: ListPlaylistTracksInput) -> QueryAttempt[PlaylistTrackPage]:
        """Validate input, resolve OAuth credentials, and classify the provider response."""
        try:
            playlist_id, market, limit, offset = _validate_input(input)
        except ValueError as error:
            return _branch(DEFECT, FailureKind.VALIDATION, str(error))
        try:
            credentials = await self.client.credentials.resolve(call)
            if not isinstance(credentials, Credentials) or not credentials.access_token.reveal():
                raise ValueError("missing access token")
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Spotify authorization is unavailable or revoked"
            )
        query_values = {"limit": str(limit), "offset": str(offset)}
        if market:
            query_values["market"] = market
        target = f"{self.client.endpoint}/playlists/{playlist_id}/items?{_encode_query(query_values)}"
        for request_attempt in range(2):
            try:
                request = self.client.http_client.build_request(
                    "GET",
                    target,
                    headers={
                        "Accept": "application/json",
                        "Authorization": f"Bearer {credentials.access_token.reveal()}",
                        "User-Agent": "superdurable-dex-spotify-connector/0.1",
                    },
                    timeout=self.client.config.timeout_seconds,
                )
                response, _ = await send_traced(self.client.http_client, request)
                status = response.status_code
                headers = response.headers
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Spotify playlist response exceeds the configured size limit",
                )
            except (ProviderTransportError, httpx.HTTPError, ValueError):
                return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "Spotify is unavailable"))
            if (
                status == 401
                and request_attempt == 0
                and hasattr(self.client.credentials, "resolve_after_rejection")
            ):
                try:
                    credentials = await self.client.credentials.resolve_after_rejection(call)
                    if credentials.access_token.reveal():
                        continue
                except Exception:
                    pass
            break
        if status == 429:
            delay = _retry_after(headers)
            return QueryAttempt.retry(
                _failure(FailureKind.RATE_LIMIT, "Spotify rate limit was reached"), delay
            )
        if status >= 500:
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "Spotify is unavailable"))
        if status == 401:
            return _branch(
                AUTHORIZATION_REVOKED,
                FailureKind.AUTHENTICATION,
                "Spotify authorization is invalid or revoked",
            )
        if status == 403:
            return _branch(
                ACCESS_DENIED, FailureKind.AUTHORIZATION, "Spotify denied access to the playlist items"
            )
        if status == 404:
            return _branch(NOT_FOUND, FailureKind.NOT_FOUND, "Spotify playlist was not found")
        if not 200 <= status < 300:
            return _branch(
                PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "Spotify rejected the playlist query"
            )
        try:
            page = _decode_page(body, playlist_id)
        except (ValueError, KeyError, TypeError):
            return _branch(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Spotify returned an invalid playlist response"
            )
        return QueryAttempt.branch_result(LISTED, page, Receipt())


def _validate_input(input: ListPlaylistTracksInput) -> tuple[str, str, int, int]:
    if not isinstance(input, ListPlaylistTracksInput):
        raise ValueError("playlist input is required")
    playlist_id = input.playlist_id.strip()
    market = input.market.strip().upper()
    limit = input.limit or 20
    if not _SPOTIFY_ID.fullmatch(playlist_id):
        raise ValueError("playlist ID must be a 22-character Spotify ID")
    if market and not re.fullmatch(r"[A-Z]{2}", market):
        raise ValueError("market must be an ISO 3166-1 alpha-2 country code")
    if type(limit) is not int or not 1 <= limit <= 50:
        raise ValueError("limit must be between 1 and 50")
    if type(input.offset) is not int or not 0 <= input.offset <= 2147483647:
        raise ValueError("offset must be between 0 and 2147483647")
    return playlist_id, market, limit, input.offset


def _retry_after(headers: httpx.Headers) -> float:
    return parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC)) or 60.0


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict):
        raise ValueError("expected object")
    return value


def _string(value: object) -> str:
    if not isinstance(value, str):
        raise ValueError("expected string")
    return value


def _integer(value: object) -> int:
    if type(value) is not int or not -(2**63) <= value <= 2**63 - 1:
        raise ValueError("expected integer")
    return value


def _boolean(value: object) -> bool:
    if type(value) is not bool:
        raise ValueError("expected boolean")
    return value


def _field_string(value: dict[str, object], key: str) -> str:
    item = value.get(key, "")
    return "" if item is None else _string(item)


def _field_integer(value: dict[str, object], key: str) -> int:
    item = value.get(key, 0)
    return 0 if item is None else _integer(item)


def _field_boolean(value: dict[str, object], key: str) -> bool:
    item = value.get(key, False)
    return False if item is None else _boolean(item)


def _array(value: object) -> list[object]:
    if not isinstance(value, list):
        raise ValueError("expected array")
    return value


def _metadata(value: str, required: bool = False) -> str:
    if required and not value:
        raise ValueError("missing metadata")
    if len(value.encode()) > 4096 or any(
        ord(character) < 32 or 127 <= ord(character) < 160 for character in value
    ):
        raise ValueError("invalid metadata")
    return value


def _spotify_uri(value: str) -> str:
    if value and (not value.startswith("spotify:") or len(value.encode()) > 2048):
        raise ValueError("invalid Spotify URI")
    return _metadata(value)


def _spotify_url(value: str) -> str:
    if not value:
        return value
    parsed = urlsplit(value)
    if (
        len(value.encode()) > 2048
        or parsed.scheme != "https"
        or (parsed.hostname or "").lower() != "open.spotify.com"
        or parsed.username is not None
    ):
        raise ValueError("invalid Spotify URL")
    return value


def _external_url(provider: dict[str, object]) -> str:
    external = provider.get("external_urls")
    if external is None:
        return ""
    return _spotify_url(_field_string(_object(external), "spotify"))


def _artist(provider: object) -> PlaylistArtist:
    value = _object(provider)
    artist_id = _field_string(value, "id")
    if artist_id and not _SPOTIFY_ID.fullmatch(artist_id):
        raise ValueError("invalid Spotify artist ID")
    return PlaylistArtist(
        artist_id,
        _spotify_uri(_field_string(value, "uri")),
        _external_url(value),
        _metadata(_field_string(value, "name"), True),
    )


def _album(provider: object) -> PlaylistAlbum:
    value = _object(provider)
    album_id = _field_string(value, "id")
    if album_id and not _SPOTIFY_ID.fullmatch(album_id):
        raise ValueError("invalid Spotify album ID")
    artists = tuple(_artist(artist) for artist in _array(value.get("artists")))
    precision = _field_string(value, "release_date_precision")
    if precision and precision not in ("year", "month", "day"):
        raise ValueError("invalid Spotify release date precision")
    return PlaylistAlbum(
        album_id,
        _spotify_uri(_field_string(value, "uri")),
        _external_url(value),
        _metadata(_field_string(value, "name"), True),
        _metadata(_field_string(value, "release_date")),
        precision,
        artists,
    )


def _track(provider: object, item: dict[str, object], position: int) -> PlaylistTrack:
    value = _object(provider)
    is_local = _field_boolean(item, "is_local") or _field_boolean(value, "is_local")
    track_id = _field_string(value, "id")
    if track_id and not _SPOTIFY_ID.fullmatch(track_id) or not is_local and not track_id:
        raise ValueError("invalid Spotify track ID")
    duration = _field_integer(value, "duration_ms")
    disc = _field_integer(value, "disc_number")
    number = _field_integer(value, "track_number")
    if min(duration, disc, number) < 0:
        raise ValueError("invalid Spotify track fields")
    artists = tuple(_artist(artist) for artist in _array(value.get("artists")))
    album = _album(value.get("album"))
    user = item.get("added_by")
    user_id = _field_string(_object(user), "id") if user is not None else ""
    if len(user_id.encode()) > 128 or any(ord(character) < 32 for character in user_id):
        raise ValueError("invalid Spotify user ID")
    added = item.get("added_at")
    added_at = None
    if added is not None:
        text = _string(added)
        added_at = parse_go_datetime(text, "added_at")
    playable = value.get("is_playable")
    if playable is not None:
        playable = _boolean(playable)
    return PlaylistTrack(
        position,
        added_at,
        user_id,
        is_local,
        track_id,
        _spotify_uri(_field_string(value, "uri")),
        _external_url(value),
        _metadata(_field_string(value, "name"), True),
        artists,
        album,
        duration,
        _field_boolean(value, "explicit"),
        playable,
        disc,
        number,
    )


def _decode_page(body: bytes, playlist_id: str) -> PlaylistTrackPage:
    provider = _object(json.loads(body, parse_constant=_reject_nonfinite))
    limit = _field_integer(provider, "limit")
    offset = _field_integer(provider, "offset")
    total = _field_integer(provider, "total")
    items = _array(provider.get("items"))
    if not 1 <= limit <= 50 or offset < 0 or total < 0 or len(items) > limit:
        raise ValueError("invalid Spotify playlist page")
    tracks = []
    skipped = 0
    for index, raw_item in enumerate(items):
        item = _object(raw_item)
        track = item.get("item")
        if track is None or _field_string(_object(track), "type") != "track":
            skipped += 1
            continue
        tracks.append(_track(track, item, offset + index))
    next_page = provider.get("next")
    next_offset = 0
    if next_page is not None:
        if not _string(next_page) or not items or offset > 2147483647 - len(items):
            raise ValueError("invalid Spotify next page")
        next_offset = offset + len(items)
    return PlaylistTrackPage(playlist_id, tuple(tracks), skipped, limit, offset, total, next_offset)


def _reject_nonfinite(value: str) -> object:
    raise ValueError("Spotify returned a non-finite JSON number")
