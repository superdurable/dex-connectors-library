# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddPlaylistItemInput, AddedPlaylistItem, Channel, GetChannelInput, GetVideoInput, ListPlaylistItemsInput, PlaylistItem, PlaylistItemPage, SearchVideosInput, Video, VideoSearchPage, VideoSearchResult
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_PLAYLIST_ITEM_DEFINITION, DEFECT, FOUND, GET_CHANNEL_DEFINITION, GET_VIDEO_DEFINITION, INVALID_RESPONSE, LISTED, LIST_PLAYLIST_ITEMS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, QUOTA_EXHAUSTED, SEARCHED, SEARCH_VIDEOS_DEFINITION

def search_videos_searched(value: VideoSearchPage, failure: Failure | None = None) -> Case[VideoSearchPage]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_videos_searched, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_quota_exhausted(value: VideoSearchPage, failure: Failure | None = None) -> Case[VideoSearchPage]:
    return Case.branch_result(QUOTA_EXHAUSTED, value, failure)

setattr(search_videos_quota_exhausted, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_provider_rejected(value: VideoSearchPage, failure: Failure | None = None) -> Case[VideoSearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_videos_provider_rejected, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_invalid_response(value: VideoSearchPage, failure: Failure | None = None) -> Case[VideoSearchPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_videos_invalid_response, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_defect(value: VideoSearchPage, failure: Failure | None = None) -> Case[VideoSearchPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_videos_defect, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_retry(failure: Failure, after_seconds: float = 0) -> Case[VideoSearchPage]:
    return Case.retry(failure, after_seconds)

setattr(search_videos_retry, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_matching_compiler_video() -> Case[VideoSearchPage]:
    return Case.paged(SEARCHED, (VideoSearchPage(results=(VideoSearchResult(video_id='AbCdEfGhI12', channel_id='UC1234567890123456789012', channel_title='Compiler Lab', title='Compiler release walkthrough', description='A short compiler tour', published_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00'))),), next_page_token='compiler-search-2', total_results=2), VideoSearchPage(results=(VideoSearchResult(video_id='AbCdEfGhI12', channel_id='UC1234567890123456789012', channel_title='Compiler Lab', title='Compiler release walkthrough', description='A short compiler tour', published_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00'))), VideoSearchResult(video_id='ZxCvBnMaS34', channel_id='UC1234567890123456789012', channel_title='Compiler Lab', title='Compiler setup tips', description='A setup guide', published_at=datetime.fromisoformat("2026-10-10T10:00:00Z".replace('Z', '+00:00')))), next_page_token='compiler-search-3', total_results=2), VideoSearchPage(results=(), total_results=2)))

setattr(search_videos_matching_compiler_video, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_quota_exhausted_failure() -> Case[VideoSearchPage]:
    return Case.branch_result(QUOTA_EXHAUSTED, VideoSearchPage(), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "searchVideos", "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time."))

setattr(search_videos_quota_exhausted_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def search_videos_provider_rejected_failure() -> Case[VideoSearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, VideoSearchPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchVideos", "YouTube denied the request."))

setattr(search_videos_provider_rejected_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_found(value: Video, failure: Failure | None = None) -> Case[Video]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_video_found, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_not_found(value: Video, failure: Failure | None = None) -> Case[Video]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_video_not_found, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_quota_exhausted(value: Video, failure: Failure | None = None) -> Case[Video]:
    return Case.branch_result(QUOTA_EXHAUSTED, value, failure)

setattr(get_video_quota_exhausted, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_provider_rejected(value: Video, failure: Failure | None = None) -> Case[Video]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_video_provider_rejected, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_invalid_response(value: Video, failure: Failure | None = None) -> Case[Video]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_video_invalid_response, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_defect(value: Video, failure: Failure | None = None) -> Case[Video]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_video_defect, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_retry(failure: Failure, after_seconds: float = 0) -> Case[Video]:
    return Case.retry(failure, after_seconds)

setattr(get_video_retry, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_compiler_video() -> Case[Video]:
    return Case.branch_result(FOUND, Video(id='AbCdEfGhI12', channel_id='UC1234567890123456789012', channel_title='Compiler Lab', title='Compiler release walkthrough', description='A short compiler tour', published_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00')), duration='PT4M13S', privacy_status='public', view_count=1200))

setattr(get_video_compiler_video, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_not_found_failure() -> Case[Video]:
    return Case.branch_result(NOT_FOUND, Video(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getVideo", "No visible video has the ID, such as a deleted or private video."))

setattr(get_video_not_found_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_quota_exhausted_failure() -> Case[Video]:
    return Case.branch_result(QUOTA_EXHAUSTED, Video(), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "getVideo", "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time."))

setattr(get_video_quota_exhausted_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_video_provider_rejected_failure() -> Case[Video]:
    return Case.branch_result(PROVIDER_REJECTED, Video(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getVideo", "YouTube denied the request."))

setattr(get_video_provider_rejected_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_found(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_channel_found, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_not_found(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_channel_not_found, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_quota_exhausted(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(QUOTA_EXHAUSTED, value, failure)

setattr(get_channel_quota_exhausted, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_provider_rejected(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_channel_provider_rejected, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_invalid_response(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_channel_invalid_response, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_defect(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_channel_defect, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_retry(failure: Failure, after_seconds: float = 0) -> Case[Channel]:
    return Case.retry(failure, after_seconds)

setattr(get_channel_retry, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_compiler_channel() -> Case[Channel]:
    return Case.branch_result(FOUND, Channel(id='UC1234567890123456789012', title='Compiler Lab', description='Compiler tutorials', custom_url='@CompilerLab', published_at=datetime.fromisoformat("2020-01-01T00:00:00Z".replace('Z', '+00:00')), uploads_playlist_id='UU1234567890123456789012', video_count=42))

setattr(get_channel_compiler_channel, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_not_found_failure() -> Case[Channel]:
    return Case.branch_result(NOT_FOUND, Channel(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getChannel", "No channel has the ID or handle, or the authorized account has no channel."))

setattr(get_channel_not_found_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_quota_exhausted_failure() -> Case[Channel]:
    return Case.branch_result(QUOTA_EXHAUSTED, Channel(), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "getChannel", "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time."))

setattr(get_channel_quota_exhausted_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def get_channel_provider_rejected_failure() -> Case[Channel]:
    return Case.branch_result(PROVIDER_REJECTED, Channel(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getChannel", "YouTube denied the request."))

setattr(get_channel_provider_rejected_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_listed(value: PlaylistItemPage, failure: Failure | None = None) -> Case[PlaylistItemPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_playlist_items_listed, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_not_found(value: PlaylistItemPage, failure: Failure | None = None) -> Case[PlaylistItemPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_playlist_items_not_found, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_quota_exhausted(value: PlaylistItemPage, failure: Failure | None = None) -> Case[PlaylistItemPage]:
    return Case.branch_result(QUOTA_EXHAUSTED, value, failure)

setattr(list_playlist_items_quota_exhausted, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_provider_rejected(value: PlaylistItemPage, failure: Failure | None = None) -> Case[PlaylistItemPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_playlist_items_provider_rejected, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_invalid_response(value: PlaylistItemPage, failure: Failure | None = None) -> Case[PlaylistItemPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_playlist_items_invalid_response, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_defect(value: PlaylistItemPage, failure: Failure | None = None) -> Case[PlaylistItemPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_playlist_items_defect, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_retry(failure: Failure, after_seconds: float = 0) -> Case[PlaylistItemPage]:
    return Case.retry(failure, after_seconds)

setattr(list_playlist_items_retry, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_curated_video() -> Case[PlaylistItemPage]:
    return Case.paged(LISTED, (PlaylistItemPage(items=(PlaylistItem(id='PLITEM001', playlist_id='PLcurated123', video_id='AbCdEfGhI12', title='Compiler release walkthrough', position=0, added_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00')), video_published_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00'))),), next_page_token='curated-items-2', total_results=1), PlaylistItemPage(items=(PlaylistItem(id='PLITEM001', playlist_id='PLcurated123', video_id='AbCdEfGhI12', title='Compiler release walkthrough', position=0, added_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00')), video_published_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00'))),), next_page_token='curated-items-3', total_results=1), PlaylistItemPage(items=(), total_results=1)))

setattr(list_playlist_items_curated_video, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_not_found_failure() -> Case[PlaylistItemPage]:
    return Case.branch_result(NOT_FOUND, PlaylistItemPage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listPlaylistItems", "No playlist visible to the account has the ID."))

setattr(list_playlist_items_not_found_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_quota_exhausted_failure() -> Case[PlaylistItemPage]:
    return Case.branch_result(QUOTA_EXHAUSTED, PlaylistItemPage(), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "listPlaylistItems", "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time."))

setattr(list_playlist_items_quota_exhausted_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def list_playlist_items_provider_rejected_failure() -> Case[PlaylistItemPage]:
    return Case.branch_result(PROVIDER_REJECTED, PlaylistItemPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listPlaylistItems", "YouTube denied the request."))

setattr(list_playlist_items_provider_rejected_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_added(value: AddedPlaylistItem, failure: Failure | None = None) -> Case[AddedPlaylistItem]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_playlist_item_added, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_not_found(value: AddedPlaylistItem, failure: Failure | None = None) -> Case[AddedPlaylistItem]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_playlist_item_not_found, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_quota_exhausted(value: AddedPlaylistItem, failure: Failure | None = None) -> Case[AddedPlaylistItem]:
    return Case.branch_result(QUOTA_EXHAUSTED, value, failure)

setattr(add_playlist_item_quota_exhausted, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_provider_rejected(value: AddedPlaylistItem, failure: Failure | None = None) -> Case[AddedPlaylistItem]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_playlist_item_provider_rejected, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_invalid_response(value: AddedPlaylistItem, failure: Failure | None = None) -> Case[AddedPlaylistItem]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(add_playlist_item_invalid_response, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_defect(value: AddedPlaylistItem, failure: Failure | None = None) -> Case[AddedPlaylistItem]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_playlist_item_defect, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_retry(failure: Failure, after_seconds: float = 0) -> Case[AddedPlaylistItem]:
    return Case.retry(failure, after_seconds)

setattr(add_playlist_item_retry, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_compiler_video_added() -> Case[AddedPlaylistItem]:
    return Case.branch_result(ADDED, AddedPlaylistItem(item=PlaylistItem(id='PLITEM001', playlist_id='PLcurated123', video_id='AbCdEfGhI12', title='Compiler release walkthrough', position=0, added_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00')), video_published_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00'))), was_already_present=False))

setattr(add_playlist_item_compiler_video_added, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_not_found_failure() -> Case[AddedPlaylistItem]:
    return Case.branch_result(NOT_FOUND, AddedPlaylistItem(item=PlaylistItem()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addPlaylistItem", "No playlist the account can manage or no visible video has the ID."))

setattr(add_playlist_item_not_found_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_quota_exhausted_failure() -> Case[AddedPlaylistItem]:
    return Case.branch_result(QUOTA_EXHAUSTED, AddedPlaylistItem(item=PlaylistItem()), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "addPlaylistItem", "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time."))

setattr(add_playlist_item_quota_exhausted_failure, '__module__', "dex_connectors.google_youtube.youtubemock")

def add_playlist_item_provider_rejected_failure() -> Case[AddedPlaylistItem]:
    return Case.branch_result(PROVIDER_REJECTED, AddedPlaylistItem(item=PlaylistItem()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addPlaylistItem", "YouTube denied the request."))

setattr(add_playlist_item_provider_rejected_failure, '__module__', "dex_connectors.google_youtube.youtubemock")


class Mock:

    def __init__(self, connection_name: str = "google-youtube") -> None:
        self.search_videos_script: Query[SearchVideosInput, VideoSearchPage] = Query(SEARCH_VIDEOS_DEFINITION)
        self.search_videos_script.default(search_videos_matching_compiler_video())
        self.get_video_script: Query[GetVideoInput, Video] = Query(GET_VIDEO_DEFINITION)
        self.get_video_script.default(get_video_compiler_video())
        self.get_channel_script: Query[GetChannelInput, Channel] = Query(GET_CHANNEL_DEFINITION)
        self.get_channel_script.default(get_channel_compiler_channel())
        self.list_playlist_items_script: Query[ListPlaylistItemsInput, PlaylistItemPage] = Query(LIST_PLAYLIST_ITEMS_DEFINITION)
        self.list_playlist_items_script.default(list_playlist_items_curated_video())
        self.add_playlist_item_script: Mutation[AddPlaylistItemInput, AddedPlaylistItem] = Mutation(ADD_PLAYLIST_ITEM_DEFINITION)
        self.add_playlist_item_script.default(add_playlist_item_compiler_video_added())
        self._connection = new_connection_with_operations(
            search_videos=self.search_videos_script,
            get_video=self.get_video_script,
            get_channel=self.get_channel_script,
            list_playlist_items=self.list_playlist_items_script,
            add_playlist_item=self.add_playlist_item_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_youtube.youtubemock")

__all__ = [
    "Mock",
    "search_videos_searched",
    "search_videos_quota_exhausted",
    "search_videos_provider_rejected",
    "search_videos_invalid_response",
    "search_videos_defect",
    "search_videos_retry",
    "search_videos_matching_compiler_video",
    "search_videos_quota_exhausted_failure",
    "search_videos_provider_rejected_failure",
    "get_video_found",
    "get_video_not_found",
    "get_video_quota_exhausted",
    "get_video_provider_rejected",
    "get_video_invalid_response",
    "get_video_defect",
    "get_video_retry",
    "get_video_compiler_video",
    "get_video_not_found_failure",
    "get_video_quota_exhausted_failure",
    "get_video_provider_rejected_failure",
    "get_channel_found",
    "get_channel_not_found",
    "get_channel_quota_exhausted",
    "get_channel_provider_rejected",
    "get_channel_invalid_response",
    "get_channel_defect",
    "get_channel_retry",
    "get_channel_compiler_channel",
    "get_channel_not_found_failure",
    "get_channel_quota_exhausted_failure",
    "get_channel_provider_rejected_failure",
    "list_playlist_items_listed",
    "list_playlist_items_not_found",
    "list_playlist_items_quota_exhausted",
    "list_playlist_items_provider_rejected",
    "list_playlist_items_invalid_response",
    "list_playlist_items_defect",
    "list_playlist_items_retry",
    "list_playlist_items_curated_video",
    "list_playlist_items_not_found_failure",
    "list_playlist_items_quota_exhausted_failure",
    "list_playlist_items_provider_rejected_failure",
    "add_playlist_item_added",
    "add_playlist_item_not_found",
    "add_playlist_item_quota_exhausted",
    "add_playlist_item_provider_rejected",
    "add_playlist_item_invalid_response",
    "add_playlist_item_defect",
    "add_playlist_item_retry",
    "add_playlist_item_compiler_video_added",
    "add_playlist_item_not_found_failure",
    "add_playlist_item_quota_exhausted_failure",
    "add_playlist_item_provider_rejected_failure",
]
