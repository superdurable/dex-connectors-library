// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { SOQLValue } from './models.js';

const bindingName = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;
const numberPattern = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/;
const identifierPattern = /^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*){0,4}$/;
const rfc3339 = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/;
const loneSurrogate = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u;

function quoteSOQL(value: string, like = false, prefix = '', suffix = ''): string {
  if (loneSurrogate.test(value)) throw new TypeError('text must be valid UTF-8');
  let rendered = "'" + prefix;
  for (const character of value) {
    switch (character) {
      case '\\': rendered += '\\\\'; break;
      case "'": rendered += "\\'"; break;
      case '"': rendered += '\\"'; break;
      case '\n': rendered += '\\n'; break;
      case '\r': rendered += '\\r'; break;
      case '\t': rendered += '\\t'; break;
      case '\b': rendered += '\\b'; break;
      case '\f': rendered += '\\f'; break;
      case '%':
      case '_': rendered += (like ? '\\' : '') + character; break;
      default:
        if (character.codePointAt(0)! < 32 || character.codePointAt(0) === 127) throw new TypeError('text contains a control character SOQL cannot escape');
        rendered += character;
    }
  }
  return rendered + suffix + "'";
}

function renderBinding(value: SOQLValue): string {
  switch (value.kind) {
    case 'string': return quoteSOQL(value.text ?? '');
    case 'likeContains': return quoteSOQL(value.text ?? '', true, '%', '%');
    case 'likeStartsWith': return quoteSOQL(value.text ?? '', true, '', '%');
    case 'stringList': {
      const texts = value.texts ?? [];
      if (texts.length < 1 || texts.length > 1000) throw new TypeError('stringList must hold from 1 to 1000 values');
      return '(' + texts.map(item => quoteSOQL(item)).join(', ') + ')';
    }
    case 'number': {
      const text = value.text ?? '';
      if (text.length > 40 || !numberPattern.test(text)) throw new TypeError('number must be a plain decimal such as -12 or 1250.50');
      return text;
    }
    case 'boolean': return value.boolean ? 'TRUE' : 'FALSE';
    case 'null': return 'NULL';
    case 'identifier': {
      const text = value.text ?? '';
      if (Buffer.byteLength(text) > 255 || !identifierPattern.test(text)) throw new TypeError('identifier must be an API name or a dotted relationship path');
      return text;
    }
    case 'date': {
      const text = value.text ?? '';
      if (!/^\d{4}-\d{2}-\d{2}$/.test(text) || Number.isNaN(Date.parse(text + 'T00:00:00Z')) || new Date(text + 'T00:00:00Z').toISOString().slice(0, 10) !== text)
        throw new TypeError('date must be YYYY-MM-DD');
      return text;
    }
    case 'dateTime': {
      const text = value.text ?? '';
      const match = rfc3339.exec(text);
      if (!match) throw new TypeError('dateTime must be an RFC 3339 timestamp');
      const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number);
      const local = new Date(0);
      local.setUTCFullYear(year!, month! - 1, day!);
      local.setUTCHours(hour!, minute!, second!, 0);
      if (month! < 1 || month! > 12 || day! < 1 || hour! > 23 || minute! > 59 || second! > 59 ||
        local.getUTCFullYear() !== year || local.getUTCMonth() + 1 !== month || local.getUTCDate() !== day)
        throw new TypeError('dateTime must be an RFC 3339 timestamp');
      const zone = match[8]!;
      const offsetHour = zone === 'Z' ? 0 : Number(zone.slice(1, 3));
      const offsetMinute = zone === 'Z' ? 0 : Number(zone.slice(4, 6));
      if (offsetHour > 24 || offsetMinute > 60) throw new TypeError('dateTime must be an RFC 3339 timestamp');
      const offset = (offsetHour * 60 + offsetMinute) * 60_000 * (zone.startsWith('-') ? -1 : 1);
      return new Date(local.getTime() - offset).toISOString().slice(0, 19) + 'Z';
    }
    default: throw new TypeError('kind is not a supported SOQL value kind');
  }
}

/** Render typed SOQL bindings only outside quoted string literals. */
export function renderSOQL(statement: string, bindings: Record<string, SOQLValue>): string {
  if (!statement.trim()) throw new TypeError('soql is required');
  if (loneSurrogate.test(statement)) throw new TypeError('soql must be valid UTF-8');
  if (Object.keys(bindings).length > 100) throw new TypeError('soql accepts at most 100 bindings');
  for (const name of Object.keys(bindings)) if (!bindingName.test(name)) throw new TypeError('binding names must match the supported pattern');
  let rendered = '';
  let inLiteral = false;
  const used = new Set<string>();
  for (let index = 0; index < statement.length; index++) {
    const character = statement[index]!;
    if (inLiteral && character === '\\' && index + 1 < statement.length) {
      rendered += character + statement[++index];
      continue;
    }
    if (character === "'") inLiteral = !inLiteral;
    if (!inLiteral && character === ':' && /[A-Za-z_]/.test(statement[index + 1] ?? '')) {
      let end = index + 1;
      while (/[A-Za-z0-9_]/.test(statement[end] ?? '') && end < statement.length) end++;
      const name = statement.slice(index + 1, end);
      const value = bindings[name];
      if (!value) throw new TypeError(`placeholder :${name} has no binding`);
      try { rendered += renderBinding(value); }
      catch (error) { throw new TypeError(`binding ${name}: ${error instanceof Error ? error.message : 'invalid value'}`); }
      used.add(name);
      index = end - 1;
    } else rendered += character;
  }
  if (inLiteral) throw new TypeError('soql has an unterminated string literal');
  const unused = Object.keys(bindings).filter(name => !used.has(name)).sort();
  if (unused.length) throw new TypeError(`bindings ${unused.join(', ')} have no placeholder`);
  return rendered;
}
