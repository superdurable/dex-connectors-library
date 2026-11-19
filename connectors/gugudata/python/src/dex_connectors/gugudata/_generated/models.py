# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# SegmentTextInput selects short text for Chinese NLP segmentation.
@dataclass(frozen=True, slots=True)
class SegmentTextInput:
    # Text is nonblank UTF-8 text of at most 2000 UTF-16 code units.
    text: str = ''


setattr(SegmentTextInput, '__module__', 'dex_connectors.gugudata.models')


# SegmentTextOutput contains GuGuData's comma-separated words in source order.
@dataclass(frozen=True, slots=True)
class SegmentTextOutput:
    # SegmentedText joins provider segments with the documented default comma.
    segmented_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'segmentedText'})


setattr(SegmentTextOutput, '__module__', 'dex_connectors.gugudata.models')
