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


# ListQRCodesInput selects a bounded page of organization QR codes.
@dataclass(frozen=True, slots=True)
class ListQRCodesInput:
    # PageSize is 1 to 100; zero uses the provider's default of 20.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the opaque token returned as NextPageToken; blank starts the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListQRCodesInput, '__module__', 'dex_connectors.caoliao.models')


# QRCodeSummary contains a QR-code name and coding identifier without scanned records.
@dataclass(frozen=True, slots=True)
class QRCodeSummary:
    # Name is the organization's QR-code name.
    name: str = ''
    # Coding identifies the QR code for later provider operations.
    coding: str = ''


setattr(QRCodeSummary, '__module__', 'dex_connectors.caoliao.models')


# ListQRCodesOutput contains one page of QR-code metadata.
@dataclass(frozen=True, slots=True)
class ListQRCodesOutput:
    # QRCodes are the returned QR-code summaries, possibly empty.
    qr_codes: tuple[QRCodeSummary, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'qrCodes'})
    # NextPageToken is the provider cursor; blank indicates no next page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})
    # Total is the number of matching QR codes reported by Caoliao.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'qr_codes', _replace_nil_slices(self.qr_codes, 1))


ListQRCodesOutput.__annotations__['qr_codes'] = tuple[QRCodeSummary, ...] | None
ListQRCodesOutput.__dataclass_fields__['qr_codes'].type = tuple[QRCodeSummary, ...] | None
setattr(ListQRCodesOutput, '__module__', 'dex_connectors.caoliao.models')
