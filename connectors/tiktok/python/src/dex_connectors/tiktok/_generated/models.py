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


# ListCampaignsInput selects one advertiser campaign page.
@dataclass(frozen=True, slots=True)
class ListCampaignsInput:
    # AdvertiserID is the decimal ad account ID from TikTok Ads Manager.
    advertiser_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'advertiserId'})
    # Page starts at one; zero uses one.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize accepts 1 through 1000 campaigns; zero uses ten.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListCampaignsInput, '__module__', 'dex_connectors.tiktok.models')


# CampaignMetadata contains a stable campaign ID and display name.
@dataclass(frozen=True, slots=True)
class CampaignMetadata:
    # ID is the decimal campaign ID returned by TikTok.
    id: str = ''
    # Name is the campaign's display name.
    name: str = ''


setattr(CampaignMetadata, '__module__', 'dex_connectors.tiktok.models')


# CampaignsPage contains one bounded campaign page and pagination metadata.
@dataclass(frozen=True, slots=True)
class CampaignsPage:
    # Campaigns are the campaign identities on this page.
    campaigns: tuple[CampaignMetadata, ...] = ()
    # Page is the one-based page number returned by TikTok.
    page: int = 0
    # TotalPages is zero when no campaign pages exist.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'campaigns', _replace_nil_slices(self.campaigns, 1))


CampaignsPage.__annotations__['campaigns'] = tuple[CampaignMetadata, ...] | None
CampaignsPage.__dataclass_fields__['campaigns'].type = tuple[CampaignMetadata, ...] | None
setattr(CampaignsPage, '__module__', 'dex_connectors.tiktok.models')
