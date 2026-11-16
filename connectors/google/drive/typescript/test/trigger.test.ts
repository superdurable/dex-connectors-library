// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, TriggerDeliveryAbortedError, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newMemoryCursorStore } from '@superdurable/connectors-sdk/pollingtrigger';
import { afterEach, beforeEach, expect, it } from 'vitest';
import {
  DriveClient, newConnection, newConnectionWithOperations, newFileCreatedTrigger, newFileTrashedTrigger, newFileTriggerRunner, newFileUpdatedTrigger,
  type Connection, type Credentials, type FileEvent, type FileTriggerRunnerConfig,
} from '../src/index.js';
import { DriveChangeLog } from './drive_change_log.js';

const accessToken = 'drive-trigger-test-token';
const reference = { provider: 'google', name: 'drive-files' };
let changeLog: DriveChangeLog;
let connection: Connection;

beforeEach(async () => {
  changeLog = await new DriveChangeLog(accessToken).start();
  const credentials: Credentials = { accessToken: new SecretString(accessToken) };
  connection = newConnection(new DriveClient({ endpoint: changeLog.url, pollIntervalMs: 1000 }, new StaticCredentialProvider([[reference, credentials]]), {
    refreshDriver: { refreshRequired: () => false, refresh: async () => { throw new Error('refresh was not expected'); } },
  }), reference);
});
afterEach(async () => { await changeLog.close(); });

/** A non-durable target that consumes every event. */
class RecordingFileTarget implements TriggerTarget<FileEvent> {
  readonly eventIds: string[] = [];
  async handleTrigger(_context: TriggerContext, event: TriggerEvent<FileEvent>): Promise<void> { this.eventIds.push(event.id); }
}

async function running(runner: { run(context: TriggerContext): Promise<void> }, work: () => Promise<void>): Promise<void> {
  const stop = new AbortController();
  const task = runner.run(newTriggerContext(stop.signal));
  try { await work(); }
  finally { stop.abort(); await expect(task).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); }
}

async function eventually(condition: () => boolean, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error('condition was not met in time');
    await new Promise(resolve => setTimeout(resolve, 20));
  }
}

const hourAgo = () => Date.now() - 3_600_000;

it('shares one feed and routes each binding its events', async () => {
  for (const id of ['old_report', 'old_notes', 'old_draft', 'backlog_upload']) changeLog.put({ id, name: id, mimeType: 'application/pdf', parent: 'folder_a', createdTime: hourAgo(), modifiedTime: hourAgo() });
  const createdEverywhere = new RecordingFileTarget(), createdPDFInA = new RecordingFileTarget(), updatedEverywhere = new RecordingFileTarget(), trashedInA = new RecordingFileTarget();
  const runner = newFileTriggerRunner({
    connection, cursor: newMemoryCursorStore(),
    fileCreatedRoutes: [{ bindingName: 'every-new-file', target: createdEverywhere }, { bindingName: 'new-pdf-in-a', configuration: { folderId: 'folder_a', folderName: 'A', mimeType: 'application/pdf' }, target: createdPDFInA }],
    fileUpdatedRoutes: [{ bindingName: 'every-edit', target: updatedEverywhere }],
    fileTrashedRoutes: [{ bindingName: 'trash-in-a', configuration: { folderId: 'folder_a' }, target: trashedInA }],
  });
  await running(runner, async () => {
    await eventually(() => changeLog.listPages() >= 1);
    changeLog.createFile('new_pdf_a', 'Invoice.pdf', 'application/pdf', 'folder_a');
    changeLog.createFile('new_doc_b', 'Notes', 'application/vnd.google-apps.document', 'folder_b');
    changeLog.createFile('new_pdf_b', 'Other.pdf', 'application/pdf', 'folder_b');
    const editedAt = Date.now();
    changeLog.modifyFile('old_report', editedAt);
    changeLog.touchFile('old_notes');
    changeLog.trashFile('old_draft');
    const wantCreated = ['new_pdf_a@created', 'new_doc_b@created', 'new_pdf_b@created'];
    await eventually(() => createdEverywhere.eventIds.length === 3 && updatedEverywhere.eventIds.length === 1 && trashedInA.eventIds.length === 1);
    expect(createdEverywhere.eventIds).toEqual(wantCreated);
    expect(createdPDFInA.eventIds).toEqual(['new_pdf_a@created']);
    expect(updatedEverywhere.eventIds).toEqual([`old_report@modified@${editedAt}`]);
    expect(trashedInA.eventIds[0]).toMatch(/^old_draft@trashed@\d+$/);
    // Later polls deliver nothing again, and every binding shares one seed and one list per poll.
    const pages = changeLog.listPages();
    await eventually(() => changeLog.listPages() >= pages + 2);
    expect(createdEverywhere.eventIds).toEqual(wantCreated);
    expect(updatedEverywhere.eventIds).toHaveLength(1);
    expect(changeLog.requests('/drive/v3/changes/startPageToken')).toBe(1);
  });
}, 30_000);

