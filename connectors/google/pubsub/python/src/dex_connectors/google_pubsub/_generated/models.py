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


# ListTopicsInput selects one bounded page of topics in a Google Cloud project.
@dataclass(frozen=True, slots=True)
class ListTopicsInput:
    # ProjectID is the Google Cloud project ID shown in Pub/Sub Topics.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    # PageSize limits this page to 1 through 100 topics; zero uses 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is returned by the previous page; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListTopicsInput, '__module__', 'dex_connectors.google_pubsub.models')


# Topic identifies a Pub/Sub topic by its full resource name.
@dataclass(frozen=True, slots=True)
class Topic:
    # Name has the form projects/{project-id}/topics/{topic-id}.
    name: str = ''


setattr(Topic, '__module__', 'dex_connectors.google_pubsub.models')


# TopicsPage holds one page of topic names and its next cursor.
@dataclass(frozen=True, slots=True)
class TopicsPage:
    # Topics are visible to the authorized Google identity.
    topics: tuple[Topic, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'topics', _replace_nil_slices(self.topics, 1))


TopicsPage.__annotations__['topics'] = tuple[Topic, ...] | None
TopicsPage.__dataclass_fields__['topics'].type = tuple[Topic, ...] | None
setattr(TopicsPage, '__module__', 'dex_connectors.google_pubsub.models')
