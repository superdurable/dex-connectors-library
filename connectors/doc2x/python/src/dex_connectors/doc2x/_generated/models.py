# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetParseStatusInput names a PDF parsing task created by Doc2X preupload.
@dataclass(frozen=True, slots=True)
class GetParseStatusInput:
    # UID is the opaque task identifier returned by preupload.
    uid: str = ''


setattr(GetParseStatusInput, '__module__', 'dex_connectors.doc2x.models')


# GetParseStatusOutput contains only task state and completion percent.
@dataclass(frozen=True, slots=True)
class GetParseStatusOutput:
    # Status is processing, failed, or success.
    status: str = ''
    # Progress is a percentage between zero and one hundred.
    progress: int = 0


setattr(GetParseStatusOutput, '__module__', 'dex_connectors.doc2x.models')
