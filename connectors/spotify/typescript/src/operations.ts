// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { ListPlaylistTracksDefinition } from './generated/definitions.js';
import type { ListPlaylistTracksInput, PlaylistAlbum, PlaylistArtist, PlaylistTrack, PlaylistTrackPage } from './generated/models.js';
import type { SpotifyClient, SpotifyResponse } from './client.js';

const emptyPage: PlaylistTrackPage = { playlistId: '', tracks: [], skippedItems: 0, limit: 0, offset: 0, total: 0 };
const spotifyId = /^[A-Za-z0-9]{22}$/;
const textEncoder = new TextEncoder();

function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'spotify', operation: 'listPlaylistTracks', message }; }
function invalid(message: string): QueryAttempt<PlaylistTrackPage> { return newQueryBranch('defect', emptyPage, zeroReceipt(), failure('VALIDATION', message)); }
function stringField(object: Record<string, unknown>, key: string, fallback = ''): string {
  const value = object[key];
  if (value === undefined || value === null) return fallback;
  if (typeof value !== 'string') throw new TypeError('invalid Spotify string');
  return value;
}
function integerField(object: Record<string, unknown>, key: string): number {
  const value = object[key];
  if (value === undefined || value === null) return 0;
  const literal = value && typeof value === 'object' && 'rawJSON' in value ? String(value.rawJSON) : '';
  if (!/^-?(0|[1-9][0-9]*)$/.test(literal)) throw new TypeError('invalid Spotify integer');
  const number = Number(literal);
  if (!Number.isSafeInteger(number)) throw new TypeError('invalid Spotify integer');
  return number;
}
function booleanField(object: Record<string, unknown>, key: string): boolean {
  const value = object[key];
  if (value === undefined || value === null) return false;
  if (typeof value !== 'boolean') throw new TypeError('invalid Spotify boolean');
  return value;
}
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid Spotify object');
  return value as Record<string, unknown>;
}
function metadata(value: string, required = false): string {
  if ((required && !value) || textEncoder.encode(value).length > 4096 || /\p{Cc}/u.test(value)) throw new TypeError('invalid Spotify metadata');
  return value;
}
function identifier(value: string): string {
  if (textEncoder.encode(value).length > 128 || /\p{Cc}/u.test(value)) throw new TypeError('invalid Spotify identifier');
  return value;
}
function spotifyURL(value: string): string {
  if (!value) return '';
  if (textEncoder.encode(value).length > 2048) throw new TypeError('invalid Spotify URL');
  const parsed = new URL(value);
  if (parsed.protocol !== 'https:' || parsed.hostname.toLowerCase() !== 'open.spotify.com' || parsed.username || parsed.password) throw new TypeError('invalid Spotify URL');
  return parsed.toString();
}
function spotifyURI(value: string): string {
  if (value && (textEncoder.encode(value).length > 2048 || !value.startsWith('spotify:') || /\p{Cc}/u.test(value))) throw new TypeError('invalid Spotify URI');
  return value;
}
function spotifyTimestamp(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new TypeError('invalid Spotify added_at');
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  const offset = match[8] ?? 'Z';
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59 ||
      (offset !== 'Z' && Number(offset.slice(4)) > 59)) throw new TypeError('invalid Spotify added_at');
  const fraction = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  return `${value.slice(0, 19)}${fraction ? `.${fraction}` : ''}${offset}`;
}
function artists(value: unknown): PlaylistArtist[] {
  if (!Array.isArray(value)) throw new TypeError('missing Spotify artists');
  return value.map(item => {
    const artist = record(item);
    const id = stringField(artist, 'id');
    if (id && !spotifyId.test(id)) throw new TypeError('invalid Spotify artist ID');
    const result: PlaylistArtist = { name: metadata(stringField(artist, 'name'), true) };
    const uri = spotifyURI(stringField(artist, 'uri'));
    const url = spotifyURL(stringField(record(artist.external_urls ?? {}), 'spotify'));
    if (id) result.id = id;
    if (uri) result.uri = uri;
    if (url) result.spotifyUrl = url;
    return result;
  });
}
function album(value: unknown): PlaylistAlbum {
  const source = record(value);
  const id = stringField(source, 'id');
  if (id && !spotifyId.test(id)) throw new TypeError('invalid Spotify album ID');
  const result: PlaylistAlbum = { name: metadata(stringField(source, 'name'), true), artists: artists(source.artists) };
  const uri = spotifyURI(stringField(source, 'uri'));
  const url = spotifyURL(stringField(record(source.external_urls ?? {}), 'spotify'));
  const releaseDate = metadata(stringField(source, 'release_date'));
  const precision = stringField(source, 'release_date_precision');
  if (precision && !['year', 'month', 'day'].includes(precision)) throw new TypeError('invalid Spotify release precision');
  if (id) result.id = id;
  if (uri) result.uri = uri;
  if (url) result.spotifyUrl = url;
  if (releaseDate) result.releaseDate = releaseDate;
  if (precision) result.releaseDatePrecision = precision;
  return result;
}
function track(item: unknown, position: number): PlaylistTrack | undefined {
  const source = record(item);
  if (source.item === null || source.item === undefined) return undefined;
  const provider = record(source.item);
  if (stringField(provider, 'type') !== 'track') return undefined;
  const isLocal = booleanField(source, 'is_local') || booleanField(provider, 'is_local');
  const id = stringField(provider, 'id');
  if ((!isLocal && !id) || (id && !spotifyId.test(id))) throw new TypeError('invalid Spotify track ID');
  const duration = integerField(provider, 'duration_ms');
  const disc = integerField(provider, 'disc_number');
  const number = integerField(provider, 'track_number');
  if (duration < 0 || disc < 0 || number < 0) throw new TypeError('invalid Spotify track');
  const result: PlaylistTrack = {
    position, isLocal, name: metadata(stringField(provider, 'name'), true), artists: artists(provider.artists), album: album(provider.album),
    durationMilliseconds: duration, explicit: booleanField(provider, 'explicit'), discNumber: disc, trackNumber: number,
  };
  const uri = spotifyURI(stringField(provider, 'uri'));
  const url = spotifyURL(stringField(record(provider.external_urls ?? {}), 'spotify'));
  if (id) result.id = id;
  if (uri) result.uri = uri;
  if (url) result.spotifyUrl = url;
  if (source.added_by !== null && source.added_by !== undefined) {
    const userId = identifier(stringField(record(source.added_by), 'id'));
    if (userId) result.addedByUserId = userId;
  }
  if (source.added_at !== null && source.added_at !== undefined) {
    const value = stringField(source, 'added_at');
    result.addedAt = spotifyTimestamp(value);
  }
  if (provider.is_playable !== null && provider.is_playable !== undefined) result.isPlayable = booleanField(provider, 'is_playable');
  return result;
}
function decodePage(body: Uint8Array, playlistId: string): PlaylistTrackPage {
  const rawJSON = (JSON as typeof JSON & { rawJSON(source: string): unknown }).rawJSON;
  const parseWithSource = JSON.parse as (text: string, reviver: (key: string, value: unknown, context: { source?: string }) => unknown) => unknown;
  const provider = record(parseWithSource(new TextDecoder('utf-8', { fatal: true }).decode(body), (_key, value, context) => typeof value === 'number' ? rawJSON(context.source!) : value));
  const limit = integerField(provider, 'limit');
  const offset = integerField(provider, 'offset');
  const total = integerField(provider, 'total');
  if (!Array.isArray(provider.items) || limit < 1 || limit > 50 || offset < 0 || total < 0 || provider.items.length > limit) throw new TypeError('invalid Spotify page');
  const tracks: PlaylistTrack[] = [];
  let skippedItems = 0;
  provider.items.forEach((item, index) => { const converted = track(item, offset + index); if (converted) tracks.push(converted); else skippedItems++; });
  const page: PlaylistTrackPage = { playlistId, tracks, skippedItems, limit, offset, total };
  if (provider.next !== null && provider.next !== undefined) {
    if (typeof provider.next !== 'string' || !provider.next || !provider.items.length || offset > 2147483647 - provider.items.length) throw new TypeError('invalid Spotify next page');
    page.nextOffset = offset + provider.items.length;
  }
  return page;
}
function responseHeader(response: SpotifyResponse, name: string): string {
  const value = response.headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

/** Invoke the Spotify playlist query and match Go's branches and retries. */
export class ListPlaylistTracksOperation implements Query<ListPlaylistTracksInput, PlaylistTrackPage> {
  /** Bind a client. */ constructor(private readonly client: SpotifyClient) {}
  /** Return the manifest definition. */ definition() { return ListPlaylistTracksDefinition; }
  /** Read one bounded playlist page. */
  async invoke(call: Call, input: ListPlaylistTracksInput): Promise<QueryAttempt<PlaylistTrackPage>> {
    const playlistId = typeof input.playlistId === 'string' ? input.playlistId.trim() : '';
    const market = typeof input.market === 'string' ? input.market.trim().toUpperCase() : '';
    const limit = input.limit || 20;
    const offset = input.offset || 0;
    if (!spotifyId.test(playlistId)) return invalid('playlist ID must be a 22-character Spotify ID');
    if (market && !/^[A-Z]{2}$/.test(market)) return invalid('market must be an ISO 3166-1 alpha-2 country code');
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > 50) return invalid('limit must be between 1 and 50');
    if (!Number.isSafeInteger(offset) || offset < 0 || offset > 2147483647) return invalid('offset must be between 0 and 2147483647');
    const token = await this.client.resolveAccessToken(call);
    if (!token) return newQueryBranch('defect', emptyPage, zeroReceipt(), failure('AUTHENTICATION', 'Spotify authorization is unavailable or revoked'));
    let response: SpotifyResponse;
    try { response = await this.client.requestPage(call, token, playlistId, market, limit, offset); }
    catch { return newQueryRetry(failure('AVAILABILITY', 'Spotify is unavailable')); }
    if (response.isTooLarge) return newQueryBranch('invalidResponse', emptyPage, zeroReceipt(), failure('RESPONSE_TOO_LARGE', 'Spotify playlist response exceeds the configured size limit'));
    if (response.status === 429) {
      const delay = parseRetryAfter(responseHeader(response, 'retry-after')) || 60_000;
      return newQueryRetry(failure('RATE_LIMIT', 'Spotify rate limit was reached'), delay);
    }
    if (response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Spotify is unavailable'));
    if (response.status === 401) return newQueryBranch('authorizationRevoked', emptyPage, zeroReceipt(), failure('AUTHENTICATION', 'Spotify authorization is invalid or revoked'));
    if (response.status === 403) return newQueryBranch('accessDenied', emptyPage, zeroReceipt(), failure('AUTHORIZATION', 'Spotify denied access to the playlist items'));
    if (response.status === 404) return newQueryBranch('notFound', emptyPage, zeroReceipt(), failure('NOT_FOUND', 'Spotify playlist was not found'));
    if (response.status < 200 || response.status >= 300) return newQueryBranch('providerRejected', emptyPage, zeroReceipt(), failure('PROVIDER_REJECTION', 'Spotify rejected the playlist query'));
    try { return newQueryBranch('listed', decodePage(response.body, playlistId), zeroReceipt()); }
    catch { return newQueryBranch('invalidResponse', emptyPage, zeroReceipt(), failure('PROTOCOL', 'Spotify returned an invalid playlist response')); }
  }
}
