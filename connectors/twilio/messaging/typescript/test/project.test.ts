// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, it } from 'vitest';
import { type Call } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { HeartbeatContext } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { newProjectConnection } from '../src/project.js';

const accountSid = 'AC00000000000000000000000000000000';
const messageSid = 'SMfedcba9876543210fedcba9876543210';

it('uses credential auth_method and a non-refreshing provider after 401', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'twilio-project-'));
  const configurationFile = join(directory, 'connections.json');
  try {
    await writeFile(configurationFile, JSON.stringify({
      schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
      connections: [{ connectorId: 'twilio-messaging', connectionName: 'twilio-test',
        modulePath: '@superdurable/connector-twilio-messaging', provider: 'twilio', authMethodId: 'api-key',
        configuration: { accountSid, defaultSender: '+14155550100' },
        credentials: { auth_token: 'auth-token-sentinel-0123456789ab' } }],
    }), { mode: 0o600 });
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: configurationFile });
    const requests: string[] = [];
    const connection = newProjectConnection(project, 'twilio-test', { send: async request => {
      requests.push(request.headers?.Authorization ?? '');
      if (request.method === 'POST') return { status: 401, headers: {}, body: Buffer.from('{"code":20003}') };
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ sid: messageSid, account_sid: accountSid, to: '+14155550123', status: 'queued' })) };
    } });
    const context = new HeartbeatContext(undefined);
    const call = { id: 'd1ffeecf-f887-5bbc-8ca8-1325f0d0700d', idempotencyKey: 'd1ffeecf-f887-5bbc-8ca8-1325f0d0700d',
      connection: { provider: 'twilio', name: 'twilio-test' }, operation: { connectorId: 'twilio-messaging', operationId: 'getMessage' }, context,
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false } as unknown as Call;
    expect(await connection.operations.getMessage().invoke(call, { messageSid })).toMatchObject({ branch: 'found' });
    const rejection = await connection.operations.sendMessage().invoke(call, { to: '+14155550123', body: 'hello' });
    expect(rejection).toMatchObject({ branch: 'providerRejected', failure: { kind: 'AUTHENTICATION', message: 'Twilio credential cannot be refreshed after rejection' } });
    expect(requests).toEqual(Array(2).fill(`Basic ${Buffer.from(`${accountSid}:auth-token-sentinel-0123456789ab`).toString('base64')}`));
  } finally { await rm(directory, { recursive: true, force: true }); }
});
