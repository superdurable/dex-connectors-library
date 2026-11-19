// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// A tokenizer with Go encoding/xml non-strict semantics: HTML 4 entities decode, unknown entities stay as
// text, a mismatched end tag closes the elements it skips, and namespaces resolve as Go resolves them.

import { htmlEntityCodePoints } from './xml_entities.js';

/** XML namespace URI that the xml prefix names. */
export const namespaceXML = 'http://www.w3.org/XML/1998/namespace';
const maxXMLDepth = 128;

/** A namespace-resolved name: space is the URI, or an undeclared prefix. */
export interface XMLName { space: string; local: string }
/** One attribute with a resolved name. */
export interface XMLAttribute { name: XMLName; value: string }
/** One token in document order. */
export type XMLToken =
  | { kind: 'start'; name: XMLName; attributes: XMLAttribute[] }
  | { kind: 'end'; name: XMLName }
  | { kind: 'text'; text: string };

/** Malformed XML that Go's non-strict decoder also rejects. */
export class XMLSyntaxError extends Error {
  constructor(reason: string) { super(`XML syntax error: ${reason}`); this.name = 'XMLSyntaxError'; }
}

interface OpenElement { rawName: string; name: XMLName; restoredNamespaces: Array<[string, string | undefined]> }

/** Streams tokens from a decoded XML document. */
export class XMLTokenizer {
  private position = 0;
  private readonly stack: OpenElement[] = [];
  private readonly namespaces = new Map<string, string>();
  private readonly pending: XMLToken[] = [];
  private pendingClose: string | undefined;

  constructor(private readonly source: string) {}

  /** Return the next token, or undefined at the end of a balanced document. */
  next(): XMLToken | undefined {
    if (this.pending.length > 0) return this.pending.shift();
    if (this.pendingClose !== undefined) {
      const rawName = this.pendingClose;
      this.pendingClose = undefined;
      return this.closeElement(rawName);
    }
    while (this.position < this.source.length) {
      if (this.source[this.position] !== '<') return { kind: 'text', text: this.readText() };
      if (this.source.startsWith('<?', this.position)) { this.skipUntil('?>', 'unterminated processing instruction'); continue; }
      if (this.source.startsWith('<!--', this.position)) { this.skipComment(); continue; }
      if (this.source.startsWith('<![CDATA[', this.position)) {
        const end = this.source.indexOf(']]>', this.position + 9);
        if (end < 0) throw new XMLSyntaxError('unterminated CDATA section');
        const text = this.source.slice(this.position + 9, end);
        this.position = end + 3;
        checkCharacters(text);
        return { kind: 'text', text: text.replace(/\r\n?/g, '\n') };
      }
      if (this.source.startsWith('<!', this.position)) { this.skipDirective(); continue; }
      if (this.source.startsWith('</', this.position)) {
        this.position += 2;
        const rawName = this.readName();
        this.skipSpace();
        if (this.source[this.position] !== '>') throw new XMLSyntaxError('invalid characters between </ and >');
        this.position++;
        return this.closeElement(rawName);
      }
      this.position++;
      return this.readStartElement();
    }
    if (this.stack.length > 0) throw new XMLSyntaxError('unexpected EOF');
    return undefined;
  }

  private readStartElement(): XMLToken {
    const rawName = this.readName();
    const rawAttributes: Array<[string, string]> = [];
    let isSelfClosing = false;
    while (true) {
      this.skipSpace();
      const character = this.source[this.position];
      if (character === undefined) throw new XMLSyntaxError('unexpected EOF');
      if (character === '/') {
        if (this.source[this.position + 1] !== '>') throw new XMLSyntaxError('expected /> in element');
        this.position += 2; isSelfClosing = true; break;
      }
      if (character === '>') { this.position++; break; }
      const attributeName = this.readName();
      this.skipSpace();
      if (this.source[this.position] !== '=') { rawAttributes.push([attributeName, attributeName]); continue; }
      this.position++;
      this.skipSpace();
      rawAttributes.push([attributeName, this.readAttributeValue()]);
    }
    if (this.stack.length >= maxXMLDepth * 64) throw new XMLSyntaxError('XML nesting is too deep');
    const restoredNamespaces: Array<[string, string | undefined]> = [];
    for (const [name, value] of rawAttributes) {
      const parsed = splitName(name);
      const prefix = parsed.space === 'xmlns' ? parsed.local : parsed.space === '' && parsed.local === 'xmlns' ? '' : undefined;
      if (prefix === undefined) continue;
      restoredNamespaces.push([prefix, this.namespaces.get(prefix)]);
      this.namespaces.set(prefix, value);
    }
    const name = this.translate(splitName(rawName), true);
    const attributes = rawAttributes.map(([attributeName, value]) => ({ name: this.translate(splitName(attributeName), false), value }));
    this.stack.push({ rawName, name, restoredNamespaces });
    if (isSelfClosing) this.pending.push(this.closeElement(rawName));
    return { kind: 'start', name, attributes };
  }

