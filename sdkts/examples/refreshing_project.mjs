// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createCall } from '../dist/index.js';
import { loadFromEnvironment, newRefreshingCredentialProvider } from '../dist/projectconfig/index.js';

const directory = await mkdtemp(join(tmpdir(), 'sdkts-project-refresh-'));
const path = join(directory, 'connections.json');
try {
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{ connectorId: 'example', connectionName: 'default', modulePath: 'example',
      provider: 'example', authMethodId: 'oauth2', configuration: {},
      credentials: { accessToken: 'old', refreshToken: 'renewal' },
      credentialExpiresAt: new Date(Date.now() - 1000).toISOString() }],
  }), { mode: 0o600 });
  const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
  const provider = newRefreshingCredentialProvider(
    project.connections, { connectorId: 'example', connectionName: 'default' },
    value => ({ accessToken: String(value.accessToken), refreshToken: String(value.refreshToken) }),
    value => ({ accessToken: value.accessToken, refreshToken: value.refreshToken }),
  );
  const context = { flowId: 'example-flow', stepExecutionId: 'refresh:1', attempt: 1 };
  const call = createCall(context, { provider: 'example', name: 'default' }, { connectorId: 'example', operationId: 'getItem' });
  const credentials = await provider.resolveWithRefresh(call, {
    refreshRequired: () => false,
    refresh: async state => ({
      credentials: { accessToken: 'new', refreshToken: state.credentials.refreshToken },
      expiresAt: new Date(Date.now() + 3600_000),
    }),
  });
  const saved = JSON.parse(await readFile(path, 'utf8'));
  const resolved = await project.resolveCredential('example', 'default');
  if (credentials.accessToken !== 'new' || resolved.accessToken !== 'new' || saved.connections[0].credentials.accessToken !== 'new') throw new Error('project refresh example failed');
} finally {
  await rm(directory, { recursive: true, force: true });
}
