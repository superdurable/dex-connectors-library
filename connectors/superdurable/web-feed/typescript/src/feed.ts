// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { decodeXMLDocument, type FetchFailure, invalidResponse } from './document.js';
import { type GoURL, parseGoURL } from './go_url.js';
import type { Feed, FeedItem } from './generated/models.js';
import {
  appendBoundedValue, boundedIdentifier, boundedText, collapseWhitespace, digestIdentifier, firstNonEmpty, goTrimSpace,
  maxDescriptionCharacters, maxNameCharacters, maxTitleCharacters, plainTextFromEscapedTitle, plainTextFromHTML,
  resolveLink, truncateCharacters,
} from './text.js';
import { optionalTimestamp } from './timestamp.js';
import { decodeXMLNode, namespaceXML, readRootElement, XMLNode, XMLSyntaxError } from './xml.js';

const namespaceAtom = 'http://www.w3.org/2005/Atom';
const namespaceAtom03 = 'http://purl.org/atom/ns#';
const namespaceRSS1 = 'http://purl.org/rss/1.0/';
const namespaceRSS090 = 'http://my.netscape.com/rdf/simple/0.9/';
const namespaceRDF = 'http://www.w3.org/1999/02/22-rdf-syntax-ns#';
const namespaceDublin = 'http://purl.org/dc/elements/1.1/';
const namespaceContent = 'http://purl.org/rss/1.0/modules/content/';

/** Per-call bounds and the URL that served the document. */
export interface FeedParseOptions { limit: number; maxSummaryCharacters: number; documentURL: string }

/** A parsed feed, or the failure that invalidates the document. */
export type FeedParseResult = { feed: Feed } | { failure: FetchFailure };

/** Detect JSON Feed by its first byte and parse every other document as XML. */
export function parseFeedDocument(body: Uint8Array, httpCharset: string, options: FeedParseOptions): FeedParseResult {
  let start = 0;
  if (body[0] === 0xef && body[1] === 0xbb && body[2] === 0xbf) start = 3;
  while (start < body.length && [0x20, 0x09, 0x0d, 0x0a].includes(body[start]!)) start++;
  const trimmed = body.subarray(start);
  if (trimmed.length === 0) return { failure: invalidResponse('the feed document is empty') };
  if (trimmed[0] === 0x7b) return parseJSONFeed(trimmed, options);
  return parseXMLFeed(trimmed, httpCharset, options);
}

function parseXMLFeed(body: Uint8Array, httpCharset: string, options: FeedParseOptions): FeedParseResult {
  let root: XMLNode;
  try {
    const tokenizer = decodeXMLDocument(body, httpCharset);
    let start;
    try { start = readRootElement(tokenizer); } catch { return { failure: invalidResponse('the document is neither XML nor a JSON Feed') }; }
    if (start.name.local.toLowerCase() === 'html') return { failure: invalidResponse('the URL serves an HTML page, not a feed; find its feeds with discoverFeeds') };
    if (!['rss', 'RDF', 'feed'].includes(start.name.local)) return { failure: invalidResponse('the XML document is not an RSS, RDF, or Atom feed') };
    root = decodeXMLNode(tokenizer, start);
  } catch (error) {
    if (error instanceof XMLSyntaxError || error instanceof TypeError || error instanceof RangeError) {
      return { failure: invalidResponse('the feed XML is malformed or nested too deeply') };
    }
    throw error;
  }
  let documentURL: GoURL;
  try { documentURL = parseGoURL(options.documentURL); } catch { return { failure: invalidResponse('the feed URL cannot be used as a base URL') }; }
  if (root.name.local === 'rss') return { feed: parseRSS(root, documentURL, options) };
  if (root.name.local === 'RDF') return { feed: parseRDF(root, documentURL, options) };
  if (root.name.space !== namespaceAtom && root.name.space !== namespaceAtom03) return { failure: invalidResponse('the feed element is not in the Atom namespace') };
  return { feed: parseAtom(root, documentURL, options) };
}

