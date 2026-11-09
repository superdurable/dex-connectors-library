# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAdInput identifies one Meta Marketing API ad.
@dataclass(frozen=True, slots=True)
class GetAdInput:
    # AdID is the numeric ad ID from Ads Manager.
    ad_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'adId'})


setattr(GetAdInput, '__module__', 'dex_connectors.meta_ads_insights.models')


# AdMetadata contains selected ad identity and delivery status fields.
@dataclass(frozen=True, slots=True)
class AdMetadata:
    # ID is the stable numeric Meta ad ID.
    id: str = ''
    # Name is the ad name displayed in Ads Manager.
    name: str = ''
    # Status is the configured ad status.
    status: str = ''
    # EffectiveStatus is the actual delivery status after parent and review effects.
    effective_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'effectiveStatus'})


setattr(AdMetadata, '__module__', 'dex_connectors.meta_ads_insights.models')
