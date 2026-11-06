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

CONNECTOR_ID = "spotify"
PROVIDER = "spotify"
LIST_PLAYLIST_TRACKS_OPERATION_ID = "listPlaylistTracks"
ACCESS_DENIED = "accessDenied"
AUTHORIZATION_REVOKED = "authorizationRevoked"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_PLAYLIST_TRACKS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PLAYLIST_TRACKS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One bounded page of playlist tracks was loaded.", False),
        BranchDefinition(AUTHORIZATION_REVOKED, "The Spotify OAuth grant is invalid or revoked.", True),
        BranchDefinition(ACCESS_DENIED, "The authenticated user cannot access the playlist items.", True),
        BranchDefinition(NOT_FOUND, "The requested Spotify playlist was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "Spotify conclusively rejected the playlist query.", True),
        BranchDefinition(INVALID_RESPONSE, "Spotify returned an invalid or oversized playlist response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
