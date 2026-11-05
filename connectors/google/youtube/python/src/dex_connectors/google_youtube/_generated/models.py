# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchVideosInput selects one page of public video search results.
# Query or ChannelID must be set.
@dataclass(frozen=True, slots=True)
class SearchVideosInput:
    # Query is the keyword search, up to 500 characters; YouTube supports - to exclude and | to match either term.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ChannelID limits results to one channel, such as UC followed by 22 characters.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId', 'dex_json_omit_default': True})
    # Order is one of SearchOrders; blank uses YouTube's relevance order.
    order: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PublishedAfter limits results to videos published after this time; nil applies no limit.
    published_after: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'publishedAfter', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # MaxResults is the page size, 1 to 50; zero uses YouTube's default of 5.
    max_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxResults', 'dex_json_omit_default': True})
    # PageToken continues from the previous page's NextPageToken; blank starts at the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(SearchVideosInput, '__module__', 'dex_connectors.google_youtube.models')


# VideoSearchResult is one matching video. Title and Description are untrusted provider text.
@dataclass(frozen=True, slots=True)
class VideoSearchResult:
    # VideoID is the 11-character video ID.
    video_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'videoId'})
    # ChannelID is the publishing channel's ID.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ChannelTitle is the publishing channel's display name.
    channel_title: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelTitle'})
    # Title is the video title, which YouTube HTML-escapes in search results.
    title: str = ''
    # Description is a truncated description.
    description: str = ''
    # PublishedAt is when the video was published, in UTC.
    published_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'publishedAt', 'dex_json_format': 'rfc3339'})


setattr(VideoSearchResult, '__module__', 'dex_connectors.google_youtube.models')


# VideoSearchPage is one page of search results in the requested order.
@dataclass(frozen=True, slots=True)
class VideoSearchPage:
    # Results holds this page, possibly empty.
    results: tuple[VideoSearchResult, ...] = ()
    # NextPageToken continues the search; blank means the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # TotalResults is YouTube's approximate total, which may exceed the results it serves.
    total_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalResults'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'results', _replace_nil_slices(self.results, 1))


VideoSearchPage.__annotations__['results'] = tuple[VideoSearchResult, ...] | None
VideoSearchPage.__dataclass_fields__['results'].type = tuple[VideoSearchResult, ...] | None
setattr(VideoSearchPage, '__module__', 'dex_connectors.google_youtube.models')


# GetVideoInput identifies one video.
@dataclass(frozen=True, slots=True)
class GetVideoInput:
    # VideoID is the 11-character video ID, such as dQw4w9WgXcQ from youtube.com/watch?v=dQw4w9WgXcQ.
    video_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'videoId'})


setattr(GetVideoInput, '__module__', 'dex_connectors.google_youtube.models')


# Video is one video's details. Title and Description are untrusted provider
# text: escape them before rendering HTML.
@dataclass(frozen=True, slots=True)
class Video:
    # ID is the 11-character video ID.
    id: str = ''
    # ChannelID is the ID of the channel that published the video.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ChannelTitle is that channel's display name.
    channel_title: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelTitle'})
    # Title is the video title.
    title: str = ''
    # Description is the video description.
    description: str = ''
    # PublishedAt is when the video was published, in UTC.
    published_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'publishedAt', 'dex_json_format': 'rfc3339'})
    # Duration is the ISO 8601 duration, such as PT4M13S; blank for an upcoming live stream.
    duration: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PrivacyStatus is public, unlisted, or private.
    privacy_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'privacyStatus'})
    # ViewCount is the view count, or nil when YouTube does not report it.
    view_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'viewCount', 'dex_json_omit_default': True})
    # LikeCount is the like count, or nil when the owner hides it.
    like_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'likeCount', 'dex_json_omit_default': True})
    # CommentCount is the comment count, or nil when comments are disabled.
    comment_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'commentCount', 'dex_json_omit_default': True})


setattr(Video, '__module__', 'dex_connectors.google_youtube.models')


# GetChannelInput selects one channel. Set exactly one field.
@dataclass(frozen=True, slots=True)
class GetChannelInput:
    # ChannelID is the channel ID, UC followed by 22 characters, from youtube.com/channel/<ID>.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId', 'dex_json_omit_default': True})
    # Handle is the channel handle with or without @, such as @GoogleDevelopers.
    handle: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Mine reads the authorized account's own channel.
    mine: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(GetChannelInput, '__module__', 'dex_connectors.google_youtube.models')