function parseRSS(root: XMLNode, documentURL: GoURL, options: FeedParseOptions): Feed {
  // RSS has no namespace, but some RSS 2.0 feeds declare a default one; core elements share the root's.
  const space = root.name.space;
  const channel = root.firstChild(space, 'channel') ?? emptyNode();
  const base = baseURL(channel, baseURL(root, documentURL));
  const feed = newFeed('rss', {
    title: plainTextFromEscapedTitle(channel.childText(space, 'title')),
    siteUrl: resolveLink(base, channel.childText(space, 'link')),
    description: boundedText(plainTextFromHTML(channel.childText(space, 'description')), maxDescriptionCharacters),
    updatedAt: optionalTimestamp(firstNonEmpty(channel.childText(space, 'lastBuildDate'), channel.childText(space, 'pubDate'), channel.childText(namespaceDublin, 'date'))),
  });
  let itemNodes = channel.childrenNamed(space, 'item');
  if (itemNodes.length === 0) itemNodes = root.childrenNamed(space, 'item');
  const items = itemNodes.slice(0, options.limit).map(node => parseRSSItem(node, space, base, options));
  return withItems(feed, items, itemNodes.length, options.limit);
}

function parseRSSItem(node: XMLNode, space: string, inheritedBase: GoURL | undefined, options: FeedParseOptions): FeedItem {
  const base = baseURL(node, inheritedBase);
  const item: Partial<FeedItem> = {
    title: plainTextFromEscapedTitle(node.childText(space, 'title')),
    link: resolveLink(base, node.childText(space, 'link')),
    publishedAt: optionalTimestamp(firstNonEmpty(node.childText(space, 'pubDate'), node.childText(namespaceDublin, 'date'))),
    updatedAt: optionalTimestamp(node.childText(namespaceAtom, 'updated')),
  };
  const guid = node.firstChild(space, 'guid');
  if (guid) {
    item.id = goTrimSpace(guid.text());
    const isPermaLink = goTrimSpace(guid.attribute('', 'isPermaLink')).toLowerCase() !== 'false';
    if (!item.link && isPermaLink) item.link = resolveLink(base, guid.text());
  }
  const summary = firstNonEmpty(node.childText(space, 'description'), node.childText(namespaceContent, 'encoded'));
  item.summary = truncateCharacters(plainTextFromHTML(summary), options.maxSummaryCharacters);
  for (const author of node.childrenNamed(space, 'author')) item.authors = appendBoundedValue(item.authors, rssAuthorName(author.text()), maxNameCharacters);
  for (const creator of node.childrenNamed(namespaceDublin, 'creator')) item.authors = appendBoundedValue(item.authors, creator.text(), maxNameCharacters);
  for (const category of node.childrenNamed(space, 'category')) item.categories = appendBoundedValue(item.categories, category.text(), maxNameCharacters);
  for (const subject of node.childrenNamed(namespaceDublin, 'subject')) item.categories = appendBoundedValue(item.categories, subject.text(), maxNameCharacters);
  return finishFeedItem(item);
}

/** Read the "address (Name)" and "Name <address>" forms RSS uses, keeping the name when one is given. */
export function rssAuthorName(value: string): string {
  const trimmed = goTrimSpace(value);
  const angle = /^"?([^"<]*?)"?\s*<([^<>\s]+@[^<>\s]+)>$/.exec(trimmed);
  if (angle) return angle[1]!.trim() || angle[2]!;
  const comment = /^([^\s()<>@]+@[^\s()<>@]+)\s*\(([^()]*)\)$/.exec(trimmed);
  if (comment) return comment[2]!.trim() || comment[1]!;
  if (/^[^\s()<>@]+@[^\s()<>@]+\.[^\s()<>@]+$/.test(trimmed)) return trimmed;
  const open = trimmed.indexOf('(');
  const close = trimmed.lastIndexOf(')');
  if (open > 0 && close > open) return trimmed.slice(open + 1, close);
  return trimmed;
}

