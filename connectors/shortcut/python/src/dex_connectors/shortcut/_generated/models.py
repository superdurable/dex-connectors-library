# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetStoryInput selects a Story by its positive decimal public ID.
@dataclass(frozen=True, slots=True)
class GetStoryInput:
    # StoryID is the non-secret decimal ID from a Shortcut Story URL.
    story_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'storyId'})


setattr(GetStoryInput, '__module__', 'dex_connectors.shortcut.models')


# StoryMetadata excludes descriptions, comments, owners, and other private fields.
@dataclass(frozen=True, slots=True)
class StoryMetadata:
    # StoryID is the public decimal Story ID.
    story_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'storyId'})
    # Name is the Story title.
    name: str = ''
    # StoryType is Shortcut's feature, bug, or chore classification.
    story_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'storyType'})
    # WorkflowStateID is the decimal ID of the current workflow state.
    workflow_state_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workflowStateId'})
    # Archived reports whether the Story is archived.
    archived: bool = False


setattr(StoryMetadata, '__module__', 'dex_connectors.shortcut.models')
