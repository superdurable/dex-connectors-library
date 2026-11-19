# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPatentValuationInput identifies one patent by its publication number.
@dataclass(frozen=True, slots=True)
class GetPatentValuationInput:
    # PublicationNumber is the patent publication number, for example JP6012277B2.
    publication_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'publicationNumber'})


setattr(GetPatentValuationInput, '__module__', 'dex_connectors.zhihuiya.models')


# GetPatentValuationOutput includes only the provider's valuation and five numeric scores.
@dataclass(frozen=True, slots=True)
class GetPatentValuationOutput:
    # PublicationNumber is the provider's matching patent publication number.
    publication_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'publicationNumber'})
    # PatentID is the provider's stable patent identifier.
    patent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'patentID'})
    # PatentValueUSD is the provider's estimated patent value in US dollars.
    patent_value_usd: int = _dataclass_field(default=0, metadata={'dex_json_name': 'patentValueUSD'})
    # LegalScore is the provider's legal value score.
    legal_score: int = _dataclass_field(default=0, metadata={'dex_json_name': 'legalScore'})
    # MarketScore is the provider's market value score.
    market_score: int = _dataclass_field(default=0, metadata={'dex_json_name': 'marketScore'})
    # EconomyScore is the provider's economic value score.
    economy_score: int = _dataclass_field(default=0, metadata={'dex_json_name': 'economyScore'})
    # StrategicScore is the provider's strategic value score.
    strategic_score: int = _dataclass_field(default=0, metadata={'dex_json_name': 'strategicScore'})
    # TechnologyScore is the provider's technology value score.
    technology_score: int = _dataclass_field(default=0, metadata={'dex_json_name': 'technologyScore'})


setattr(GetPatentValuationOutput, '__module__', 'dex_connectors.zhihuiya.models')
