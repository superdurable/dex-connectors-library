// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { StructuredOutputRules } from './wire_format.js';
import type { TextGenerationRequest } from './contract.js';

const keywords = new Set(['type','properties','required','additionalProperties','items','enum','const','description','title','minimum','maximum','minLength','maxLength','minItems','maxItems','format']);
/** Schema keywords that may move into provider guidance. */
export const movableSchemaKeywords: readonly string[] = Object.freeze(['minimum','maximum','minLength','maxLength','minItems','maxItems','format']);
/** Schema keywords that providers may omit while local validation retains them. */
export const removableSchemaKeywords: readonly string[] = Object.freeze([...movableSchemaKeywords, 'title', 'additionalProperties']);
const types = new Set(['object','array','string','number','integer','boolean','null']);
const formats = new Set(['date-time','date','time','uuid']);
function jsonPointerError(pointer: string, reason: string): Error { return new Error(`at ${pointer || 'the root'}: ${reason}`); }
function isJSONObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function pointerSegment(value: string): string { return value.replaceAll('~','~0').replaceAll('/','~1'); }
function boundedNumber(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) && parseRational(String(value)) !== undefined; }
function validateCountBounds(node: Record<string, unknown>, pointer: string, minimum: string, maximum: string): void {
  for (const keyword of [minimum, maximum]) if (keyword in node && (!Number.isSafeInteger(node[keyword]) || (node[keyword] as number) < 0)) throw jsonPointerError(pointer, `"${keyword}" must be a non-negative integer`);
  if (minimum in node && maximum in node && (node[minimum] as number) > (node[maximum] as number)) throw jsonPointerError(pointer, `"${minimum}" exceeds "${maximum}"`);
}
function schemaType(node: Record<string, unknown>, pointer: string): [string, boolean] {
  const type = node.type;
  if (typeof type === 'string' && types.has(type)) return [type, false];
  if (Array.isArray(type) && type.length === 2 && type.every(entry => typeof entry === 'string') && type.includes('null')) {
    const other = type.find(entry => entry !== 'null');
    if (other && types.has(other)) return [other, true];
  }
  throw jsonPointerError(pointer, '"type" must be one JSON type or a pair of one type and "null"');
}
/** Validate and clone the portable JSON Schema subset before a provider request. */
export function canonicalizePortableSchema(schema: Record<string, unknown>): Record<string, unknown> {
  function checkSerializable(value: unknown, seen: Set<object>): void {
    if (typeof value === 'number' && !Number.isFinite(value)) throw new Error('schema must be JSON serializable');
    if (typeof value === 'undefined' || typeof value === 'function' || typeof value === 'symbol' || typeof value === 'bigint') throw new Error('schema must be JSON serializable');
    if (value && typeof value === 'object') {
      if ('toJSON' in value && typeof value.toJSON === 'function') return;
      if (seen.has(value)) throw new Error('schema must be JSON serializable');
      seen.add(value);
      for (const child of Object.values(value)) checkSerializable(child,seen);
      seen.delete(value);
    }
  }
  checkSerializable(schema,new Set());
  let canonical: unknown;
  try { canonical = JSON.parse(JSON.stringify(schema)); } catch { throw new Error('schema must be JSON serializable'); }
  if (!isJSONObject(canonical)) throw new Error('schema must be a JSON object');
  const [rootType, nullable] = schemaType(canonical, '');
  if (rootType !== 'object' || nullable) throw jsonPointerError('', 'the root must have type "object"');
  checkNode(canonical, '', 1);
  return canonical;
}
function checkNode(node: Record<string, unknown>, pointer: string, depth: number): void {
  if (depth > 10) throw jsonPointerError(pointer, 'the schema is nested deeper than 10 levels');
  for (const keyword of Object.keys(node).sort()) if (!keywords.has(keyword)) throw jsonPointerError(pointer, `keyword ${JSON.stringify(keyword.slice(0,64))} is outside the portable subset`);
  const [type] = schemaType(node, pointer);
  const keywordRequiredType: Record<string,string> = { properties:'object', required:'object', additionalProperties:'object', items:'array', minItems:'array', maxItems:'array', minLength:'string', maxLength:'string', format:'string' };
  for (const keyword of Object.keys(node).sort()) {
    if (keywordRequiredType[keyword] && keywordRequiredType[keyword] !== type) throw jsonPointerError(pointer, `keyword "${keyword}" applies only to type "${keywordRequiredType[keyword]}"`);
    if ((keyword === 'minimum' || keyword === 'maximum') && type !== 'number' && type !== 'integer') throw jsonPointerError(pointer, `keyword "${keyword}" applies only to number and integer types`);
  }
  for (const keyword of ['description','title']) if (keyword in node && typeof node[keyword] !== 'string') throw jsonPointerError(pointer, `"${keyword}" must be a string`);
  if ('enum' in node && (!Array.isArray(node.enum) || node.enum.length === 0)) throw jsonPointerError(pointer, '"enum" must be a non-empty array');
  for (const value of (node.enum as unknown[] | undefined) ?? []) if (!scalar(value)) throw jsonPointerError(pointer, '"enum" values must be strings, numbers, booleans, or null');
  if ('const' in node && !scalar(node.const)) throw jsonPointerError(pointer, '"const" must be a string, number, boolean, or null');
  if (type === 'object') {
    if (!isJSONObject(node.properties)) throw jsonPointerError(pointer, 'an object requires a "properties" object');
    if (node.additionalProperties !== false) throw jsonPointerError(pointer, 'an object must set "additionalProperties" to false');
    if (!Array.isArray(node.required)) throw jsonPointerError(pointer, 'an object must list every property in "required"');
    const required = node.required as unknown[];
    if (required.some(name => typeof name !== 'string') || new Set(required).size !== required.length) throw jsonPointerError(pointer, '"required" must list property names once each');
    for (const name of required) if (!Object.hasOwn(node.properties, name as string)) throw jsonPointerError(pointer, '"required" lists a property that "properties" does not declare');
    if (required.length !== Object.keys(node.properties).length) throw jsonPointerError(pointer, 'an object must list every property in "required"; use type [T, "null"] for an optional value');
    for (const [name, child] of Object.entries(node.properties).sort(([a],[b]) => a.localeCompare(b))) {
      const childPointer = `${pointer}/properties/${pointerSegment(name)}`;
      if (!isJSONObject(child)) throw jsonPointerError(childPointer, 'a property schema must be an object');
      checkNode(child, childPointer, depth+1);
    }
  } else if (type === 'array') {
    validateCountBounds(node, pointer, 'minItems','maxItems');
    if (!isJSONObject(node.items)) throw jsonPointerError(pointer, 'an array requires an "items" schema object');
    checkNode(node.items, `${pointer}/items`, depth+1);
  } else if (type === 'string') {
    if ('format' in node && !formats.has(node.format as string)) throw jsonPointerError(pointer, '"format" must be date-time, date, time, or uuid');
    validateCountBounds(node, pointer, 'minLength','maxLength');
  } else if (type === 'number' || type === 'integer') {
    for (const keyword of ['minimum','maximum']) if (keyword in node && !boundedNumber(node[keyword])) throw jsonPointerError(pointer, `"${keyword}" must be a number`);
    if (node.minimum !== undefined && node.maximum !== undefined && (node.minimum as number) > (node.maximum as number)) throw jsonPointerError(pointer, '"minimum" exceeds "maximum"');
  }
}
function scalar(value: unknown): boolean { return value === null || typeof value === 'string' || typeof value === 'boolean' || boundedNumber(value); }
/** Transform only the provider copy; the original schema remains authoritative. */
export function transformSchemaForProvider(schema: Record<string, unknown>, rules: StructuredOutputRules): Record<string, unknown> {
  const copy = JSON.parse(JSON.stringify(schema)) as Record<string, unknown>;
  function visit(node: Record<string, unknown>): void {
    const constraints: string[] = [];
    for (const keyword of [...(rules.keywordsMovedToDescription ?? [])].sort()) if (Object.hasOwn(node, keyword)) { constraints.push(`${keyword}: ${marshalGoJSON(node[keyword])}`); delete node[keyword]; }
    if (constraints.length) node.description = `${typeof node.description === 'string' && node.description ? `${node.description} ` : ''}(${constraints.join(', ')})`;
    for (const keyword of rules.keywordsRemoved ?? []) delete node[keyword];
    if (isJSONObject(node.properties)) for (const child of Object.values(node.properties)) if (isJSONObject(child)) visit(child);
    if (isJSONObject(node.items)) visit(node.items);
  }
  visit(copy);
  return copy;
}
/** Add the Go-compatible JSON object instruction for providers without schema enforcement. */
export function jsonObjectInstruction(name: string, description: string, schema: Record<string, unknown>): string {
  return `Respond with only one JSON object, without Markdown code fences, that matches the JSON Schema named ${JSON.stringify(name)}${description ? ` (${description})` : ''}:\n${marshalGoJSON(schema)}`;
}
function marshalGoJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(marshalGoJSON).join(',')}]`;
  if (isJSONObject(value)) {
    const keys = Object.keys(value).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
    return `{${keys.map(key => `${marshalGoJSON(key)}:${marshalGoJSON(value[key])}`).join(',')}}`;
  }
  const encoded = JSON.stringify(value);
  if (encoded === undefined) throw new Error('schema must be JSON serializable');
  return encoded.replace(/[<>&\u2028\u2029]/g, character => `\\u${character.codePointAt(0)!.toString(16).padStart(4, '0')}`);
}
/** Remove whitespace and one JSON Markdown fence, then validate the original schema. */
export function extractStructuredAnswer(request: TextGenerationRequest, generatedText: string): string {
  if (!request.structuredOutput) return generatedText;
  const schema = canonicalizePortableSchema(request.structuredOutput.schema);
  return extractStructuredAnswerFromSchema(schema, generatedText);
}
/** Check generated JSON against an already canonical schema without re-serializing it. */
export function extractStructuredAnswerFromSchema(schema: Record<string, unknown>, generatedText: string): string {
  let text = generatedText.trim();
  const fence = /^```([^\n]*)\n([\s\S]*)```$/.exec(text);
  if (fence && (!fence[1]?.trim() || fence[1].trim().toLowerCase() === 'json')) text = (fence[2] ?? '').trim();
  let value: unknown;
  const sourceByHolder = new WeakMap<object, Map<string, string>>();
  try {
    const sourceReviver = function (this: object, key: string, child: unknown, context: { source?: string }) {
      if (typeof child === 'number' && context.source !== undefined) {
        let sources = sourceByHolder.get(this);
        if (!sources) { sources = new Map(); sourceByHolder.set(this, sources); }
        sources.set(key, context.source);
      }
      return child;
    };
    value = JSON.parse(text, sourceReviver as Parameters<typeof JSON.parse>[1]);
  } catch { throw jsonPointerError('', 'the text is not one JSON value'); }
  const numbers = new Map<string,string>();
  function assign(current: unknown, pointer: string): void {
    if (current === null || typeof current !== 'object') return;
    for (const [name, child] of Object.entries(current)) {
      const childPointer = `${pointer}/${pointerSegment(name)}`;
      if (typeof child === 'number') numbers.set(childPointer, sourceByHolder.get(current)?.get(name) ?? '');
      else assign(child, childPointer);
    }
  }
  assign(value,'');
  validateInstance(value, schema, '', numbers);
  return text;
}
function validateInstance(value: unknown, schema: Record<string, unknown>, pointer: string, numbers: Map<string,string>): void {
  const [type, nullable] = schemaType(schema, pointer);
  const number = typeof value === 'number' ? parseRational(numbers.get(pointer) ?? '') : undefined;
  if (typeof value === 'number' && !number) throw jsonPointerError(pointer, 'the number is out of range');
  if (value === null) { if (!nullable && type !== 'null') throw jsonPointerError(pointer, `expected type ${type}, found null`); }
  else {
    const actualType = Array.isArray(value) ? 'array' : isJSONObject(value) ? 'object' : typeof value;
    if (!(type === actualType || type === 'integer' && number && number.numerator % number.denominator === 0n || type === 'number' && number)) throw jsonPointerError(pointer, `expected type ${type}`);
  }
  const equal = (expected: unknown): boolean => {
    if (typeof expected === 'number' && number) { const wanted = parseRational(String(expected)); return wanted !== undefined && compareRational(wanted,number) === 0; }
    return Object.is(expected,value);
  };
  if (Array.isArray(schema.enum) && !schema.enum.some(equal)) throw jsonPointerError(pointer, 'the value is not one of the enum values');
  if ('const' in schema && !equal(schema.const)) throw jsonPointerError(pointer, 'the value does not equal const');
  if (value === null) return;
  if (type === 'object' && isJSONObject(value) && isJSONObject(schema.properties)) {
    for (const name of Object.keys(value).sort()) if (!Object.hasOwn(schema.properties, name)) throw jsonPointerError(pointer, 'the object has a property the schema does not declare');
    for (const [name, child] of Object.entries(schema.properties).sort(([a],[b]) => a.localeCompare(b))) {
      const childPointer = `${pointer}/${pointerSegment(name)}`;
      if (!Object.hasOwn(value, name)) throw jsonPointerError(childPointer, 'a required property is missing');
      if (isJSONObject(child)) validateInstance(value[name], child, childPointer, numbers);
    }
  } else if (type === 'array' && Array.isArray(value) && isJSONObject(schema.items)) {
    checkInstanceCount(value.length, schema, pointer, 'minItems','maxItems','items');
    value.forEach((item, index) => validateInstance(item, schema.items as Record<string, unknown>, `${pointer}/${index}`, numbers));
  } else if (type === 'string' && typeof value === 'string') {
    checkInstanceCount([...value].length, schema, pointer, 'minLength','maxLength','characters');
    if (typeof schema.format === 'string' && !matchesFormat(value, schema.format)) throw jsonPointerError(pointer, `the string does not match format ${schema.format}`);
  } else if ((type === 'number' || type === 'integer') && typeof value === 'number') {
    if (number && typeof schema.minimum === 'number' && compareRational(number, parseRational(String(schema.minimum))!) < 0) throw jsonPointerError(pointer, 'the value is below minimum');
    if (number && typeof schema.maximum === 'number' && compareRational(number, parseRational(String(schema.maximum))!) > 0) throw jsonPointerError(pointer, 'the value is above maximum');
  }
}
function checkInstanceCount(value: number, schema: Record<string, unknown>, pointer: string, minimum: string, maximum: string, unit: string): void {
  if (typeof schema[minimum] === 'number' && value < schema[minimum]) throw jsonPointerError(pointer, `the value has fewer ${unit} than ${minimum}`);
  if (typeof schema[maximum] === 'number' && value > schema[maximum]) throw jsonPointerError(pointer, `the value has more ${unit} than ${maximum}`);
}
function matchesFormat(value: string, format: string): boolean {
  if (format === 'uuid') return /^[0-9A-Fa-f]{8}-(?:[0-9A-Fa-f]{4}-){3}[0-9A-Fa-f]{12}$/.test(value);
  if (format === 'date') {
    const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
    return !!match && isValidCalendarDate(Number(match[1]), Number(match[2]), Number(match[3]));
  }
  if (format === 'time') return matchesGoClock(value);
  const match = /^(\d{4})-(\d{2})-(\d{2})T(.+)$/.exec(value);
  if (!match || !matchesGoClock(match[4] ?? '')) return false;
  return isValidCalendarDate(Number(match[1]), Number(match[2]), Number(match[3]));
}
function isValidCalendarDate(year: number, month: number, day: number): boolean {
  const daysInMonth = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1] ?? 0;
  return month >= 1 && month <= 12 && day >= 1 && day <= daysInMonth;
}
function matchesGoClock(value: string): boolean {
  const match = /^(\d{1,2}):(\d{2}):(\d{2})(?:[.,]\d+)?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(value);
  return !!match && Number(match[1]) < 24 && Number(match[2]) < 60 && Number(match[3]) < 60 && Number(match[4] ?? 0) <= 24 && Number(match[5] ?? 0) <= 60;
}

interface Rational { numerator: bigint; denominator: bigint }
function parseRational(literal: string): Rational | undefined {
  if (!literal || literal.length > 256) return undefined;
  const match = /^(-?)(0|[1-9]\d*)(?:\.(\d+))?(?:[eE]([+-]?\d+))?$/.exec(literal);
  if (!match) return undefined;
  const exponent = Number(match[4] ?? 0);
  if (!Number.isInteger(exponent) || Math.abs(exponent) > 400) return undefined;
  const digits = (match[2] ?? '') + (match[3] ?? '');
  const power = exponent - (match[3]?.length ?? 0);
  const signed = BigInt(digits) * (match[1] === '-' ? -1n : 1n);
  return power >= 0 ? { numerator:signed * 10n ** BigInt(power), denominator:1n } : { numerator:signed, denominator:10n ** BigInt(-power) };
}
function compareRational(left: Rational, right: Rational): number {
  const difference = left.numerator * right.denominator - right.numerator * left.denominator;
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}