function parseRDF(root: XMLNode, documentURL: GoURL, options: FeedParseOptions): Feed {
  let space = namespaceRSS1;
  let channel = root.firstChild(space, 'channel');
  if (!channel) { space = namespaceRSS090; channel = root.firstChild(space, 'channel'); }
  channel ??= emptyNode();
  const base = baseURL(root, documentURL);
  const feed = newFeed('rdf', {
    title: plainTextFromEscapedTitle(channel.childText(space, 'title')),
    siteUrl: resolveLink(base, channel.childText(space, 'link')),
    description: boundedText(plainTextFromHTML(channel.childText(space, 'description')), maxDescriptionCharacters),
    updatedAt: optionalTimestamp(channel.childText(namespaceDublin, 'date')),
  });
  const itemNodes = root.childrenNamed(space, 'item');
  const items = itemNodes.slice(0, options.limit).map(node => {
    const item: Partial<FeedItem> = {
      id: node.attribute(namespaceRDF, 'about'),
      title: plainTextFromEscapedTitle(node.childText(space, 'title')),
      link: resolveLink(base, node.childText(space, 'link')),
      publishedAt: optionalTimestamp(node.childText(namespaceDublin, 'date')),
    };
    const summary = firstNonEmpty(node.childText(space, 'description'), node.childText(namespaceContent, 'encoded'));
    item.summary = truncateCharacters(plainTextFromHTML(summary), options.maxSummaryCharacters);
    for (const creator of node.childrenNamed(namespaceDublin, 'creator')) item.authors = appendBoundedValue(item.authors, creator.text(), maxNameCharacters);
    for (const subject of node.childrenNamed(namespaceDublin, 'subject')) item.categories = appendBoundedValue(item.categories, subject.text(), maxNameCharacters);
    return finishFeedItem(item);
  });
  return withItems(feed, items, itemNodes.length, options.limit);
}

function parseAtom(root: XMLNode, documentURL: GoURL, options: FeedParseOptions): Feed {
  const space = root.name.space;
  const base = baseURL(root, documentURL);
  const feed = newFeed('atom', {
    title: boundedText(atomText(root.firstChild(space, 'title')), maxTitleCharacters),
    siteUrl: atomAlternateLink(root, space, base),
    description: boundedText(atomText(root.firstChild(space, 'subtitle') ?? root.firstChild(space, 'tagline')), maxDescriptionCharacters),
    updatedAt: optionalTimestamp(firstNonEmpty(root.childText(space, 'updated'), root.childText(space, 'modified'))),
  });
  const feedAuthors = atomAuthors(root, space);
  const entryNodes = root.childrenNamed(space, 'entry');
  const items = entryNodes.slice(0, options.limit).map(node => {
    const entryBase = baseURL(node, base);
    const item: Partial<FeedItem> = {
      id: goTrimSpace(node.childText(space, 'id')),
      title: atomText(node.firstChild(space, 'title')),
      link: atomAlternateLink(node, space, entryBase),
      publishedAt: optionalTimestamp(firstNonEmpty(node.childText(space, 'published'), node.childText(space, 'issued'), node.childText(space, 'created'))),
      updatedAt: optionalTimestamp(firstNonEmpty(node.childText(space, 'updated'), node.childText(space, 'modified'))),
      authors: atomAuthors(node, space),
    };
    let summary = atomText(node.firstChild(space, 'summary'));
    if (summary === '') summary = atomText(node.firstChild(space, 'content'));
    item.summary = truncateCharacters(summary, options.maxSummaryCharacters);
    if (!item.authors?.length) item.authors = feedAuthors;
    for (const category of node.childrenNamed(space, 'category')) {
      item.categories = appendBoundedValue(item.categories, firstNonEmpty(category.attribute('', 'term'), category.attribute('', 'label')), maxNameCharacters);
    }
    return finishFeedItem(item);
  });
  return withItems(feed, items, entryNodes.length, options.limit);
}

/** Read an Atom text construct: text as is, html without markup, xhtml from its descendants in order. */
function atomText(node: XMLNode | undefined): string {
  if (!node) return '';
  switch (goTrimSpace(node.attribute('', 'type')).toLowerCase()) {
    case 'html': case 'text/html': case 'escaped': return plainTextFromHTML(node.text());
    case 'xhtml': case 'application/xhtml+xml': return collapseWhitespace(node.allText());
  }
  if (node.attribute('', 'mode') === 'escaped') return plainTextFromHTML(node.text());
  return collapseWhitespace(node.text());
}

/** Pick an entry's page: rel alternate or no rel, preferring an HTML type. */
function atomAlternateLink(node: XMLNode, space: string, base: GoURL | undefined): string {
  let fallback = '';
  for (const link of node.childrenNamed(space, 'link')) {
    const relation = goTrimSpace(link.attribute('', 'rel'));
    if (relation !== '' && relation !== 'alternate') continue;
    const href = resolveLink(baseURL(link, base), link.attribute('', 'href'));
    if (href === '') continue;
    const mediaType = link.attribute('', 'type').toLowerCase();
    if (mediaType === '' || mediaType === 'text/html' || mediaType === 'application/xhtml+xml') return href;
    if (fallback === '') fallback = href;
  }
  return fallback;
}

