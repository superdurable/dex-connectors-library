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
