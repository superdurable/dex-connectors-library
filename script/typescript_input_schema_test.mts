// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// Check published input schemas against the validators that each package exports from its root.

import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

type JSONSchema = { [keyword: string]: any };
type RuntimeValidator = { safeParse(value: unknown): { success: boolean } };

const maximumSampleDepth = 4;

function sampleValue(schema: JSONSchema, root: JSONSchema, depth: number): unknown {
  if (schema.$ref) return sampleValue(root.$defs[schema.$ref.split('/').pop()], root, depth);
  if (schema.anyOf) return sampleValue(schema.anyOf[0], root, depth);
  if (schema.enum) return schema.enum.find((value: unknown) => value !== null && value !== '' && value !== 0);
  const kind = Array.isArray(schema.type) ? schema.type[0] : schema.type;
  if (kind === 'string') {
    if (schema.format === 'date-time') return '2026-01-02T03:04:05Z';
    return schema.contentEncoding ? 'AQID' : 'sample';
  }
  if (kind === 'integer') return 7;
  if (kind === 'number') return 1.5;
  if (kind === 'boolean') return true;
  if (kind === 'array') return [sampleValue(schema.items, root, depth + 1)];
  if (kind === 'object' && schema.properties) {
    const required = new Set<string>(schema.required ?? []);
    return Object.fromEntries(Object.entries<JSONSchema>(schema.properties)
      .filter(([name]) => required.has(name) || depth < maximumSampleDepth)
      .map(([name, property]) => [name, sampleValue(property, root, depth + 1)]));
  }
  if (kind === 'object') return { key: sampleValue(schema.additionalProperties, root, depth + 1) };
  return { opaque: 'value' };
}

function checkOperation(models: Record<string, unknown>, factory: JSONSchema): void {
  const label = factory.operationId as string;
  const validator = models[`${factory.inputType}Schema`] as RuntimeValidator | undefined;
  if (!validator) throw new Error(`${label}: ${factory.inputType}Schema is not exported from the package root`);
  const schema = factory.inputSchema as JSONSchema;
  const sample = sampleValue(schema, schema, 0) as Record<string, unknown>;
  if (!validator.safeParse(sample).success) throw new Error(`${label}: the validator rejected a complete sample`);
  if (validator.safeParse({ ...sample, notAWireField: true }).success) throw new Error(`${label}: the validator accepted an unknown field`);
  const required = new Set<string>(schema.required ?? []);
  for (const name of Object.keys(sample)) {
    const { [name]: _omitted, ...withoutField } = sample;
    const isAccepted = validator.safeParse(withoutField).success;
    if (!required.has(name) && !isAccepted) throw new Error(`${label}: ${name} is optional in the schema but the validator requires it`);
    const isOpaque = Object.keys(schema.properties[name]).every(keyword => keyword === 'description');
    if (required.has(name) && isAccepted && !isOpaque) throw new Error(`${label}: ${name} is required in the schema but the validator accepts its absence`);
  }
}

const packageDirectory = resolve(process.argv[2] ?? '.');
const packageRoot = await import(pathToFileURL(join(packageDirectory, 'src', 'index.ts')).href) as Record<string, unknown>;
const manifest = JSON.parse(readFileSync(join(packageDirectory, 'dex-connector.json'), 'utf8')) as { factories: JSONSchema[] };
for (const factory of manifest.factories) checkOperation(packageRoot, factory);
