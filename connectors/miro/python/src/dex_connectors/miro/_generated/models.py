# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetBoardInput selects a board by its non-secret ID.
@dataclass(frozen=True, slots=True)
class GetBoardInput:
    # BoardID is the ID after /board/ in a Miro board URL.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})


setattr(GetBoardInput, '__module__', 'dex_connectors.miro.models')


# BoardMetadata excludes the board description, policy, owner, and members.
@dataclass(frozen=True, slots=True)
class BoardMetadata:
    # ID is the Miro board ID.
    id: str = ''
    # Name is the board title.
    name: str = ''


setattr(BoardMetadata, '__module__', 'dex_connectors.miro.models')