# Channel is one channel's details. Title and Description are untrusted provider text.
@dataclass(frozen=True, slots=True)
class Channel:
    # ID is the channel ID.
    id: str = ''
    # Title is the channel's display name.
    title: str = ''
    # Description is the channel description.
    description: str = ''
    # CustomURL is the channel's handle, such as @googledevelopers; blank when it has none.
    custom_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'customUrl', 'dex_json_omit_default': True})
    # PublishedAt is when the channel was created, in UTC.
    published_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'publishedAt', 'dex_json_format': 'rfc3339'})
    # UploadsPlaylistID lists the channel's public uploads, newest first, for ListPlaylistItems.
    uploads_playlist_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'uploadsPlaylistId'})
    # SubscriberCount is the rounded subscriber count, or nil when the owner hides it.
    subscriber_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'subscriberCount', 'dex_json_omit_default': True})
    # VideoCount is the number of public videos, or nil when YouTube does not report it.
    video_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'videoCount', 'dex_json_omit_default': True})
    # ViewCount is the channel's total views, or nil when YouTube does not report it.
    view_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'viewCount', 'dex_json_omit_default': True})


setattr(Channel, '__module__', 'dex_connectors.google_youtube.models')


# ListPlaylistItemsInput selects one page of a playlist in playlist order.
@dataclass(frozen=True, slots=True)
class ListPlaylistItemsInput:
    # PlaylistID is the playlist ID from youtube.com/playlist?list=<ID>, or a channel's UploadsPlaylistID.
    playlist_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'playlistId'})
    # VideoID returns only items for this 11-character video ID; blank returns every item.
    video_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'videoId', 'dex_json_omit_default': True})
    # MaxResults is the page size, 1 to 50; zero uses YouTube's default of 5.
    max_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxResults', 'dex_json_omit_default': True})
    # PageToken continues from the previous page's NextPageToken; blank starts at the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListPlaylistItemsInput, '__module__', 'dex_connectors.google_youtube.models')


# PlaylistItem is one entry of a playlist. Title is untrusted provider text.
@dataclass(frozen=True, slots=True)
class PlaylistItem:
    # ID is the playlist item ID, distinct from the video ID.
    id: str = ''
    # PlaylistID is the playlist holding the item.
    playlist_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'playlistId'})
    # VideoID is the 11-character ID of the video.
    video_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'videoId'})
    # Title is the video title, or "Private video" or "Deleted video" for unavailable videos.
    title: str = ''
    # Position is the zero-based position in the playlist.
    position: int = 0
    # AddedAt is when the item was added to the playlist, in UTC.
    added_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'addedAt', 'dex_json_format': 'rfc3339'})
    # VideoPublishedAt is when the video was published, or nil when it is private or deleted.
    video_published_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'videoPublishedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(PlaylistItem, '__module__', 'dex_connectors.google_youtube.models')


# PlaylistItemPage is one page of playlist items.
@dataclass(frozen=True, slots=True)
class PlaylistItemPage:
    # Items holds this page in playlist position order, possibly empty.
    items: tuple[PlaylistItem, ...] = ()
    # NextPageToken continues the listing; blank means the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # TotalResults is the number of items in the playlist, or matching VideoID.
    total_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalResults'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


PlaylistItemPage.__annotations__['items'] = tuple[PlaylistItem, ...] | None
PlaylistItemPage.__dataclass_fields__['items'].type = tuple[PlaylistItem, ...] | None
setattr(PlaylistItemPage, '__module__', 'dex_connectors.google_youtube.models')


# AddPlaylistItemInput names the video to add to the end of a playlist the account manages.
@dataclass(frozen=True, slots=True)
class AddPlaylistItemInput:
    # PlaylistID is the playlist ID from youtube.com/playlist?list=<ID>; the authorized account must own it.
    playlist_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'playlistId'})
    # VideoID is the 11-character ID of the video to add.
    video_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'videoId'})


setattr(AddPlaylistItemInput, '__module__', 'dex_connectors.google_youtube.models')


# AddedPlaylistItem is the playlist entry that holds the video.
@dataclass(frozen=True, slots=True)
class AddedPlaylistItem:
    # Item is the new entry, or the existing entry when WasAlreadyPresent is true.
    item: PlaylistItem = _dataclass_field(default_factory=PlaylistItem)
    # WasAlreadyPresent reports that the playlist already held the video, so nothing was inserted.
    was_already_present: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyPresent'})


setattr(AddedPlaylistItem, '__module__', 'dex_connectors.google_youtube.models')
