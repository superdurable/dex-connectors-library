// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { inspect } from 'node:util';
import { MaximumEnvironmentFields, MaximumEnvironmentValueBytes, isApplicationEnvironmentName, isValidEnvironmentValueEncoding } from './application_environment_rules.js';

export { isApplicationEnvironmentName } from './application_environment_rules.js';

/**
 * One application-owned string declared by AppManifest `application.environment`.
 *
 * Declarations contain no values or defaults. Absent optional members take Go's zero values: not required, not secret,
 * `minLength` 0, and no enum. Secret fields have write-only browser input and exact versioned object references.
 */
export interface EnvironmentDeclaration {
  /** Nonreserved uppercase environment name, at most 128 ASCII characters; see `isApplicationEnvironmentName`. */
  readonly name: string;
  /** Rejects a missing value; an empty string still follows `minLength` and `enum`. */
  readonly required?: boolean;
  /** Stores the value as a private object outside the ordinary configuration document. */
  readonly secret?: boolean;
  /** Minimum Unicode code point count, from 0 through 32768. */
  readonly minLength?: number;
  /** Allowed ordinary values when nonempty, at most 128 unique strings. Secret fields cannot publish allowed values. */
  readonly enum?: readonly string[] | null;
}

/** A declaration after `validateAndSortEnvironmentDeclarations`, with every member present and the enum sorted. */
export interface ValidatedEnvironmentDeclaration {
  /** Nonreserved uppercase environment name. */
  readonly name: string;
  /** Rejects a missing value. */
  readonly required: boolean;
  /** Stores the value as a private object. */
  readonly secret: boolean;
  /** Minimum Unicode code point count. */
  readonly minLength: number;
  /** Allowed values in UTF-8 byte order; empty allows any value. */
  readonly enum: readonly string[];
}

const privateEnvironmentSerializationMessage = 'private application environment cannot be serialized';

/**
 * Resolved private startup values, including application secrets.
 *
 * Values never appear in `toString`, template literals, `util.inspect`, or `console` output, and `JSON.stringify`
 * throws. Keep an instance in application memory; never place it or its values in Flow inputs, Attributes, Results,
 * Streams, logs, or browser responses. Instances are immutable and safe to share.
 */
export class ApplicationEnvironment {
  readonly #values: ReadonlyMap<string, string>;

  /** Copy resolved values; `apply` validates every name and value again before changing the process environment. */
  constructor(values: ReadonlyMap<string, string> = new Map()) { this.#values = new Map(values); }

  /** Number of resolved fields. */
  get size(): number { return this.#values.size; }

  /** Return a resolved value, which may be secret, or `undefined` when the name was not configured. */
  lookup(name: string): string | undefined { return this.#values.get(name); }

  /**
   * Set every resolved value in `environment`, which defaults to `process.env`.
   *
   * Call once before reading application settings or creating application services and clients. It first validates
   * all fields and throws without setting any when one name is reserved or one value is invalid, so it never sets
   * platform, SDK, AWS, process-loader, toolchain, trust-root, or proxy variables. Errors never contain values.
   */
  apply(environment: NodeJS.ProcessEnv = process.env): void {
    for (const [name, value] of this.#values) {
      if (!isApplicationEnvironmentName(name) || !isValidEnvironmentValueEncoding(value)) throw new Error('invalid application environment');
    }
    for (const [name, value] of this.#values) environment[name] = value;
  }

  /** Redact all values. */
  toString(): string { return `projectconfig.ApplicationEnvironment{${this.#values.size} fields}`; }

  /** Redact all values in Node inspection and console output. */
  [inspect.custom](): string { return this.toString(); }

  /** Refuse JSON transport of resolved secrets. */
  toJSON(): never { throw new TypeError(privateEnvironmentSerializationMessage); }
}

/**
 * Validate declarations and return independently sorted copies with sorted enums.
 *
 * Accepts at most 128 unique names and 128 unique enum values per field. Enum values must each satisfy the field's
 * `minLength` and the 32768-byte UTF-8 limit, and a secret field cannot declare an enum. Names sort in UTF-8 byte
 * order, matching Go's `sort.Strings`. Throws a value-free error for any invalid declaration.
 */
export function validateAndSortEnvironmentDeclarations(declarations: readonly EnvironmentDeclaration[]): ValidatedEnvironmentDeclaration[] {
  if (!Array.isArray(declarations)) throw new TypeError('application environment declaration is invalid');
  if (declarations.length > MaximumEnvironmentFields) throw new Error('application environment exceeds field limit');
  const names = new Set<string>();
  const result = declarations.map(declaration => {
    const field = declarationWithZeroValues(declaration);
    if (!field || !isApplicationEnvironmentName(field.name) || names.has(field.name) || field.minLength < 0 || field.minLength > MaximumEnvironmentValueBytes ||
      field.enum.length > MaximumEnvironmentFields || (field.secret && field.enum.length > 0)) throw new Error('application environment declaration is invalid');
    names.add(field.name);
    const sortedEnum = [...field.enum].sort(compareUTF8);
    sortedEnum.forEach((value, index) => {
      if (!isValidEnvironmentValueEncoding(value) || codePointLength(value) < field.minLength || (index > 0 && value === sortedEnum[index - 1])) throw new Error('application environment enum is invalid');
    });
    return { ...field, enum: sortedEnum };
  });
  return result.sort((left, right) => compareUTF8(left.name, right.name));
}

/**
 * Return sorted plain objects for canonical JSON semantic digests shared with manifest readers.
 *
 * Every object has `enum`, `minLength`, `name`, `required`, and `secret` in that key order; an absent enum is `[]`.
 */
export function canonicalEnvironmentDeclarations(declarations: readonly EnvironmentDeclaration[]): Array<{ enum: string[]; minLength: number; name: string; required: boolean; secret: boolean }> {
  return validateAndSortEnvironmentDeclarations(declarations).map(field => ({ enum: [...field.enum], minLength: field.minLength, name: field.name, required: field.required, secret: field.secret }));
}

/**
 * Check one value against a previously validated declaration: encoding, 32768-byte limit, code point `minLength`,
 * and enum membership. Error messages contain the field name only, never the value.
 */
export function validateEnvironmentValue(field: ValidatedEnvironmentDeclaration, value: string): void {
  if (!isValidEnvironmentValueEncoding(value) || codePointLength(value) < field.minLength) throw new Error(`application environment ${field.name} has invalid length or encoding`);
  if (field.enum.length > 0 && !field.enum.includes(value)) throw new Error(`application environment ${field.name} is not an allowed value`);
}

function declarationWithZeroValues(declaration: EnvironmentDeclaration): ValidatedEnvironmentDeclaration | undefined {
  if (declaration === null || typeof declaration !== 'object') return undefined;
  const { name, required = false, secret = false, minLength = 0 } = declaration;
  const enumValues = declaration.enum ?? [];
  if (typeof name !== 'string' || typeof required !== 'boolean' || typeof secret !== 'boolean' || !Number.isSafeInteger(minLength) ||
    !Array.isArray(enumValues) || !enumValues.every(value => typeof value === 'string')) return undefined;
  return { name, required, secret, minLength, enum: enumValues };
}

function codePointLength(value: string): number { return Array.from(value).length; }

function compareUTF8(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, 'utf8'), Buffer.from(right, 'utf8'));
}
