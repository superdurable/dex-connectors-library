# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "google-youtube"
PROVIDER = "google"
SEARCH_VIDEOS_OPERATION_ID = "searchVideos"
GET_VIDEO_OPERATION_ID = "getVideo"
GET_CHANNEL_OPERATION_ID = "getChannel"
LIST_PLAYLIST_ITEMS_OPERATION_ID = "listPlaylistItems"
ADD_PLAYLIST_ITEM_OPERATION_ID = "addPlaylistItem"
ADDED = "added"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
QUOTA_EXHAUSTED = "quotaExhausted"
SEARCHED = "searched"

SEARCH_VIDEOS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_VIDEOS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "YouTube returned one page of matching videos; the page may be empty.", False),
        BranchDefinition(QUOTA_EXHAUSTED, "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time.", True),
        BranchDefinition(PROVIDER_REJECTED, "YouTube conclusively rejected the search, such as an invalid parameter, a missing scope, or a revoked grant.", True),
        BranchDefinition(INVALID_RESPONSE, "YouTube returned an invalid or oversized search response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_VIDEO_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_VIDEO_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The video exists and the account can see it.", False),
        BranchDefinition(NOT_FOUND, "No visible video has the ID, such as a deleted or private video.", True),
        BranchDefinition(QUOTA_EXHAUSTED, "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time.", True),
        BranchDefinition(PROVIDER_REJECTED, "YouTube conclusively rejected the read, such as a missing scope or a revoked grant.", True),
        BranchDefinition(INVALID_RESPONSE, "YouTube returned an invalid or oversized video response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CHANNEL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CHANNEL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The channel exists and was returned.", False),
        BranchDefinition(NOT_FOUND, "No channel has the ID or handle, or the authorized account has no channel.", True),
        BranchDefinition(QUOTA_EXHAUSTED, "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time.", True),
        BranchDefinition(PROVIDER_REJECTED, "YouTube conclusively rejected the read, such as a missing scope or a revoked grant.", True),
        BranchDefinition(INVALID_RESPONSE, "YouTube returned an invalid or oversized channel response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_PLAYLIST_ITEMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PLAYLIST_ITEMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "YouTube returned one page of playlist items; the page may be empty.", False),
        BranchDefinition(NOT_FOUND, "No playlist visible to the account has the ID.", True),
        BranchDefinition(QUOTA_EXHAUSTED, "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time.", True),
        BranchDefinition(PROVIDER_REJECTED, "YouTube conclusively rejected the listing, such as a private playlist of another account or a revoked grant.", True),
        BranchDefinition(INVALID_RESPONSE, "YouTube returned an invalid or oversized playlist page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_PLAYLIST_ITEM_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_PLAYLIST_ITEM_OPERATION_ID),
    (
        BranchDefinition(ADDED, "The playlist holds the video, added now or found already present.", False),
        BranchDefinition(NOT_FOUND, "No playlist the account can manage or no visible video has the ID.", True),
        BranchDefinition(QUOTA_EXHAUSTED, "The project's daily YouTube Data API quota is spent; it resets at midnight Pacific Time.", True),
        BranchDefinition(PROVIDER_REJECTED, "YouTube conclusively rejected the insert, such as a playlist of another account, a full playlist, or a revoked grant.", True),
        BranchDefinition(INVALID_RESPONSE, "YouTube returned an invalid or oversized response to the check or the insert.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