  private closeElement(rawName: string): XMLToken {
    const top = this.stack.pop();
    if (!top) throw new XMLSyntaxError(`unexpected end element </${rawName}>`);
    const closing = splitName(rawName);
    const opened = splitName(top.rawName);
    if (opened.local !== closing.local) this.pendingClose = rawName;
    else if (opened.space !== closing.space) throw new XMLSyntaxError('element closed in another name space');
    for (const [prefix, previous] of top.restoredNamespaces.reverse()) {
      if (previous === undefined) this.namespaces.delete(prefix);
      else this.namespaces.set(prefix, previous);
    }
    return { kind: 'end', name: top.name };
  }

  private translate(name: XMLName, isElementName: boolean): XMLName {
    if (name.space === 'xmlns') return name;
    if (name.space === '' && !isElementName) return name;
    if (name.space === 'xml') return { space: namespaceXML, local: name.local };
    if (name.space === '' && name.local === 'xmlns') return name;
    const resolved = this.namespaces.get(name.space);
    return resolved !== undefined ? { space: resolved, local: name.local } : name;
  }

  private readName(): string {
    const match = /^(?:[\p{L}\p{N}_:.\u00b7\u203f\u2040-]|\p{Mn})+/u.exec(this.source.slice(this.position, this.position + 1024));
    if (!match || /^[\p{N}.-]/u.test(match[0])) throw new XMLSyntaxError('expected element name');
    this.position += match[0].length;
    return match[0];
  }

  private readAttributeValue(): string {
    const quote = this.source[this.position];
    if (quote === '"' || quote === "'") {
      const end = this.source.indexOf(quote, this.position + 1);
      if (end < 0) throw new XMLSyntaxError('unexpected EOF');
      const raw = this.source.slice(this.position + 1, end);
      this.position = end + 1;
      if (raw.includes('<')) throw new XMLSyntaxError('unescaped < inside quoted string');
      return decodeText(raw);
    }
    const match = /^[^\s>/]+/.exec(this.source.slice(this.position));
    if (!match) throw new XMLSyntaxError('unquoted or missing attribute value in element');
    this.position += match[0].length;
    return decodeText(match[0]);
  }

  private readText(): string {
    const end = this.source.indexOf('<', this.position);
    const raw = this.source.slice(this.position, end < 0 ? this.source.length : end);
    this.position = end < 0 ? this.source.length : end;
    return decodeText(raw);
  }

  private skipSpace(): void {
    while (this.position < this.source.length && ' \t\r\n'.includes(this.source[this.position]!)) this.position++;
  }

  private skipUntil(terminator: string, reason: string): void {
    const end = this.source.indexOf(terminator, this.position);
    if (end < 0) throw new XMLSyntaxError(reason);
    this.position = end + terminator.length;
  }

  private skipComment(): void {
    const end = this.source.indexOf('--', this.position + 4);
    if (end < 0) throw new XMLSyntaxError('unterminated comment');
    if (this.source[end + 2] !== '>') throw new XMLSyntaxError('invalid sequence "--" not allowed in comments');
    this.position = end + 3;
  }

  private skipDirective(): void {
    let depth = 0;
    let quote = '';
    for (let index = this.position + 2; index < this.source.length; index++) {
      const character = this.source[index]!;
      if (quote) { if (character === quote) quote = ''; continue; }
      if (character === '"' || character === "'") quote = character;
      else if (character === '<') depth++;
      else if (character === '>') {
        if (depth === 0) { this.position = index + 1; return; }
        depth--;
      }
    }
    throw new XMLSyntaxError('unterminated directive');
  }
}