function atomAuthors(node: XMLNode, space: string): string[] | undefined {
  let authors: string[] | undefined;
  for (const author of node.childrenNamed(space, 'author')) {
    authors = appendBoundedValue(authors, firstNonEmpty(author.childText(space, 'name'), author.childText(space, 'email')), maxNameCharacters);
  }
  return authors;
}

/** Apply the node's xml:base to the inherited base URL, as the Go connector does. */
function baseURL(node: XMLNode, inherited: GoURL | undefined): GoURL | undefined {
  const value = goTrimSpace(node.attribute(namespaceXML, 'base'));
  if (value === '') return inherited;
  let parsed: GoURL;
  try { parsed = parseGoURL(value); } catch { return inherited; }
  return inherited ? inherited.resolveReference(parsed) : parsed;
}

interface JSONFeedAuthor { name: string; url: string }

/** Read a JSON Feed whose version names https://jsonfeed.org/version/1 or a 1.x revision. */
function parseJSONFeed(body: Uint8Array, options: FeedParseOptions): FeedParseResult {
  let document: unknown;
  try { document = JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(body)); } catch { return { failure: invalidResponse('the JSON document is not a valid JSON Feed') }; }
  const invalid = { failure: invalidResponse('the JSON document is not a valid JSON Feed') };
  if (!isObject(document)) return invalid;
  const fields = readFields(document, { version: 'string', title: 'string', home_page_url: 'string', description: 'string', author: 'object', authors: 'objects', items: 'objects' });
  if (!fields) return invalid;
  const version = String(fields.version ?? '').replace(/^https:\/\//, '').replace(/^http:\/\//, '');
  if (!version.startsWith('jsonfeed.org/version/1')) return { failure: invalidResponse('the JSON document does not declare a JSON Feed 1.x version') };
  let base: GoURL;
  try { base = parseGoURL(options.documentURL); } catch { return { failure: invalidResponse('the feed URL cannot be used as a base URL') }; }
  const feedAuthorObject = fields.author === undefined ? undefined : readAuthor(fields.author);
  const feedAuthorList = (fields.authors as unknown[] | undefined)?.map(readAuthor);
  if (feedAuthorObject === null || feedAuthorList?.includes(null)) return invalid;
  const feed = newFeed('json', {
    title: boundedText(String(fields.title ?? ''), maxTitleCharacters),
    siteUrl: resolveLink(base, String(fields.home_page_url ?? '')),
    description: boundedText(String(fields.description ?? ''), maxDescriptionCharacters),
    updatedAt: undefined,
  });
  const feedAuthors = jsonFeedAuthorNames(feedAuthorObject ?? undefined, feedAuthorList as JSONFeedAuthor[] | undefined);
  const rawItems = (fields.items as unknown[] | undefined) ?? [];
  const items: FeedItem[] = [];
  for (const rawItem of rawItems) {
    const source = readFields(rawItem, {
      url: 'string', external_url: 'string', title: 'string', content_html: 'string', content_text: 'string', summary: 'string',
      date_published: 'string', date_modified: 'string', author: 'object', authors: 'objects', tags: 'strings',
    });
    if (!source) return invalid;
    const author = source.author === undefined ? undefined : readAuthor(source.author);
    const authors = (source.authors as unknown[] | undefined)?.map(readAuthor);
    if (author === null || authors?.includes(null)) return invalid;
    if (items.length >= options.limit) continue;
    const item: Partial<FeedItem> = {
      id: jsonFeedIdentifier(isObject(rawItem) ? jsonField(rawItem, 'id') : undefined),
      title: String(source.title ?? ''),
      link: firstNonEmpty(resolveLink(base, String(source.url ?? '')), resolveLink(base, String(source.external_url ?? ''))),
      publishedAt: optionalTimestamp(String(source.date_published ?? '')),
      updatedAt: optionalTimestamp(String(source.date_modified ?? '')),
      authors: jsonFeedAuthorNames(author ?? undefined, authors as JSONFeedAuthor[] | undefined),
    };
    let summary = collapseWhitespace(firstNonEmpty(String(source.summary ?? ''), String(source.content_text ?? '')));
    if (summary === '') summary = plainTextFromHTML(String(source.content_html ?? ''));
    item.summary = truncateCharacters(summary, options.maxSummaryCharacters);
    if (!item.authors?.length) item.authors = feedAuthors;
    for (const tag of (source.tags as Array<string | null> | undefined) ?? []) item.categories = appendBoundedValue(item.categories, tag ?? '', maxNameCharacters);
    items.push(finishFeedItem(item));
  }
  return { feed: withItems(feed, items, rawItems.length, options.limit) };
}

