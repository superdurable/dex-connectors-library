// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ExchangeFailure, TMDBClient } from './client.js';
import { GetMovieDefinition, SearchMoviesDefinition } from './generated/definitions.js';
import type { GetMovieInput, Movie, MoviePage, SearchMoviesInput } from './generated/models.js';

const emptyMovie: Movie = { id: 0, title: '', overview: '', release_date: '' };
const emptyPage: MoviePage = { page: 0, total_pages: 0, total_results: 0, results: [] };

/** Search TMDB movie titles. */
export class SearchMoviesOperation implements Query<SearchMoviesInput, MoviePage> {
  /** Bind a validated client. */ constructor(private readonly client: TMDBClient) {}
  /** Return the generated operation definition. */ definition() { return SearchMoviesDefinition; }
  /** Validate and execute one search attempt. */
  async invoke(call: Call, input: SearchMoviesInput): Promise<QueryAttempt<MoviePage>> {
    if (!input.query.trim() || !Number.isSafeInteger(input.page) || input.page < 0 || input.page > 500) return failed('searchMovies', 'defect', 'VALIDATION', emptyPage, 'query is required and page must be 1–500');
    const query = input.page > 0 ? `page=${input.page}&query=${queryEscape(input.query)}` : `query=${queryEscape(input.query)}`;
    const result = await this.client.exchange(call, 'searchMovies', `/search/movie?${query}`, false);
    if ('outcome' in result) return fromFailure(result.outcome, emptyPage);
    try {
      const page = decodePage(result.response.body);
      if (page.results === null || page.page <= 0) throw new TypeError('movie page incomplete');
      return newQueryBranch('searched', page, {});
    } catch { return failed('searchMovies', 'invalidResponse', 'PROTOCOL', emptyPage, 'provider response is incomplete or malformed'); }
  }
}

/** Read one TMDB movie by ID. */
export class GetMovieOperation implements Query<GetMovieInput, Movie> {
  /** Bind a validated client. */ constructor(private readonly client: TMDBClient) {}
  /** Return the generated operation definition. */ definition() { return GetMovieDefinition; }
  /** Validate and execute one movie lookup attempt. */
  async invoke(call: Call, input: GetMovieInput): Promise<QueryAttempt<Movie>> {
    if (!Number.isSafeInteger(input.id) || input.id <= 0) return failed('getMovie', 'defect', 'VALIDATION', emptyMovie, 'movie ID must be positive');
    const result = await this.client.exchange(call, 'getMovie', `/movie/${input.id}`, true);
    if ('outcome' in result) return fromFailure(result.outcome, emptyMovie);
    try {
      const movie = decodeMovie(JSON.parse(Buffer.from(result.response.body).toString('utf8')) as unknown);
      if (movie.id !== input.id) throw new TypeError('movie ID mismatch');
      return newQueryBranch('found', movie, {});
    } catch { return failed('getMovie', 'invalidResponse', 'PROTOCOL', emptyMovie, 'provider response is incomplete or malformed'); }
  }
}

function fromFailure<T>(outcome: ExchangeFailure, zero: T): QueryAttempt<T> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.disposition, zero, {}, outcome.failure);
}

function failed<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, message: string): QueryAttempt<T> {
  return newQueryBranch(branch, zero, {}, { provider: 'tmdb', operation, kind, message });
}

function queryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/~/g, '%7E').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }

function decodePage(body: Uint8Array): MoviePage {
  const raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown;
  const object = asObject(raw);
  return { page: intField(object, 'page'), total_pages: intField(object, 'total_pages'), total_results: intField(object, 'total_results'), results: object.results === undefined || object.results === null ? null : Array.isArray(object.results) ? object.results.map(decodeMovie) : invalid() };
}

function decodeMovie(raw: unknown): Movie {
  const object = asObject(raw);
  return { id: intField(object, 'id'), title: stringField(object, 'title'), overview: stringField(object, 'overview'), release_date: stringField(object, 'release_date') };
}

function asObject(raw: unknown): Record<string, unknown> { if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return invalid(); return raw as Record<string, unknown>; }
function intField(object: Record<string, unknown>, key: string): number { const value = object[key]; if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) return invalid(); return value; }
function stringField(object: Record<string, unknown>, key: string): string { const value = object[key]; if (value === undefined || value === null) return ''; if (typeof value !== 'string') return invalid(); return value; }
function invalid(): never { throw new TypeError('invalid provider response'); }