/** One element with its children and character data in document order. */
export class XMLNode {
  readonly children: XMLNode[] = [];
  readonly segments: Array<string | XMLNode> = [];
  constructor(readonly name: XMLName, readonly attributes: XMLAttribute[]) {}

  /** The node's own character data, without that of its children. */
  text(): string { return this.segments.filter((segment): segment is string => typeof segment === 'string').join(''); }

  /** The first direct child with this exact namespace and local name. */
  firstChild(space: string, local: string): XMLNode | undefined {
    return this.children.find(child => child.name.local === local && child.name.space === space);
  }

  /** Every direct child with this exact namespace and local name. */
  childrenNamed(space: string, local: string): XMLNode[] {
    return this.children.filter(child => child.name.local === local && child.name.space === space);
  }

  /** The trimmed text of the first matching child, or an empty string. */
  childText(space: string, local: string): string { return this.firstChild(space, local)?.text().trim() ?? ''; }

  /** The value of an attribute with this exact namespace and local name, or an empty string. */
  attribute(space: string, local: string): string {
    return this.attributes.find(attribute => attribute.name.local === local && attribute.name.space === space)?.value ?? '';
  }

  /** Character data of the node and its descendants in document order, with a space before each child. */
  allText(): string {
    return this.segments.map(segment => typeof segment === 'string' ? segment : ' ' + segment.allText()).join('');
  }
}

/** Skip to the root element; text before it fails as in Go. */
export function readRootElement(tokenizer: XMLTokenizer): Extract<XMLToken, { kind: 'start' }> {
  while (true) {
    const token = tokenizer.next();
    if (!token) throw new XMLSyntaxError('no root element');
    if (token.kind === 'start') return token;
    if (token.kind === 'text' && token.text.trim() !== '') throw new XMLSyntaxError('text before the root element');
  }
}

/** Read the element that start opened, through its end element, within the depth bound. */
export function decodeXMLNode(tokenizer: XMLTokenizer, start: Extract<XMLToken, { kind: 'start' }>, depth = 1): XMLNode {
  if (depth > maxXMLDepth) throw new XMLSyntaxError('XML nesting is too deep');
  const node = new XMLNode(start.name, start.attributes);
  while (true) {
    const token = tokenizer.next();
    if (!token) throw new XMLSyntaxError('unexpected EOF');
    if (token.kind === 'end') return node;
    if (token.kind === 'text') { node.segments.push(token.text); continue; }
    const child = decodeXMLNode(tokenizer, token, depth + 1);
    node.children.push(child);
    node.segments.push(child);
  }
}

function splitName(raw: string): XMLName {
  const colon = raw.indexOf(':');
  if (colon <= 0 || colon === raw.length - 1) return { space: '', local: raw };
  return { space: raw.slice(0, colon), local: raw.slice(colon + 1) };
}

// Decode character and entity references; an unknown or malformed reference stays as text.
function decodeText(raw: string): string {
  checkCharacters(raw);
  const normalized = raw.replace(/\r\n?/g, '\n');
  return normalized.replace(/&(#[xX][0-9a-fA-F]+|#[0-9]+|[A-Za-z_][A-Za-z0-9_.-]*);/g, (reference, body: string) => {
    if (body.startsWith('#')) {
      const codePoint = body[1] === 'x' || body[1] === 'X' ? Number.parseInt(body.slice(2), 16) : Number.parseInt(body.slice(1), 10);
      return isCharacter(codePoint) ? String.fromCodePoint(codePoint) : reference;
    }
    switch (body) {
      case 'lt': return '<';
      case 'gt': return '>';
      case 'amp': return '&';
      case 'apos': return "'";
      case 'quot': return '"';
    }
    const codePoint = htmlEntityCodePoints.get(body);
    return codePoint === undefined ? reference : String.fromCodePoint(codePoint);
  });
}

function checkCharacters(text: string): void {
  for (const character of text) {
    if (!isCharacter(character.codePointAt(0)!)) throw new XMLSyntaxError('illegal character code');
  }
}

function isCharacter(codePoint: number): boolean {
  return codePoint === 0x09 || codePoint === 0x0a || codePoint === 0x0d || (codePoint >= 0x20 && codePoint <= 0xd7ff) ||
    (codePoint >= 0xe000 && codePoint <= 0xfffd) || (codePoint >= 0x10000 && codePoint <= 0x10ffff);
}
