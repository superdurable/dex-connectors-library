// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListPlaylistTracksStep, type Connection, type ListPlaylistTracksInput, type PlaylistTrackPage } from '@superdurable/connector-spotify';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'spotify-playlist-reader';
/** Safe page selected by this Flow. */
export const playlistPage = new Attribute<PlaylistTrackPage>('spotify-playlist-page', jsonCodec<PlaylistTrackPage>());

/** Read one bounded playlist page through the Spotify connector. */
export class PlaylistTracksFlow implements Flow<ListPlaylistTracksInput> {
  private readonly prepare = new PreparePlaylistTracks();
  private readonly complete = new CompletePlaylistTracks();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'SpotifyPlaylistTracksFlow'; }
  /** Register the playlist query and completion Steps. */
  getSteps(): StepList<ListPlaylistTracksInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListPlaylistTracksStep({
        stepType: 'ListSpotifyPlaylistTracks',
        annotations: { groupId: 'spotify-playlist', groupLabel: 'Spotify Playlist', explanation: 'Read one bounded page of playlist tracks.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted provider output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [playlistPage] }; }
}

// dex:group group-id:spotify-playlist group-label:"Spotify Playlist"
// dex:explanation text:"Validate the playlist ID before invoking Spotify."
/** Validate a playlist selection before a provider call. */
export class PreparePlaylistTracks implements Step<ListPlaylistTracksInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareSpotifyPlaylistTracks'; }
  /** Start the provider query with nonsecret input. */
  execute(_context: Context, input: ListPlaylistTracksInput): StepDecision {
    if (!/^[A-Za-z0-9]{22}$/.test(input.playlistId)) return forceFail('Spotify playlist ID is invalid');
    return goTo(new StepRef('ListSpotifyPlaylistTracks'), input);
  }
}

// dex:group group-id:spotify-playlist group-label:"Spotify Playlist"
// dex:explanation text:"Persist the bounded track page and complete the Flow."
/** Complete the Flow with a bounded playlist page. */
export class CompletePlaylistTracks implements Step<QueryResult<PlaylistTrackPage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteSpotifyPlaylistTracks'; }
  /** Keep only safe provider metadata. */
  execute(context: Context, result: QueryResult<PlaylistTrackPage>): StepDecision {
    playlistPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
