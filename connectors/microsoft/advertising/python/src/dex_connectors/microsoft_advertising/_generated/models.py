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


# ListCampaignsInput identifies the Microsoft Advertising account to query.
@dataclass(frozen=True, slots=True)
class ListCampaignsInput:
    # AccountID is the numeric aid in the signed-in Campaigns URL.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})


setattr(ListCampaignsInput, '__module__', 'dex_connectors.microsoft_advertising.models')


# CampaignSummary contains public campaign fields selected for a Flow.
@dataclass(frozen=True, slots=True)
class CampaignSummary:
    # ID is the stable Microsoft Advertising campaign ID.
    id: str = ''
    # Name is the campaign's display name.
    name: str = ''
    # CampaignType is the provider campaign type.
    campaign_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'campaignType'})
    # Status is the campaign's provider status.
    status: str = ''


setattr(CampaignSummary, '__module__', 'dex_connectors.microsoft_advertising.models')


# CampaignsPage is the bounded campaign list returned for one account.
@dataclass(frozen=True, slots=True)
class CampaignsPage:
    # Campaigns contains the returned campaigns, which may be empty.
    campaigns: tuple[CampaignSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'campaigns', _replace_nil_slices(self.campaigns, 1))


CampaignsPage.__annotations__['campaigns'] = tuple[CampaignSummary, ...] | None
CampaignsPage.__dataclass_fields__['campaigns'].type = tuple[CampaignSummary, ...] | None
setattr(CampaignsPage, '__module__', 'dex_connectors.microsoft_advertising.models')
