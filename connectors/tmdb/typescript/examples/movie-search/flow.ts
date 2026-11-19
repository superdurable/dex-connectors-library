// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchMoviesStep, type Connection, type MoviePage, type SearchMoviesInput } from '../../src/index.js';

/** Static Dex Web connection name. */
export const ConnectionName = 'tmdb';
/** Retained safe movie search result. */
export const moviePage = new Attribute<MoviePage>('tmdb-typescript-movie-page', jsonCodec<MoviePage>());

/** Search one page and complete with its movie results. */
export class MovieSearchFlow implements Flow<SearchMoviesInput> {
  private readonly prepare = new PrepareMovieSearch();
  private readonly complete = new CompleteMovieSearch();

  /** Bind a named provider or scripted connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('TMDB connection name differs from the example');
  }

  /** Stable Flow identity. */ getFlowType(): string { return 'TMDBTypeScriptMovieSearch'; }
  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<SearchMoviesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSearchMoviesStep({ stepType: 'SearchTMDBMovies', annotations: { groupId: 'tmdb', groupLabel: 'TMDB', explanation: 'Search TMDB movie titles.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, searched: this.complete }), this.complete);
  }
  /** Register the persisted result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [moviePage] }; }
}

// dex:group group-id:tmdb group-label:"TMDB"
// dex:explanation text:"Send a validated movie title search to the TMDB connector."
class PrepareMovieSearch implements Step<SearchMoviesInput> {
  getStepType(): string { return 'PrepareTMDBMovieSearch'; }
  execute(_context: Context, input: SearchMoviesInput): StepDecision { return goTo(new StepRef('SearchTMDBMovies'), input); }
}

// dex:group group-id:tmdb group-label:"TMDB"
// dex:explanation text:"Persist the matching movie page and complete the Flow."
class CompleteMovieSearch implements Step<QueryResult<MoviePage>> {
  getStepType(): string { return 'CompleteTMDBMovieSearch'; }
  execute(context: Context, result: QueryResult<MoviePage>): StepDecision { moviePage.set(context, result.value); return gracefulComplete(result.value); }
}
