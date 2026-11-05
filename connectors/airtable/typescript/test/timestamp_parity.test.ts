// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { GoTimeJSONSchema } from '../src/generated/models.js';

type TimestampCase = { input: string; accepted: boolean; output?: string; marshalError?: boolean };
const cases = JSON.parse(readFileSync(new URL('../../../../internal/codegen/testdata/go_time_json_cases.json', import.meta.url), 'utf8')) as TimestampCase[];

describe('Go time.Time JSON input parity', () => {
  it.each(cases)('$input', ({ input, accepted, output, marshalError }) => {
    const parsed = GoTimeJSONSchema.safeParse(input);
    expect(parsed.success).toBe(accepted);
    if (parsed.success && !marshalError) expect(JSON.stringify(parsed.data)).toBe(JSON.stringify(output));
  });
});
