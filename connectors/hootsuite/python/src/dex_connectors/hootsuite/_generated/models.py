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


# ListSocialProfilesInput requests profiles visible to the connected user.
@dataclass(frozen=True, slots=True)
class ListSocialProfilesInput:
    pass


setattr(ListSocialProfilesInput, '__module__', 'dex_connectors.hootsuite.models')


# SocialProfile contains one visible publishing channel.
@dataclass(frozen=True, slots=True)
class SocialProfile:
    # ID is Hootsuite's stable social profile ID.
    id: str = ''
    # Type is the connected social network, such as FACEBOOK.
    type: str = ''
    # Username is the account label shown by Hootsuite.
    username: str = ''


setattr(SocialProfile, '__module__', 'dex_connectors.hootsuite.models')


# SocialProfilesPage contains the bounded list returned by Hootsuite.
@dataclass(frozen=True, slots=True)
class SocialProfilesPage:
    # Profiles are the connected user's visible social profiles.
    profiles: tuple[SocialProfile, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'profiles', _replace_nil_slices(self.profiles, 1))


SocialProfilesPage.__annotations__['profiles'] = tuple[SocialProfile, ...] | None
SocialProfilesPage.__dataclass_fields__['profiles'].type = tuple[SocialProfile, ...] | None
setattr(SocialProfilesPage, '__module__', 'dex_connectors.hootsuite.models')


# ListScheduledPostsInput selects one page of scheduled messages in a UTC interval.
@dataclass(frozen=True, slots=True)
class ListScheduledPostsInput:
    # StartTime is the inclusive UTC instant, formatted as RFC 3339 with a Z suffix.
    start_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'startTime'})
    # EndTime is the exclusive UTC instant, no later than four weeks after StartTime.
    end_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'endTime'})
    # SocialProfileID optionally filters to one ID returned by listSocialProfiles.
    social_profile_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'socialProfileId', 'dex_json_omit_default': True})
    # Cursor is the opaque cursor from a prior page; empty selects the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListScheduledPostsInput, '__module__', 'dex_connectors.hootsuite.models')


# ScheduledPost identifies a scheduled outbound message without returning its text.
@dataclass(frozen=True, slots=True)
class ScheduledPost:
    # ID is Hootsuite's stable message ID.
    id: str = ''
    # State is the provider's outbound message state.
    state: str = ''
    # SocialProfileID is the destination social profile.
    social_profile_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'socialProfileId'})
    # ScheduledSendTime is the UTC send instant.
    scheduled_send_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledSendTime'})


setattr(ScheduledPost, '__module__', 'dex_connectors.hootsuite.models')


# ScheduledPostsPage contains at most 100 scheduled messages and a continuation cursor.
@dataclass(frozen=True, slots=True)
class ScheduledPostsPage:
    # Posts contains the scheduled messages in provider order.
    posts: tuple[ScheduledPost, ...] = ()
    # NextCursor is empty when there is no next page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'posts', _replace_nil_slices(self.posts, 1))


ScheduledPostsPage.__annotations__['posts'] = tuple[ScheduledPost, ...] | None
ScheduledPostsPage.__dataclass_fields__['posts'].type = tuple[ScheduledPost, ...] | None
setattr(ScheduledPostsPage, '__module__', 'dex_connectors.hootsuite.models')


# SchedulePostInput schedules one text message on one social profile.
@dataclass(frozen=True, slots=True)
class SchedulePostInput:
    # Text is the message body; Hootsuite applies network-specific length restrictions.
    text: str = ''
    # SocialProfileID is a stable ID returned by listSocialProfiles.
    social_profile_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'socialProfileId'})
    # ScheduledSendTime is an ISO-8601 UTC instant ending in Z.
    scheduled_send_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledSendTime'})


setattr(SchedulePostInput, '__module__', 'dex_connectors.hootsuite.models')


# ScheduledPostReceipt identifies a message accepted by Hootsuite.
@dataclass(frozen=True, slots=True)
class ScheduledPostReceipt:
    # ID is Hootsuite's stable message ID.
    id: str = ''
    # State is the state Hootsuite assigned to the message.
    state: str = ''
    # SocialProfileID is the requested destination profile.
    social_profile_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'socialProfileId'})


setattr(ScheduledPostReceipt, '__module__', 'dex_connectors.hootsuite.models')