type FieldKind = 'string' | 'object' | 'objects' | 'strings';

// Decode known fields like Go encoding/json: keys match case-insensitively, null leaves the zero value,
// and a value of another type fails the whole document.
function readFields(value: unknown, kinds: Record<string, FieldKind>): Record<string, unknown> | undefined {
  if (!isObject(value)) return undefined;
  const result: Record<string, unknown> = {};
  for (const [name, kind] of Object.entries(kinds)) {
    const field = jsonField(value, name);
    if (field === undefined || field === null) continue;
    switch (kind) {
      case 'string': if (typeof field !== 'string') return undefined; break;
      case 'object': if (!isObject(field)) return undefined; break;
      case 'objects': if (!Array.isArray(field) || field.some(element => element !== null && !isObject(element))) return undefined; break;
      case 'strings': if (!Array.isArray(field) || field.some(element => element !== null && typeof element !== 'string')) return undefined; break;
    }
    result[name] = field;
  }
  return result;
}

function jsonField(value: Record<string, unknown>, name: string): unknown {
  if (Object.hasOwn(value, name)) return value[name];
  let match: unknown;
  for (const [key, field] of Object.entries(value)) if (key.toLowerCase() === name.toLowerCase()) match = field;
  return match;
}

// Returns null for an author whose fields have the wrong types.
function readAuthor(value: unknown): JSONFeedAuthor | null {
  if (value === null) return { name: '', url: '' };
  const fields = readFields(value, { name: 'string', url: 'string' });
  return fields ? { name: String(fields.name ?? ''), url: String(fields.url ?? '') } : null;
}

function jsonFeedIdentifier(raw: unknown): string {
  if (typeof raw === 'string') return raw;
  if (typeof raw === 'number') return JSON.stringify(raw);
  return '';
}

/** Read JSON Feed 1.1 authors, falling back to the 1.0 author object. */
function jsonFeedAuthorNames(author: JSONFeedAuthor | undefined, authors: JSONFeedAuthor[] | undefined): string[] | undefined {
  const list = (authors?.length ?? 0) === 0 && author ? [author] : authors ?? [];
  let names: string[] | undefined;
  for (const each of list) names = appendBoundedValue(names, firstNonEmpty(each.name, each.url), maxNameCharacters);
  return names;
}

function newFeed(format: string, fields: { title: string; siteUrl: string; description: string; updatedAt: string | undefined }): Feed {
  const feed: Feed = { format, finalUrl: '', fetchedAt: '', items: [], itemCount: 0, hasMore: false };
  if (fields.title) feed.title = fields.title;
  if (fields.siteUrl) feed.siteUrl = fields.siteUrl;
  if (fields.description) feed.description = fields.description;
  if (fields.updatedAt) feed.updatedAt = fields.updatedAt;
  return feed;
}

function withItems(feed: Feed, items: FeedItem[], totalItemCount: number, limit: number): Feed {
  feed.items = items.slice(0, limit);
  feed.itemCount = totalItemCount;
  feed.hasMore = totalItemCount > feed.items.length;
  return feed;
}

/** Fill the fallback identity, bound the title, and drop empty optional fields as Go omitempty does. */
function finishFeedItem(item: Partial<FeedItem>): FeedItem {
  let id = boundedIdentifier(item.id ?? '');
  if (id === '') id = boundedIdentifier(item.link ?? '');
  if (id === '') id = digestIdentifier(item.title ?? '', item.publishedAt ?? '', item.updatedAt ?? '', item.summary ?? '');
  const finished: FeedItem = { id };
  const title = boundedText(item.title ?? '', maxTitleCharacters);
  if (title) finished.title = title;
  if (item.link) finished.link = item.link;
  if (item.publishedAt) finished.publishedAt = item.publishedAt;
  if (item.updatedAt) finished.updatedAt = item.updatedAt;
  if (item.summary) finished.summary = item.summary;
  if (item.authors?.length) finished.authors = item.authors;
  if (item.categories?.length) finished.categories = item.categories;
  return finished;
}

function emptyNode(): XMLNode { return new XMLNode({ space: '', local: '' }, []); }

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
