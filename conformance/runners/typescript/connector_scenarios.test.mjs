// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { strict as assert } from 'node:assert';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import { loadScenarios } from './connector_scenarios.ts';

const cases = JSON.parse(readFileSync(new URL('../clock_cases.json', import.meta.url), 'utf8'));

test('loadScenarios validates shared fixed clock cases', () => {
  const source = JSON.parse(readFileSync(new URL('../../../conformance/connectors/airtable/scenarios.json', import.meta.url), 'utf8'));
  const directory = mkdtempSync(join(tmpdir(), 'connector-clock-'));
  try {
    const path = join(directory, 'scenarios.json');
    for (const clock of cases.valid) {
      source.scenarios[0].fixedClock = clock;
      writeFileSync(path, JSON.stringify(source));
      assert.equal(loadScenarios(pathToFileURL(path))[0].fixedClock, clock);
    }
    for (const clock of cases.invalid) {
      source.scenarios[0].fixedClock = clock;
      writeFileSync(path, JSON.stringify(source));
      assert.throws(() => loadScenarios(pathToFileURL(path)), String(clock));
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
