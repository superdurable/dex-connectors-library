// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

class NumberToken {
  constructor(readonly source: string) {}
}

/** Preserve the source spelling of JSON numbers while decoding provider fields. */
export function parseBalanceResponse(body: Uint8Array): unknown {
  return JSON.parse(Buffer.from(body).toString('utf8'), (_name, value: unknown, context?: { source?: string }) => {
    if (typeof value === 'number') {
      if (!context?.source) throw new TypeError('A source-aware JSON parser is required');
      return new NumberToken(context.source);
    }
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
    }
    return value;
  }) as unknown;
}

/** Read a quoted or numeric JSON decimal without rounding its representation. */
export function exactAmount(value: unknown): string | undefined {
  const source = value instanceof NumberToken ? value.source : value;
  return typeof source === 'string' && /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/.test(source) ? source : undefined;
}

/** Decode a signed 64-bit integer token for a provider status or payment code. */
export function integerCode(value: unknown): bigint | undefined {
  if (!(value instanceof NumberToken) || value.source.length > 20 || !/^-?(?:0|[1-9][0-9]*)$/.test(value.source)) return undefined;
  const integer = BigInt(value.source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? integer : undefined;
}

interface AccountFields {
  readonly [field: string]: 'integer' | 'string' | 'boolean' | AccountFields;
}

/** Decode declared account fields, retaining integers outside JavaScript's exact number range. */
export function decodeAccountFields(body: Uint8Array, fields: AccountFields): Record<string, unknown> {
  return readAccountFields(parseBalanceResponse(body), fields);
}

function readAccountFields(value: unknown, fields: AccountFields): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Object.getPrototypeOf(value) !== Object.prototype)
    throw new TypeError('Expected an account object');
  const source = value as Record<string, unknown>;
  const result: Record<string, unknown> = {};
  for (const [name, kind] of Object.entries(fields)) {
    const field = source[name];
    if (field == null) continue;
    if (typeof kind === 'object') result[name] = readAccountFields(field, kind);
    else if (kind === 'integer') {
      const integer = integerCode(field);
      if (integer === undefined) throw new TypeError('Invalid account integer');
      const number = Number(integer);
      result[name] = Number.isSafeInteger(number) ? number : integer;
    } else {
      if (typeof field !== kind) throw new TypeError('Invalid account field');
      result[name] = field;
    }
  }
  return result;
}
