// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ListSitesInput, SitesPage, ListDeploysInput, DeploysPage, Site, Deploy } from './generated/models.js';
import { ListSitesDefinition, ListDeploysDefinition } from './generated/definitions.js';
import { emptySites, emptyDeploys, type NetlifyClient } from './client.js';

/** List sites visible to a Netlify token. */
export class ListSitesOperation implements Query<ListSitesInput, SitesPage> {
  /** Bind a validated client. */ constructor(private readonly client: NetlifyClient) {}
  /** Return the manifest operation definition. */ definition() { return ListSitesDefinition; }
  /** Read and decode one provider page. */
  invoke(call: Call, input: ListSitesInput): Promise<QueryAttempt<SitesPage>> {
    void input;
    return this.client.query(call, 'listSites', '/sites', emptySites, false, body => ({ sites: decodeArray(body, decodeSite) }));
  }
}
/** List deploys for a Netlify site. */
export class ListDeploysOperation implements Query<ListDeploysInput, DeploysPage> {
  /** Bind a validated client. */ constructor(private readonly client: NetlifyClient) {}
  /** Return the manifest operation definition. */ definition() { return ListDeploysDefinition; }
  /** Validate the site and read its deploy page. */
  invoke(call: Call, input: ListDeploysInput): Promise<QueryAttempt<DeploysPage>> {
    if (!input.siteId?.trim()) return Promise.resolve(newQueryValidation());
    return this.client.query(call, 'listDeploys', `/sites/${goPathEscape(input.siteId)}/deploys`, emptyDeploys, true, body => ({ deploys: decodeArray(body, decodeDeploy) }));
  }
}
function newQueryValidation(): QueryAttempt<DeploysPage> {
  const failure = { kind: 'VALIDATION' as const, provider: 'netlify', operation: 'listDeploys', message: 'siteId is required' };
  return newQueryBranch('defect', emptyDeploys, {}, failure);
}
function goPathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%(?:24|26|2B|3A|3D|40)/g, escape => String.fromCharCode(Number.parseInt(escape.slice(1), 16))); }
function decodeArray<Item>(body: Uint8Array, decode: (value: unknown) => Item): Item[] {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!Array.isArray(document)) throw new Error('expected array');
  return document.map(decode);
}
function object(value: unknown): Record<string, unknown> {
  if (value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object');
  return value as Record<string, unknown>;
}
function field(value: Record<string, unknown>, name: string): string {
  const selected = value[name];
  if (selected === undefined || selected === null) return '';
  if (typeof selected !== 'string') throw new Error('expected string');
  return selected;
}
function decodeSite(value: unknown): Site {
  const source = object(value);
  return { id: field(source, 'id'), name: field(source, 'name'), url: field(source, 'url'), admin_url: field(source, 'admin_url') };
}
function decodeDeploy(value: unknown): Deploy {
  const source = object(value);
  return { id: field(source, 'id'), site_id: field(source, 'site_id'), state: field(source, 'state'), url: field(source, 'url') };
}
