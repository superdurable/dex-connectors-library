# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Cloudflare zone pages and URL cache purges."""

from dataclasses import dataclass

from ._generated.models import ListZonesInput as ListZonesInput
from ._generated.models import PurgeCacheByURLsInput as PurgeCacheByURLsInput
from ._generated.models import Zone as Zone
from ._generated.models import ZonesPage as ZonesPage


@dataclass(frozen=True, slots=True)
class PurgeRequest:
    """Cloudflare's purge request ID."""

    id: str = ""


@dataclass(frozen=True, slots=True)
class CachePurge:
    """Cloudflare's acceptance of the purge request, not a cache-miss guarantee."""

    success: bool = False
    result: PurgeRequest = PurgeRequest()


# connectorctl: model-alias CachePurgeResult=PurgeRequest

ListZonesInput.__doc__ = "Request for the default first page of Cloudflare zones; it carries no parameters."
Zone.__doc__ = "A zone's identifier used by cache operations, its DNS name, and its provider-reported status."
ZonesPage.__doc__ = "Cloudflare's success flag and one zone page, possibly empty."
PurgeCacheByURLsInput.__doc__ = "The zone whose cache is purged and 1 through 30 absolute HTTP or HTTPS URLs."
