// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { strict as assert } from 'node:assert';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import { loadScenarios, scenarioClock } from './connector_scenarios.ts';

const cases = JSON.parse(readFileSync(new URL('../clock_cases.json', import.meta.url), 'utf8'));

test('scenarioClock follows shared UTC grammar', () => {
  for (const clock of cases.valid) {
    const observed = scenarioClock({ name: 'fixed', clock })();
    assert.equal(observed.toISOString().slice(0, 19), clock.slice(0, 19));
  }
  for (const clock of cases.invalid) {
    assert.throws(() => scenarioClock({ name: 'fixed', clock }));
  }
});

test('loadScenarios accepts a clocked fixture', () => {
  const source = JSON.parse(readFileSync(new URL('../../../conformance/connectors/airtable/scenarios.json', import.meta.url), 'utf8'));
  source.scenarios[0].clock = '2026-10-26T12:00:00.120Z';
  const directory = mkdtempSync(join(tmpdir(), 'connector-clock-'));
  try {
    const path = join(directory, 'scenarios.json');
    writeFileSync(path, JSON.stringify(source));
    assert.equal(loadScenarios(pathToFileURL(path))[0].clock, source.scenarios[0].clock);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
