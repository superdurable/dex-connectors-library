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


# ListCampaignsInput selects a zero-based campaign page; zero size uses 20.
@dataclass(frozen=True, slots=True)
class ListCampaignsInput:
    # PageNum is the zero-based page number.
    page_num: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNum'})
    # PageSize is the number of campaigns requested, from 1 to 1000.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListCampaignsInput, '__module__', 'dex_connectors.sensorsdata.models')


# Campaign contains only channel campaign metadata.
@dataclass(frozen=True, slots=True)
class Campaign:
    # Name is the campaign name.
    name: str = ''
    # LinkCount is the number of channel links under the campaign.
    link_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'linkCount'})
    # LatestUseTime is the provider's last channel creation time.
    latest_use_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'latestUseTime'})


setattr(Campaign, '__module__', 'dex_connectors.sensorsdata.models')


# ListCampaignsOutput contains one page and its pagination metadata.
@dataclass(frozen=True, slots=True)
class ListCampaignsOutput:
    # Campaigns are the returned campaign metadata in provider order.
    campaigns: tuple[Campaign, ...] = ()
    # TotalRows is the total number of campaigns.
    total_rows: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalRows'})
    # TotalPages is the total number of pages.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})
    # PageNum is the returned zero-based page number.
    page_num: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNum'})
    # PageSize is the returned page size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'campaigns', _replace_nil_slices(self.campaigns, 1))


ListCampaignsOutput.__annotations__['campaigns'] = tuple[Campaign, ...] | None
ListCampaignsOutput.__dataclass_fields__['campaigns'].type = tuple[Campaign, ...] | None
setattr(ListCampaignsOutput, '__module__', 'dex_connectors.sensorsdata.models')
