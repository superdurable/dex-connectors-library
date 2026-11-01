# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Unsplash photo searches and photos."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class SearchPhotosInput:
    """Search phrase; zero page and per_page use Unsplash defaults, per_page at most 30."""

    query: str = ""
    page: int = field(default=0, metadata={"dex_json_omit_default": True})
    per_page: int = field(default=0, metadata={"dex_json_name": "perPage", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class GetPhotoInput:
    """Unsplash photo ID; it must not contain /, ?, &, or #."""

    id: str = ""


@dataclass(frozen=True, slots=True)
class Photo:
    """Photo identity, optional descriptions, HTTPS page and image URLs, and photographer attribution."""

    id: str = ""
    html_url: str = field(default="", metadata={"dex_json_name": "htmlUrl"})
    regular_url: str = field(default="", metadata={"dex_json_name": "regularUrl"})
    photographer_name: str = field(default="", metadata={"dex_json_name": "photographerName"})
    photographer_url: str = field(default="", metadata={"dex_json_name": "photographerURL"})
    description: str | None = field(default=None, metadata={"dex_json_omit_default": True})
    alt_description: str | None = field(
        default=None, metadata={"dex_json_name": "altDescription", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class PhotoPage:
    """One page of matching photos with Unsplash's totals."""

    total: int = 0
    total_pages: int = field(default=0, metadata={"dex_json_name": "totalPages"})
    photos: tuple[Photo, ...] = ()
