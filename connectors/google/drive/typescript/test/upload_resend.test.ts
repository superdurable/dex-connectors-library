// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DriveClient } from '../src/client.js';
import { UploadFileDefinition } from '../src/generated/definitions.js';
import type { CredentialSource, Credentials } from '../src/generated/models.js';

const callId = '6072a0ce-9ddb-5937-8998-675e48cfa409';
const connection = { provider: 'google', name: 'drive-files' };
const credentials: Credentials = { accessToken: new SecretString('inert-token') };
const createdFile = { id: 'created-1', name: 'report.txt', mimeType: 'text/plain', createdTime: '2026-09-30T12:00:00Z', size: '6', appProperties: { dexIdempotencyKey: callId } };
const refreshDriver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

class AttemptContext {
  marker: unknown;
  heartbeatCount = 0;
  readonly runId = 'run';
  readonly flowId = 'drive-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1) {}
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.heartbeatCount++; this.marker = value; }
}

type UploadOutcome = () => Promise<ProviderResponse>;

/** Answer lookups from stored files and uploads with queued outcomes. */
class FakeDrive {
  readonly files: Array<Record<string, unknown>> = [];
  isLookupHidden = false;
  uploads = 0;
  constructor(readonly outcomes: UploadOutcome[] = []) {}
  readonly transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    if (request.method === 'POST') { this.uploads++; return this.outcomes.shift()!(); }
    const url = new URL(request.url);
    if (url.pathname === '/drive/v3/files') return json(200, { files: this.isLookupHidden ? [] : this.files });
    const file = this.files.find(candidate => url.pathname.endsWith(`/${String(candidate.id)}`));
    return file ? json(200, file) : json(404, { error: { code: 404 } });
  };
  stores(outcome: UploadOutcome): UploadOutcome {
    return async () => { this.files.push(createdFile); return outcome(); };
  }
}

function json(status: number, body: unknown): ProviderResponse {
  return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
}

async function run(fake: FakeDrive, context: AttemptContext, source?: CredentialSource): Promise<string> {
  const client = new DriveClient({ endpoint: 'http://localhost' }, source ?? new StaticCredentialProvider([[connection, credentials]]), { refreshDriver, transport: fake.transport });
  const call = { id: callId, idempotencyKey: callId, connection, operation: UploadFileDefinition.operation, context } as unknown as Call;
  try { return (await runMutation(client.uploadFile(), call, { name: 'report.txt', mimeType: 'text/plain', textContent: 'report' })).branch; }
  catch (error) { if (error instanceof RetryError) return 'retry'; throw error; }
}

function failingSource(error: Error): CredentialSource {
  return { resolve: async () => { throw error; } } as unknown as CredentialSource;
}

describe('Drive uploadFile never resends an accepted upload', () => {
  it('looks a broken 2xx body up on later attempts without resending', async () => {
    const fake = new FakeDrive();
    fake.outcomes.push(fake.stores(async () => { throw new ProviderTransportError(false, 200, {}); }));
    fake.isLookupHidden = true;
    const context = new AttemptContext();
    expect(await run(fake, context)).toBe('retry');
    context.attempt++;
    expect(await run(fake, context)).toBe('retry');
    fake.isLookupHidden = false;
    context.attempt++;
    expect(await run(fake, context)).toBe('uploaded');
    expect(fake.uploads).toBe(1);
  });

  it('selects uncertain at the cap when an accepted upload stays invisible', async () => {
    const fake = new FakeDrive();
    fake.outcomes.push(fake.stores(async () => json(200, { name: 'report.txt' })));
    fake.isLookupHidden = true;
    const context = new AttemptContext();
    const branches: string[] = [];
    for (let attempt = 0; attempt < 3; attempt++, context.attempt++) branches.push(await run(fake, context));
    expect(branches).toEqual(['retry', 'retry', 'uncertain']);
    expect(fake.uploads).toBe(1);
  });

  it('treats a refusal whose body is oversized as an unknown outcome', async () => {
    const fake = new FakeDrive([async () => { throw new BodyTooLargeError(400, {}); }, async () => json(200, createdFile)]);
    const context = new AttemptContext();
    expect(await run(fake, context)).toBe('retry');
    context.attempt++;
    expect(await run(fake, context)).toBe('uploaded');
    expect(fake.uploads).toBe(2);
  });

  it('records canResend for a 429 whose body breaks', async () => {
    const fake = new FakeDrive([async () => { throw new ProviderTransportError(false, 429, {}); }]);
    const context = new AttemptContext();
    expect(await run(fake, context)).toBe('retry');
    expect(JSON.stringify(context.marker)).toContain('canResend');
    expect(fake.uploads).toBe(1);
  });

  it('reads a refusal carrying a file ID back by ID', async () => {
    const fake = new FakeDrive();
    fake.outcomes.push(fake.stores(async () => json(422, createdFile)));
    expect(await run(fake, new AttemptContext())).toBe('uploaded');
    expect(fake.uploads).toBe(1);
  });

  it('looks up before resending after a throttled resend', async () => {
    const fake = new FakeDrive();
    fake.outcomes.push(fake.stores(async () => json(500, { error: { code: 500 } })), async () => json(429, { error: { code: 429 } }));
    fake.isLookupHidden = true;
    const context = new AttemptContext();
    expect(await run(fake, context)).toBe('retry');
    context.attempt++;
    expect(await run(fake, context)).toBe('retry');
    fake.isLookupHidden = false;
    context.attempt++;
    expect(await run(fake, context)).toBe('uploaded');
    expect(fake.uploads).toBe(2);
  });

  it('never records notSent after a possible send and maps reauthorization', async () => {
    const afterPossibleSend = new AttemptContext(2);
    expect(await run(new FakeDrive(), afterPossibleSend, failingSource(new Error('credential store offline')))).toBe('retry');
    expect(afterPossibleSend.heartbeatCount).toBe(0);
    const revoked = failingSource(new ReauthorizationRequiredError());
    expect(await run(new FakeDrive(), new AttemptContext(), revoked)).toBe('defect');
    expect(await run(new FakeDrive(), new AttemptContext(2), revoked)).toBe('uncertain');
  });
});
