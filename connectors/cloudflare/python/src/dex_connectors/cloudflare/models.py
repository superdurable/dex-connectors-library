# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Cloudflare zone pages and URL cache purges."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListZonesInput:
    """Request for the default first page of Cloudflare zones; it carries no parameters."""


@dataclass(frozen=True, slots=True)
class Zone:
    """A zone's identifier used by cache operations, its DNS name, and its provider-reported status."""

    id: str = ""
    name: str = ""
    status: str = ""


@dataclass(frozen=True, slots=True)
class ZonesPage:
    """Cloudflare's success flag and one zone page, possibly empty."""

    success: bool = False
    result: tuple[Zone, ...] = ()


@dataclass(frozen=True, slots=True)
class PurgeCacheByURLsInput:
    """The zone whose cache is purged and 1 through 30 absolute HTTP or HTTPS URLs."""

    zone_id: str = field(default="", metadata={"dex_json_name": "zoneId"})
    urls: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class PurgeRequest:
    """Cloudflare's purge request ID."""

    id: str = ""


@dataclass(frozen=True, slots=True)
class CachePurge:
    """Cloudflare's acceptance of the purge request, not a cache-miss guarantee."""

    success: bool = False
    result: PurgeRequest = PurgeRequest()
