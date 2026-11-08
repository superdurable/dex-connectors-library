# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListMediaInput selects a professional Instagram account and an optional media cursor.
@dataclass(frozen=True, slots=True)
class ListMediaInput:
    # IGUserID is the numeric Instagram account ID linked to a Facebook Page.
    ig_user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'igUserId'})
    # After is Meta's opaque cursor from a prior page; blank starts at the first page.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListMediaInput, '__module__', 'dex_connectors.meta_instagram_business.models')


# InstagramMedia contains one media object's public identity and display fields.
@dataclass(frozen=True, slots=True)
class InstagramMedia:
    # ID is Meta's stable Instagram media ID.
    id: str = ''
    # Caption is the optional caption of the media object.
    caption: str = ''
    # Timestamp is Meta's raw media timestamp.
    timestamp: str = ''
    # Permalink is the optional public HTTPS URL of the media object.
    permalink: str = ''
    # MediaType is IMAGE, VIDEO, or CAROUSEL_ALBUM.
    media_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mediaType'})


setattr(InstagramMedia, '__module__', 'dex_connectors.meta_instagram_business.models')


# MediaListPage contains one bounded Instagram media page.
@dataclass(frozen=True, slots=True)
class MediaListPage:
    # Media are the account media objects in provider order.
    media: tuple[InstagramMedia, ...] = ()
    # NextCursor is blank when Meta reported no later page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'media', _replace_nil_slices(self.media, 1))


MediaListPage.__annotations__['media'] = tuple[InstagramMedia, ...] | None
MediaListPage.__dataclass_fields__['media'].type = tuple[InstagramMedia, ...] | None
setattr(MediaListPage, '__module__', 'dex_connectors.meta_instagram_business.models')
