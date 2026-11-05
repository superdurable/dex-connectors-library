// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { parseDocument } from 'htmlparser2';
import { appendInline, appendParagraphBlock, flattenBlocksToInlines, markdownFenceLanguage, safeLinkHref, trimInlineEdges, type DocumentBlock, type DocumentInline, type DocumentTableRow } from './document.js';

interface StorageNode { type: string; name?: string; data?: string; attribs?: Record<string, string>; children?: StorageNode[]; }
const blockElements = new Set(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'ac:task-list', 'blockquote', 'pre', 'hr', 'table', 'div', 'section', 'ac:layout', 'ac:layout-section', 'ac:layout-cell', 'ac:rich-text-body', 'ac:adf-extension']);

/** Parse lenient Confluence storage XHTML into the shared document model. */
export function parseStorageDocument(storage: string): DocumentBlock[] {
  if (Buffer.byteLength(storage) > 16_000_000) throw new TypeError('body is not readable Confluence storage format');
  if (storage.includes('<![CDATA[') && !/<!\[CDATA\[[\s\S]*?\]\]>/.test(storage)) throw new TypeError('body is not readable Confluence storage format');
  const html = storage.replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, (_match, contents: string) => contents.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'));
  const parsed = parseDocument(`<storage-root>${html}</storage-root>`, { decodeEntities: true, recognizeSelfClosing: true, lowerCaseTags: true }) as unknown as StorageNode;
  const root = (parsed.children || []).find(node => node.name === 'storage-root');
  if (!root) throw new TypeError('body is not readable Confluence storage format');
  let count = 0;
  const pending = [root];
  while (pending.length) {
    const node = pending.pop()!;
    if (++count > 200000) throw new TypeError('body is not readable Confluence storage format');
    pending.push(...node.children || []);
  }
  return readBlocks(root.children || [], 0);
}
function readBlocks(nodes: StorageNode[], depth: number): DocumentBlock[] {
  let blocks: DocumentBlock[] = [], inlines: DocumentInline[] = [];
  for (const node of nodes) {
    if (isBlockElement(node)) { blocks = appendParagraphBlock(blocks, inlines); inlines = []; blocks.push(...readBlock(node, depth + 1)); }
    else inlines = appendInlines(inlines, node, {}, depth + 1);
  }
  return appendParagraphBlock(blocks, inlines);
}
function readBlock(node: StorageNode, depth: number): DocumentBlock[] {
  if (depth > 64) return [];
  const children = node.children || [];
  switch (node.name) {
    case 'p': return appendParagraphBlock([], readInlines(children, {}, depth));
    case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6':
      return [{ kind: 'heading', headingLevel: Number(node.name[1]), inlines: trimInlineEdges(readInlines(children, {}, depth)) }];
    case 'ul': case 'ol': {
      const isOrdered = node.name === 'ol';
      return [{ kind: isOrdered ? 'orderedList' : 'bulletList', orderedStart: Number(node.attribs?.start || 0), listItems: children.filter(child => child.name === 'li').map(child => ({ blocks: readBlocks(child.children || [], depth) })) }];
    }
    case 'ac:task-list': return [{ kind: 'taskList', listItems: children.filter(child => child.name === 'ac:task').map(task => ({
      isTaskDone: (task.children || []).some(part => part.name === 'ac:task-status' && storageText(part).trim() === 'complete'),
      blocks: readBlocks((task.children || []).find(part => part.name === 'ac:task-body')?.children || [], depth),
    })) }];
    case 'blockquote': return [{ kind: 'quote', children: readBlocks(children, depth) }];
    case 'pre': return [{ kind: 'code', codeText: storageText(node).replace(/^\n+|\n+$/g, '') }];
    case 'hr': return [{ kind: 'rule' }];
    case 'table': return [{ kind: 'table', tableRows: readTableRows(node, depth) }];
    case 'ac:structured-macro': case 'ac:macro': return readMacro(node, depth);
    case 'ac:adf-extension': return readBlocks(children.find(child => child.name === 'ac:adf-fallback')?.children || [], depth);
    default: return readBlocks(children, depth);
  }
}
function readTableRows(node: StorageNode, depth: number): DocumentTableRow[] {
  const rows = (node.children || []).flatMap(child => child.name === 'tr' ? [child] : ['thead', 'tbody', 'tfoot'].includes(child.name || '') ? (child.children || []).filter(row => row.name === 'tr') : []);
  return rows.map(row => {
    const cells = (row.children || []).filter(child => child.name === 'th' || child.name === 'td');
    return { isHeader: !!cells.length && cells.every(cell => cell.name === 'th'), cells: cells.map(cell => flattenBlocksToInlines(readBlocks(cell.children || [], depth))) };
  });
}
function readMacro(node: StorageNode, depth: number): DocumentBlock[] {
  const name = node.attribs?.['ac:name'] || '';
  const children = node.children || [];
  const parameters = new Map(children.filter(child => child.name === 'ac:parameter').map(child => [child.attribs?.['ac:name'] || '', storageText(child).trim()]));
  const rich = children.find(child => child.name === 'ac:rich-text-body');
  const plain = children.find(child => child.name === 'ac:plain-text-body');
  if (name === 'code' || name === 'noformat') return [{ kind: 'code', codeLanguage: markdownFenceLanguage(parameters.get('language') || ''), codeText: storageText(plain).replace(/^\n+|\n+$/g, '') }];
  if (rich && ['info', 'note', 'warning', 'tip', 'panel'].includes(name)) return [{ kind: 'quote', children: readBlocks(rich.children || [], depth) }];
  if (rich) return readBlocks(rich.children || [], depth);
  if (plain) return [{ kind: 'paragraph', inlines: [{ text: storageText(plain).trim() }] }];
  return [];
}
function readInlines(nodes: StorageNode[], style: DocumentInline, depth: number): DocumentInline[] {
  let inlines: DocumentInline[] = [];
  for (const node of nodes) inlines = appendInlines(inlines, node, style, depth + 1);
  return inlines;
}
function appendInlines(inlines: DocumentInline[], node: StorageNode, style: DocumentInline, depth: number): DocumentInline[] {
  if (depth > 64) return inlines;
  if (node.type === 'text') return appendInline(inlines, { ...style, text: collapseWhitespace(node.data || '') });
  let nextStyle = style;
  switch (node.name) {
    case 'br': return appendInline(inlines, { isLineBreak: true });
    case 'strong': case 'b': nextStyle = { ...style, isStrong: true }; break;
    case 'em': case 'i': nextStyle = { ...style, isEmphasis: true }; break;
    case 'code': case 'tt': nextStyle = { ...style, isCode: true }; break;
    case 's': case 'del': case 'strike': nextStyle = { ...style, isStrikethrough: true }; break;
    case 'span': if ((node.attribs?.style || '').toLowerCase().includes('line-through')) nextStyle = { ...style, isStrikethrough: true }; break;
    case 'a': nextStyle = { ...style, linkHref: safeLinkHref(node.attribs?.href || '') }; break;
    case 'ac:link': return appendLinkInlines(inlines, node, style, depth);
    case 'ac:emoticon': return appendInline(inlines, { ...style, text: node.attribs?.['ac:emoji-fallback'] || '' });
    case 'time': return appendInline(inlines, { ...style, text: node.attribs?.datetime || '' });
    case 'ac:structured-macro': return appendInlineMacro(inlines, node, style);
    case 'ac:image': case 'ac:placeholder': case 'ac:parameter': case 'ac:adf-attribute': case 'ri:user': case 'ri:attachment': case 'ri:page': return inlines;
    case 'p': case 'div': case 'li': case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6': case 'tr': case 'blockquote': case 'pre': if (inlines.length) inlines = appendInline(inlines, { isLineBreak: true }); break;
  }
  for (const child of node.children || []) inlines = appendInlines(inlines, child, nextStyle, depth + 1);
  return inlines;
}
function appendLinkInlines(inlines: DocumentInline[], node: StorageNode, style: DocumentInline, depth: number): DocumentInline[] {
  let fallback = '', body: StorageNode | undefined, nextStyle = style;
  for (const child of node.children || []) {
    switch (child.name) {
      case 'ac:link-body': body = child; break;
      case 'ac:plain-text-link-body': fallback = storageText(child).trim(); break;
      case 'ri:page': if (!fallback) fallback = child.attribs?.['ri:content-title'] || ''; break;
      case 'ri:attachment': if (!fallback) fallback = child.attribs?.['ri:filename'] || ''; break;
      case 'ri:url': nextStyle = { ...style, linkHref: safeLinkHref(child.attribs?.['ri:value'] || '') }; if (!fallback) fallback = child.attribs?.['ri:value'] || ''; break;
    }
  }
  if (body) for (const child of body.children || []) inlines = appendInlines(inlines, child, nextStyle, depth + 1);
  else inlines = appendInline(inlines, { ...nextStyle, text: fallback });
  return inlines;
}
function appendInlineMacro(inlines: DocumentInline[], node: StorageNode, style: DocumentInline): DocumentInline[] {
  if (node.attribs?.['ac:name'] !== 'status') return inlines;
  const title = (node.children || []).find(child => child.name === 'ac:parameter' && child.attribs?.['ac:name'] === 'title');
  return title ? appendInline(inlines, { ...style, text: storageText(title).trim() }) : inlines;
}
function isBlockElement(node: StorageNode): boolean { return blockElements.has(node.name || '') || ['ac:structured-macro', 'ac:macro'].includes(node.name || '') && node.attribs?.['ac:name'] !== 'status'; }
function storageText(node?: StorageNode, depth = 0): string { if (!node || depth > 64) return ''; return node.type === 'text' ? node.data || '' : (node.children || []).map(child => storageText(child, depth + 1)).join(''); }
function collapseWhitespace(value: string): string { return value.replace(/[ \t\n\r\u00a0]+/g, ' '); }
