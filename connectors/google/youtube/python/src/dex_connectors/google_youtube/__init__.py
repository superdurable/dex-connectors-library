# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""YouTube Data API connector for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddedPlaylistItem,
    AddPlaylistItemInput,
    Channel,
    GetChannelInput,
    GetVideoInput,
    ListPlaylistItemsInput,
    PlaylistItem,
    PlaylistItemPage,
    SearchVideosInput,
    Video,
    VideoSearchPage,
    VideoSearchResult,
)
from .steps import (
    new_add_playlist_item_step,
    new_get_channel_step,
    new_get_video_step,
    new_list_playlist_items_step,
    new_search_videos_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "AddPlaylistItemInput",
    "AddedPlaylistItem",
    "Channel",
    "GetChannelInput",
    "GetVideoInput",
    "ListPlaylistItemsInput",
    "PlaylistItem",
    "PlaylistItemPage",
    "SearchVideosInput",
    "Video",
    "VideoSearchPage",
    "VideoSearchResult",
    "new_add_playlist_item_step",
    "new_get_channel_step",
    "new_get_video_step",
    "new_list_playlist_items_step",
    "new_search_videos_step",
]
