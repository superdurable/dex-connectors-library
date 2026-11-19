// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { decodeHTML } from 'entities';
import { Parser } from 'htmlparser2';
import { type GoURL, parseGoURL } from './go_url.js';

/** Bound on a title, in code points. */
export const maxTitleCharacters = 500;
/** Bound on a description, in code points. */
export const maxDescriptionCharacters = 1000;
/** Bound on an author, category, or short label, in code points. */
export const maxNameCharacters = 200;
/** Bound on list fields such as authors and categories. */
export const maxListValues = 20;
/** Bound on an identifier before it is replaced by its digest, in UTF-8 bytes. */
export const maxIdentifierBytes = 2048;
/** Bound on input, redirect, and output URLs, in UTF-8 bytes. */
export const maxURLBytes = 2048;

// The characters Go unicode.IsSpace accepts; JavaScript \s differs at U+0085 and U+FEFF.
const goWhitespaceClass = String.raw`[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]`;
const goWhitespace = new RegExp(`${goWhitespaceClass}+`, 'u');

const skippedElements = new Set(['script', 'style', 'noscript', 'template', 'svg']);
const blockElements = new Set([
  'address', 'article', 'aside', 'blockquote', 'br', 'dd', 'details', 'div', 'dl', 'dt', 'figcaption', 'figure', 'footer',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'header', 'hr', 'img', 'li', 'main', 'nav', 'ol', 'p', 'pre', 'section', 'summary',
  'table', 'td', 'th', 'tr', 'ul',
]);

/** Join the whitespace-separated words of value with single spaces, as Go strings.Fields. */
export function collapseWhitespace(value: string): string {
  return value.split(goWhitespace).filter(word => word !== '').join(' ');
}

/** Collapse whitespace and cut to at most maxCharacters code points. */
export function boundedText(value: string, maxCharacters: number): string {
  return truncateCharacters(collapseWhitespace(value), maxCharacters);
}

/** Cut value to at most maxCharacters code points at a late word boundary, ending a cut value with an ellipsis. */
export function truncateCharacters(value: string, maxCharacters: number): string {
  const codePoints = Array.from(value);
  if (maxCharacters <= 0 || codePoints.length <= maxCharacters) return value;
  let cut = Buffer.from(codePoints.slice(0, maxCharacters - 1).join(''), 'utf8');
  const lastSpace = cut.lastIndexOf(0x20);
  if (lastSpace > Math.floor(cut.length / 2)) cut = cut.subarray(0, lastSpace);
  return cut.toString('utf8').replace(/ +$/, '') + '…';
}

/** Return the visible text of an HTML fragment with entities decoded and script and style content dropped. */
export function plainTextFromHTML(fragment: string): string {
  if (!fragment.includes('<') && !fragment.includes('&')) return collapseWhitespace(fragment);
  let text = '';
  let skippedDepth = 0;
  const parser = new Parser({
    onopentag(name) {
      if (skippedElements.has(name)) skippedDepth++;
      else if (blockElements.has(name)) text += ' ';
    },
    onclosetag(name) {
      if (skippedElements.has(name)) skippedDepth = Math.max(skippedDepth - 1, 0);
      else if (blockElements.has(name)) text += ' ';
    },
    ontext(value) { if (skippedDepth === 0) text += value; },
  }, { decodeEntities: true });
  parser.end(fragment);
  return collapseWhitespace(text);
}

/** Decode the HTML entities that feeds often double-escape in plain-text titles. */
export function plainTextFromEscapedTitle(title: string): string {
  return boundedText(decodeHTML(title), maxTitleCharacters);
}

/** Resolve href against base and return it only when it is an absolute http or https URL within the bound. */
export function resolveLink(base: GoURL | undefined, href: string): string {
  const trimmed = href.trim();
  if (trimmed === '') return '';
  let parsed: GoURL;
  try { parsed = parseGoURL(trimmed); } catch { return ''; }
  if (base) parsed = base.resolveReference(parsed);
  if ((parsed.scheme !== 'http' && parsed.scheme !== 'https') || parsed.hostname() === '' || parsed.user !== undefined) return '';
  const resolved = parsed.toString();
  return Buffer.byteLength(resolved, 'utf8') > maxURLBytes ? '' : resolved;
}

/** Keep an identifier within the bound by replacing a longer one with its digest. */
export function boundedIdentifier(identifier: string): string {
  const trimmed = goTrimSpace(identifier);
  return Buffer.byteLength(trimmed, 'utf8') <= maxIdentifierBytes ? trimmed : digestIdentifier(trimmed);
}

/** Return a sha256: digest of the parts joined by NUL bytes. */
export function digestIdentifier(...parts: string[]): string {
  return 'sha256:' + createHash('sha256').update(parts.join('\u0000'), 'utf8').digest('hex');
}

/** Append a bounded, nonempty value that values does not already contain, keeping at most maxListValues. */
export function appendBoundedValue(values: string[] | undefined, value: string, maxCharacters: number): string[] | undefined {
  const bounded = boundedText(value, maxCharacters);
  if (bounded === '' || (values?.length ?? 0) >= maxListValues) return values;
  if (values?.includes(bounded)) return values;
  return [...(values ?? []), bounded];
}

/** Trim leading and trailing Go whitespace, as Go strings.TrimSpace. */
export function goTrimSpace(value: string): string {
  return value.replace(new RegExp(`^${goWhitespaceClass}+`, 'u'), '')
    .replace(new RegExp(`${goWhitespaceClass}+$`, 'u'), '');
}

/** Trim trailing Go whitespace, as Go strings.TrimRightFunc with unicode.IsSpace. */
export function goTrimRight(value: string): string {
  return value.replace(new RegExp(`${goWhitespaceClass}+$`, 'u'), '');
}

/** Return the first value that is nonempty after trimming, trimmed. */
export function firstNonEmpty(...values: string[]): string {
  for (const value of values) {
    const trimmed = goTrimSpace(value);
    if (trimmed !== '') return trimmed;
  }
  return '';
}
