// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { ExcelClient } from '../src/client.js';
import { newExcelProjectConnection } from '../src/project.js';

it('uses Go defaults when numeric configuration fields are zero', () => {
  const client = new ExcelClient({ maxResponseBytes: 0, maxCells: 0 },
    new StaticCredentialProvider([]));
  expect(client.graph.maxResponseBytes).toBe(4_194_304);
  expect(client.graph.maxCells).toBe(10_000);
});

it.each(['500', true])('rejects an untyped project cell limit %s', value => {
  const project = { configuration: { connections: [{ connectorId: 'microsoft-excel', connectionName: 'approvals',
    provider: 'microsoft', configuration: { maxCells: value } }] } } as unknown as LoadedProject;
  expect(() => newExcelProjectConnection(project, 'approvals')).toThrow(/maxCells must be an integer/);
});
