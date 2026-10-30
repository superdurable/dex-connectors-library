// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadFromEnvironment } from '../dist/projectconfig/index.js';

const directory = await mkdtemp(join(tmpdir(), 'sdkts-example-'));
const path = join(directory, 'connections.json');
try {
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{ connectorId: 'example', connectionName: 'default', modulePath: 'example',
      provider: 'example', configuration: {}, credentials: { apiKey: 'test-only' } }],
  }), { mode: 0o600 });
  const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
  const credential = await project.resolveCredential('example', 'default');
  if (credential.apiKey !== 'test-only') throw new Error('project configuration example failed');
} finally {
  await rm(directory, { recursive: true, force: true });
}