it('keys fileTrashed by shared drive trash time and by My Drive change time', async () => {
  changeLog.put({ id: 'shared_file', name: 'Shared', mimeType: 'application/pdf', parent: 'folder_a', driveId: 'drive_1', createdTime: hourAgo(), modifiedTime: hourAgo() });
  changeLog.put({ id: 'my_file', name: 'Mine', mimeType: 'application/pdf', parent: 'folder_a', createdTime: hourAgo(), modifiedTime: hourAgo() });
  const trashed = new RecordingFileTarget();
  await running(newFileTriggerRunner({ connection, cursor: newMemoryCursorStore(), fileTrashedRoutes: [{ bindingName: 'trash', target: trashed }] }), async () => {
    await eventually(() => changeLog.listPages() >= 1);
    changeLog.trashFile('shared_file');
    changeLog.trashFile('my_file');
    await eventually(() => new Set(trashed.eventIds).size === 2);
    // A shared drive file keeps its trashedTime, so a later change repeats its event ID; My Drive has none.
    changeLog.touchFile('shared_file');
    changeLog.touchFile('my_file');
    await eventually(() => new Set(trashed.eventIds).size === 3);
    const pages = changeLog.listPages();
    await eventually(() => changeLog.listPages() >= pages + 2);
    const unique = [...new Set(trashed.eventIds)];
    expect(unique).toHaveLength(3);
    expect([unique.filter(id => /^shared_file@trashed@\d+$/.test(id)).length, unique.filter(id => /^my_file@trashed@\d+$/.test(id)).length]).toEqual([1, 2]);
  });
}, 30_000);

it('reseeds after Drive rejects the stored page token', async () => {
  const created = new RecordingFileTarget();
  changeLog.script({ status: 400 });
  await running(newFileTriggerRunner({ connection, cursor: newMemoryCursorStore(), fileCreatedRoutes: [{ bindingName: 'every-new-file', target: created }] }), async () => {
    await eventually(() => changeLog.requests('/drive/v3/changes/startPageToken') === 2);
    changeLog.createFile('after_reset', 'After.pdf', 'application/pdf', 'folder_a');
    await eventually(() => created.eventIds.length === 1);
    expect(created.eventIds).toEqual(['after_reset@created']);
  });
}, 30_000);

it('pauses on a rate limit and then delivers', async () => {
  const created = new RecordingFileTarget();
  await running(newFileTriggerRunner({ connection, cursor: newMemoryCursorStore(), fileCreatedRoutes: [{ bindingName: 'new', target: created }] }), async () => {
    await eventually(() => changeLog.listPages() >= 1);
    changeLog.script({ status: 429, retryAfter: '1' });
    changeLog.createFile('after_limit', 'After.pdf', 'application/pdf', 'folder_a');
    await eventually(() => created.eventIds.length === 1);
    expect(created.eventIds).toEqual(['after_limit@created']);
    expect(changeLog.requests('/drive/v3/changes/startPageToken')).toBe(1);
  });
}, 30_000);

it('polls one binding with its own cursor through the per-Trigger factory', async () => {
  const created = new RecordingFileTarget();
  const runner = newFileCreatedTrigger({ connection, connectionName: reference.name, bindingName: 'new-docs', configuration: { mimeType: 'application/vnd.google-apps.document' }, target: created });
  await running(runner, async () => {
    await eventually(() => changeLog.listPages() >= 1);
    changeLog.createFile('sheet', 'Budget', 'application/vnd.google-apps.spreadsheet', 'folder_a');
    changeLog.createFile('doc', 'Plan', 'application/vnd.google-apps.document', 'folder_a');
    await eventually(() => created.eventIds.length === 1);
    expect(created.eventIds).toEqual(['doc@created']);
  });
  expect(() => newFileTrashedTrigger({ connection, connectionName: reference.name, bindingName: 'bad', configuration: { mimeType: 'PDF' }, target: created })).toThrow();
  expect(() => newFileUpdatedTrigger({ connection, connectionName: 'other', bindingName: 'edits', configuration: {}, target: created })).toThrow();
}, 30_000);

it('rejects incomplete runner configuration', () => {
  const cursor = newMemoryCursorStore(), target = new RecordingFileTarget();
  const route = { bindingName: 'new', target };
  const operations = connection.operations;
  const mock = newConnectionWithOperations({
    searchFiles: () => operations.searchFiles(), getFile: () => operations.getFile(), readFileText: () => operations.readFileText(), uploadFile: () => operations.uploadFile(),
  }, reference);
  const invalid: Record<string, FileTriggerRunnerConfig> = {
    'mock connection': { connection: mock, cursor, fileCreatedRoutes: [route] },
    'no cursor': { connection, cursor: undefined as never, fileCreatedRoutes: [route] },
    'no routes': { connection, cursor },
    'no target': { connection, cursor, fileCreatedRoutes: [{ bindingName: 'new', target: undefined as never }] },
    'blank binding': { connection, cursor, fileCreatedRoutes: [{ bindingName: '  ', target }] },
    'invalid folder': { connection, cursor, fileCreatedRoutes: [{ bindingName: 'new', target, configuration: { folderId: 'a b' } }] },
    'duplicate binding': { connection, cursor, fileCreatedRoutes: [route, route] },
  };
  for (const [name, config] of Object.entries(invalid)) expect(() => newFileTriggerRunner(config), name).toThrow();
  newFileTriggerRunner({ connection, cursor, fileCreatedRoutes: [route], fileTrashedRoutes: [route] });
});
