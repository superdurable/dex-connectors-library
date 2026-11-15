// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, beforeEach, expect, it } from 'vitest';
import { newFileIntakeRunner } from '../examples/file-intake/app.js';
import { ConnectionName, FileIntakeFlow, NewFileTriggerBinding, acceptCreatedFile, mapToFlowInput, mapToGetFileInput, resolveFlowId } from '../examples/file-intake/flow/workflow.js';
import { DriveClient, FileCreatedTriggerDefinition, newConnection, newProjectFileTriggerRunner, type Credentials, type FileEvent } from '../src/index.js';

const createdAt = '2026-11-01T12:00:00Z';
const file: FileEvent = { fileId: '1AbC', name: 'Invoice.pdf', mimeType: 'application/pdf', createdTime: createdAt, modifiedTime: createdAt, changeTime: createdAt, isTrashed: false };

it('maps one created file event to one Flow', () => {
  const event = { id: '1AbC@created', occurredAt: new Date(createdAt), payload: file };
  expect(acceptCreatedFile(event)).toBe(true);
  expect(acceptCreatedFile({ ...event, id: '1AbC@modified@1' })).toBe(false);
  expect(resolveFlowId(event)).toBe('google-drive-typescript-file-intake-google-drive-files-1AbC');
  expect(mapToFlowInput(event)).toEqual({ eventId: '1AbC@created', file });
  expect(mapToGetFileInput(mapToFlowInput(event))).toEqual({ fileId: '1AbC' });
});

it('declares the static fileCreated binding with its folder and file type units', () => {
  const reference = { provider: 'google', name: ConnectionName };
  const credentials: Credentials = { accessToken: new SecretString('inert') };
  const flow = new FileIntakeFlow(newConnection(new DriveClient({}, new StaticCredentialProvider([[reference, credentials]])), reference));
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding?.definition).toEqual(FileCreatedTriggerDefinition);
  expect([binding?.connectionName, binding?.bindingName]).toEqual([ConnectionName, NewFileTriggerBinding]);
  expect(binding?.configurationUI?.units.map(unit => [unit.unitId, unit.bindings.map(item => item.jsonPointer)])).toEqual([
    ['folderPicker', ['/folderId', '/folderName']], ['mimeTypePicker', ['/mimeType']],
  ]);
  expect(flow.getFlowType()).toBe('GoogleDriveFileIntakeTypeScript');
});

let directory: string;
beforeEach(async () => { directory = await mkdtemp(join(tmpdir(), 'drive-file-intake-config-')); });
afterEach(async () => { await rm(directory, { recursive: true, force: true }); });

async function loadProject(configuration: unknown) {
  const path = join(directory, 'connections.json');
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{ connectorId: 'google-drive', authMethodId: 'google-oauth', provider: 'google', connectionName: ConnectionName,
      modulePath: 'github.com/superdurable/dex-connectors-library/connectors/google/drive/go', configuration: { endpoint: 'https://drive.example' },
      credentials: { access_token: 'inert', refresh_token: 'inert' } }],
    triggerBindings: [{ connectorId: 'google-drive', connectionName: ConnectionName, triggerName: 'fileCreated', bindingName: NewFileTriggerBinding, configuration }],
  }), { mode: 0o600 });
  await chmod(path, 0o600);
  return loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
}

const inertTarget = { handleTrigger: async () => { throw new Error('no event is delivered'); } };

it.each([
  [{ folderId: 'fld_inbox', mimeType: 'application/pdf', unknown: true }],
  [{ folderId: 7 }],
  [{ folderId: 'https://drive.google.com/drive/folders/x' }],
  [{ mimeType: 'PDF' }],
])('rejects invalid saved configuration %j', async configuration => {
  const project = await loadProject(configuration);
  expect(() => newFileIntakeRunner(project, inertTarget)).toThrow();
});

it('decodes saved configuration and requires routes', async () => {
  const project = await loadProject({ folderId: 'fld_inbox', folderName: 'Inbox', mimeType: 'application/pdf', unused: undefined });
  newFileIntakeRunner(project, inertTarget);
  expect(() => newProjectFileTriggerRunner(project, ConnectionName, {})).toThrow();
  const nulls = await loadProject({ folderId: null, mimeType: null });
  newFileIntakeRunner(nulls, inertTarget);
});
