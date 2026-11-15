// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Parser } from 'htmlparser2';
import { type GoURL, parseGoURL } from './go_url.js';
import { boundedText, collapseWhitespace, goTrimSpace, maxTitleCharacters } from './text.js';

const maxLinkElements = 200;
const maxMetaElements = 400;
const maxStructuredScripts = 20;
const maxStructuredBytes = 256 << 10;

/** One link element of a page. */
export interface HTMLLinkElement { relations: string[]; mediaType: string; href: string; title: string }

/** What one pass over the page prefix collects. */
export interface HTMLDocumentHead {
  baseHref: string;
  title: string;
  links: HTMLLinkElement[];
  metaByName: Map<string, string>;
  structuredScripts: string[];
}

/** Tokenize the page prefix once; the first value of each element wins, even in the body. */
export function scanHTMLHead(body: Uint8Array, contentType: string): HTMLDocumentHead {
  const head: HTMLDocumentHead = { baseHref: '', title: '', links: [], metaByName: new Map(), structuredScripts: [] };
  let foreignDepth = 0;
  let capture: 'title' | 'script' | undefined;
  let captured = '';
  const parser = new Parser({
    onopentag(name, attributes) {
      if (capture) finishCapture();
      switch (name) {
        case 'svg': case 'math': foreignDepth++; break;
        case 'base': if (head.baseHref === '') head.baseHref = attributes.href ?? ''; break;
        case 'title': if (foreignDepth === 0 && head.title === '') { capture = 'title'; captured = ''; } break;
        case 'link':
          if (head.links.length < maxLinkElements) {
            head.links.push({
              relations: collapseWhitespace((attributes.rel ?? '').toLowerCase()).split(' ').filter(Boolean),
              mediaType: goTrimSpace((attributes.type ?? '').split(';', 1)[0] ?? '').toLowerCase(),
              href: attributes.href ?? '', title: attributes.title ?? '',
            });
          }
          break;
        case 'meta': recordMeta(head, attributes); break;
        case 'script':
          if (goTrimSpace((attributes.type ?? '').split(';', 1)[0] ?? '').toLowerCase() === 'application/ld+json' && head.structuredScripts.length < maxStructuredScripts) {
            capture = 'script'; captured = '';
          }
          break;
      }
    },
    ontext(text) { if (capture) captured += text; },
    onclosetag(name) {
      if (capture) finishCapture();
      if ((name === 'svg' || name === 'math') && foreignDepth > 0) foreignDepth--;
    },
  }, { decodeEntities: true });
  function finishCapture(): void {
    if (capture === 'title') { if (captured !== '') head.title = boundedText(captured, maxTitleCharacters); }
    else if (capture === 'script' && Buffer.byteLength(captured, 'utf8') <= maxStructuredBytes) head.structuredScripts.push(captured);
    capture = undefined;
  }
  parser.end(decodeHTMLBytes(body, contentType));
  return head;
}

function recordMeta(head: HTMLDocumentHead, attributes: Record<string, string>): void {
  if (head.metaByName.size >= maxMetaElements) return;
  const content = attributes.content ?? '';
  for (const attributeName of ['property', 'name', 'itemprop']) {
    const key = goTrimSpace((attributes[attributeName] ?? '').toLowerCase());
    if (key === '' || content === '') continue;
    if (!head.metaByName.has(key)) head.metaByName.set(key, content);
  }
}

/** Apply the page's base element, when it holds a usable URL, to the URL that served the page. */
export function headBaseURL(head: HTMLDocumentHead, pageURL: GoURL): GoURL {
  if (head.baseHref === '') return pageURL;
  let parsed: GoURL;
  try { parsed = parseGoURL(goTrimSpace(head.baseHref)); } catch { return pageURL; }
  const resolved = pageURL.resolveReference(parsed);
  return resolved.scheme === 'http' || resolved.scheme === 'https' ? resolved : pageURL;
}

/** Whether rel lists the relation. */
export function hasRelation(relations: string[], relation: string): boolean { return relations.includes(relation); }

/** Accept HTML pages and a missing media type. */
export function isHTMLMediaType(mediaType: string): boolean {
  return mediaType === '' || mediaType === 'text/html' || mediaType === 'application/xhtml+xml';
}

// Decode as golang.org/x/net/html/charset does: BOM, HTTP charset, meta prescan, UTF-8 detection, windows-1252.
function decodeHTMLBytes(body: Uint8Array, contentType: string): string {
  return new TextDecoder(determineHTMLEncoding(body.subarray(0, 1024), contentType)).decode(body);
}

function determineHTMLEncoding(preview: Uint8Array, contentType: string): string {
  if (preview[0] === 0xef && preview[1] === 0xbb && preview[2] === 0xbf) return 'utf-8';
  if (preview[0] === 0xfe && preview[1] === 0xff) return 'utf-16be';
  if (preview[0] === 0xff && preview[1] === 0xfe) return 'utf-16le';
  const httpCharset = /;\s*charset\s*=\s*"?([^";\s]+)"?/i.exec(contentType)?.[1];
  if (httpCharset && isKnownEncoding(httpCharset)) return httpCharset;
  const prescanned = prescanMetaCharset(Buffer.from(preview).toString('latin1'));
  if (prescanned && isKnownEncoding(prescanned)) return /^utf-16/i.test(prescanned) ? 'utf-8' : prescanned;
  let content = preview;
  for (let index = content.length - 1; index >= 0 && index > content.length - 4; index--) {
    const byte = content[index]!;
    if (byte < 0x80) break;
    if ((byte & 0xc0) !== 0x80) { content = content.subarray(0, index); break; }
  }
  if (content.some(byte => byte >= 0x80)) {
    try { new TextDecoder('utf-8', { fatal: true }).decode(content); return 'utf-8'; } catch { /* Not UTF-8; use the default below. */ }
  }
  return 'windows-1252';
}

function prescanMetaCharset(text: string): string | undefined {
  const withoutComments = text.replace(/<!--[\s\S]*?-->/g, '');
  for (const match of withoutComments.matchAll(/<meta\b([^>]*)>?/gi)) {
    const attributes = new Map<string, string>();
    for (const attribute of (match[1] ?? '').matchAll(/([^\s=/>]+)\s*(?:=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g)) {
      const name = attribute[1]!.toLowerCase();
      if (!attributes.has(name)) attributes.set(name, attribute[2] ?? attribute[3] ?? attribute[4] ?? '');
    }
    const charset = attributes.get('charset');
    if (charset) return charset.trim();
    if (attributes.get('http-equiv')?.toLowerCase() === 'content-type') {
      const fromContent = /charset\s*=\s*["']?([^"';\s]+)/i.exec(attributes.get('content') ?? '')?.[1];
      if (fromContent) return fromContent;
    }
  }
  return undefined;
}

function isKnownEncoding(label: string): boolean {
  try { new TextDecoder(label); return true; } catch { return false; }
}
