// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { appendInline, appendParagraphBlock, flattenBlocksToInlines, markdownFenceLanguage, safeLinkHref, trimInlineEdges, type DocumentBlock, type DocumentInline, type DocumentTableRow } from './document.js';

interface ADFMark { type?: string; attrs?: Record<string, unknown>; }
interface ADFNode { type?: string; text?: string; attrs?: Record<string, unknown>; marks?: ADFMark[]; content?: ADFNode[]; }

/** Parse an Atlassian Document Format body into the shared document model. */
export function parseADFDocument(value: string): DocumentBlock[] {
  if (!value.trim()) return [];
  let document: unknown;
  try { document = JSON.parse(value); } catch { throw new TypeError('body is not readable Atlassian Document Format'); }
  if (!isADFNode(document) || document.type !== 'doc') throw new TypeError('body is not readable Atlassian Document Format');
  return readBlocks(document.content || [], 0);
}
function isADFNode(value: unknown): value is ADFNode { return !!value && typeof value === 'object' && !Array.isArray(value); }
function readBlocks(nodes: ADFNode[], depth: number): DocumentBlock[] {
  let blocks: DocumentBlock[] = [], inlines: DocumentInline[] = [];
  for (const node of nodes) {
    if (['text', 'hardBreak', 'mention', 'status', 'emoji', 'inlineCard', 'date', 'placeholder', 'inlineExtension', 'mediaInline'].includes(node.type || '')) {
      inlines = appendADFInline(inlines, node); continue;
    }
    blocks = appendParagraphBlock(blocks, inlines); inlines = [];
    blocks.push(...readBlock(node, depth + 1));
  }
  return appendParagraphBlock(blocks, inlines);
}
function readBlock(node: ADFNode, depth: number): DocumentBlock[] {
  if (depth > 64) return [];
  const content = Array.isArray(node.content) ? node.content : [];
  switch (node.type) {
    case 'paragraph': return appendParagraphBlock([], readInlines(content));
    case 'heading': return [{ kind: 'heading', headingLevel: numberAttribute(node.attrs, 'level'), inlines: trimInlineEdges(readInlines(content)) }];
    case 'bulletList': case 'orderedList': case 'taskList': case 'decisionList': return [readList(node, depth)];
    case 'blockquote': case 'panel': return [{ kind: 'quote', children: readBlocks(content, depth) }];
    case 'codeBlock': return [{ kind: 'code', codeLanguage: markdownFenceLanguage(stringAttribute(node.attrs, 'language')), codeText: content.map(child => child.text || '').join('') }];
    case 'rule': return [{ kind: 'rule' }];
    case 'table': return [{ kind: 'table', tableRows: readTableRows(node, depth) }];
    case 'expand': case 'nestedExpand': {
      const title = stringAttribute(node.attrs, 'title').trim();
      const prefix: DocumentBlock[] = title ? [{ kind: 'paragraph', inlines: [{ text: title, isStrong: true }] }] : [];
      return [...prefix, ...readBlocks(content, depth)];
    }
    case 'blockCard': case 'embedCard': {
      const href = safeLinkHref(stringAttribute(node.attrs, 'url'));
      return href ? [{ kind: 'paragraph', inlines: [{ text: href, linkHref: href }] }] : [];
    }
    case 'mediaSingle': case 'mediaGroup': case 'media': case 'extension': return [];
    default: return readBlocks(content, depth);
  }
}
function readList(node: ADFNode, depth: number): DocumentBlock {
  const kind = node.type === 'orderedList' ? 'orderedList' : node.type === 'taskList' ? 'taskList' : 'bulletList';
  return { kind, orderedStart: numberAttribute(node.attrs, 'order'), listItems: (node.content || []).map(item => ({
    isTaskDone: stringAttribute(item.attrs, 'state') === 'DONE',
    blocks: item.type === 'taskItem' || item.type === 'decisionItem' ? appendParagraphBlock([], readInlines(item.content || [])) : readBlocks(item.content || [], depth),
  })) };
}
function readTableRows(node: ADFNode, depth: number): DocumentTableRow[] {
  return (node.content || []).filter(row => row.type === 'tableRow').map(row => ({
    isHeader: !!row.content?.length && row.content.every(cell => cell.type === 'tableHeader'),
    cells: (row.content || []).map(cell => flattenBlocksToInlines(readBlocks(cell.content || [], depth))),
  }));
}
function readInlines(nodes: ADFNode[]): DocumentInline[] { let inlines: DocumentInline[] = []; for (const node of nodes) inlines = appendADFInline(inlines, node); return inlines; }
function appendADFInline(inlines: DocumentInline[], node: ADFNode): DocumentInline[] {
  const span: DocumentInline = {};
  for (const mark of node.marks || []) {
    if (mark.type === 'strong') span.isStrong = true;
    if (mark.type === 'em') span.isEmphasis = true;
    if (mark.type === 'code') span.isCode = true;
    if (mark.type === 'strike') span.isStrikethrough = true;
    if (mark.type === 'link') span.linkHref = safeLinkHref(stringAttribute(mark.attrs, 'href'));
  }
  switch (node.type) {
    case 'text': span.text = node.text || ''; break;
    case 'hardBreak': return appendInline(inlines, { isLineBreak: true });
    case 'mention': case 'status': span.text = stringAttribute(node.attrs, 'text'); break;
    case 'emoji': span.text = stringAttribute(node.attrs, 'text') || stringAttribute(node.attrs, 'shortName'); break;
    case 'inlineCard': span.linkHref = safeLinkHref(stringAttribute(node.attrs, 'url')); span.text = span.linkHref; break;
    case 'date': {
      const timestamp = stringAttribute(node.attrs, 'timestamp');
      span.text = formatADFDate(timestamp);
      break;
    }
  }
  return appendInline(inlines, span);
}
function stringAttribute(attributes: Record<string, unknown> | undefined, key: string): string { const value = attributes?.[key]; return typeof value === 'string' ? value : ''; }
function numberAttribute(attributes: Record<string, unknown> | undefined, key: string): number { const value = attributes?.[key]; return typeof value === 'number' ? value : 0; }

function formatADFDate(timestamp: string): string {
  if (!/^[+-]?\d+$/.test(timestamp)) return '';
  const milliseconds = BigInt(timestamp);
  if (milliseconds < -(1n << 63n) || milliseconds > (1n << 63n) - 1n) return '';
  let days = milliseconds / 86_400_000n;
  if (milliseconds < 0n && milliseconds % 86_400_000n !== 0n) days--;
  const eraDays = days + 719468n;
  const era = eraDays >= 0n ? eraDays / 146097n : (eraDays - 146096n) / 146097n;
  const dayOfEra = eraDays - era * 146097n;
  const yearOfEra = (dayOfEra - dayOfEra / 1460n + dayOfEra / 36524n - dayOfEra / 146096n) / 365n;
  let year = yearOfEra + era * 400n;
  const dayOfYear = dayOfEra - (365n * yearOfEra + yearOfEra / 4n - yearOfEra / 100n);
  const monthPosition = (5n * dayOfYear + 2n) / 153n;
  const day = dayOfYear - (153n * monthPosition + 2n) / 5n + 1n;
  const month = monthPosition + (monthPosition < 10n ? 3n : -9n);
  if (month <= 2n) year++;
  return `${year.toString().padStart(4, '0')}-${month.toString().padStart(2, '0')}-${day.toString().padStart(2, '0')}`;
}
