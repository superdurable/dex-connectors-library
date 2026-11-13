// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable } from 'node:stream';
import { inspect } from 'node:util';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GetBucketVersioningCommand, GetObjectCommand, S3Client } from '@aws-sdk/client-s3';
import {
  ApplicationEnvironment, canonicalEnvironmentDeclarations, isApplicationEnvironmentName, validateAndSortEnvironmentDeclarations, validateEnvironmentValue,
} from '../src/projectconfig/application_environment.js';
import { LoadedProject, loadFromEnvironment } from '../src/projectconfig/environment.js';
import { ConfigurationSchema, scopePrefix, type ApplicationSecretRef, type Configuration, type Scope } from '../src/projectconfig/model.js';
import { CredentialBrokerClient, CredentialBrokerRejectedError, CredentialBrokerUnavailableError } from '../src/projectconfig/broker.js';
import { ConfigurationStore, InMemoryObjectStore, OutcomeUnknownError, ProjectObject, digestBytes, type ObjectStore } from '../src/projectconfig/storage.js';

const scope: Scope = { projectId: 'project-1', kind: 'preview' };
const secretValue = 'inert<&>  secret';
const paths: string[] = [];
afterEach(async () => { await Promise.all(paths.splice(0).map(path => rm(path, { recursive: true, force: true }))); });

async function projectWithEnvironment(objects: ObjectStore, environment: Configuration['environment']): Promise<LoadedProject> {
  const store = new ConfigurationStore(objects, scope);
  const written = await store.writeConfiguration(0, { connections: [], triggerBindings: [], operationConfigurations: [], environment });
  const snapshot = await store.freezeConfiguration(written.revision);
  return new LoadedProject(await store.readSnapshot(snapshot), snapshot, objects, undefined, undefined);
}

function expectNoSecret(error: unknown, ...secrets: string[]): void {
  expect(error).toBeInstanceOf(Error);
  for (const secret of secrets) {
    expect((error as Error).message).not.toContain(secret);
    expect(String((error as Error).stack)).not.toContain(secret);
    expect(inspect(error)).not.toContain(secret);
  }
}

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try { await promise; } catch (error: unknown) { return error; }
  throw new Error('expected a rejection');
}

function secretRef(name: string, identifier = '0123456789abcdef0123456789abcdef', version = 'v1', digest = `sha256:${'d'.repeat(64)}`, prefix = `${scopePrefix(scope)}/app-secrets/`): ApplicationSecretRef {
  return { key: `${prefix}${name}/${identifier}`, version, digest };
}

function documentWithEnvironment(environment: unknown): unknown {
  return { schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, connections: [], triggerBindings: [], operationConfigurations: [], environment };
}

describe('application environment', () => {
  it('resolves an exact pinned secret written with Go-compatible bytes', async () => {
    const objects = new InMemoryObjectStore();
    const store = new ConfigurationStore(objects, scope);
    const reference = await store.createApplicationSecret('WEBHOOK_SECRET', secretValue);
    expect(reference.key).toMatch(/^projects\/project-1\/preview\/app-secrets\/WEBHOOK_SECRET\/[a-f0-9]{32}$/);
    const privateObject = await objects.readObject(reference.key, reference.version);
    expect(Buffer.from(privateObject.contents).toString()).toBe('{"scope":{"projectId":"project-1","kind":"preview"},"name":"WEBHOOK_SECRET","value":"inert\\u003c\\u0026\\u003e\\u2028\\u2029secret"}');
    expect(digestBytes(privateObject.contents)).toBe(reference.digest);
    const project = await projectWithEnvironment(objects, { WEBHOOK_SECRET: { secretRef: reference }, APP_LABEL: { value: '' } });
    const environment = await project.resolveApplicationEnvironment();
    expect(environment.size).toBe(2);
    expect(environment.lookup('WEBHOOK_SECRET')).toBe(secretValue);
    expect(environment.lookup('APP_LABEL')).toBe('');
    expect(environment.lookup('MISSING')).toBeUndefined();

    await objects.compareAndSwapObject(reference.key, privateObject.etag, Buffer.from(JSON.stringify({ scope, name: 'WEBHOOK_SECRET', value: 'rotated' })));
    expect((await project.resolveApplicationEnvironment()).lookup('WEBHOOK_SECRET')).toBe(secretValue);
  });

  it('rejects reserved secret names, other field names, and other scopes', async () => {
    const objects = new InMemoryObjectStore();
    const store = new ConfigurationStore(objects, scope);
    const reference = await store.createApplicationSecret('WEBHOOK_SECRET', secretValue);
    for (const name of ['DEX_CONFIG_KEY', 'DEX_CREDENTIAL_BROKER_TOKEN', 'PATH', 'lowercase']) {
      expectNoSecret(await rejection(store.createApplicationSecret(name, 'private-value')), 'private-value');
    }
    expectNoSecret(await rejection(store.createApplicationSecret('APP_KEY', 'private\0value')), 'private');
    expectNoSecret(await rejection(store.createApplicationSecret('APP_KEY', 'a'.repeat(32769))), 'a'.repeat(64));
    await expect(projectWithEnvironment(objects, { OTHER_SECRET: { secretRef: reference } })).rejects.toThrow('application secret reference differs from scope');
    expect(ConfigurationSchema.safeParse(documentWithEnvironment({ WEBHOOK_SECRET: { secretRef: { ...reference, key: reference.key.replace('project-1', 'other') } } })).success).toBe(false);
    const otherScopeStore = new ConfigurationStore(objects, { projectId: 'other', kind: 'preview' });
    await expect(otherScopeStore.resolveApplicationEnvironment(ConfigurationSchema.parse(documentWithEnvironment({ WEBHOOK_SECRET: { secretRef: reference } })))).rejects.toThrow('application environment scope differs');
  });

  it('verifies version, digest, scope, name, and strict object shape without leaking the secret', async () => {
    const reference = secretRef('APP_KEY');
    let contents = Buffer.from('');
    let observedVersion = 'v1';
    const requestedVersions: string[] = [];
    const objects: ObjectStore = {
      async readObject(key, version = '') { requestedVersions.push(version); return new ProjectObject(key, observedVersion, 'etag', contents); },
      async createObject() { throw new Error('unused'); }, async compareAndSwapObject() { throw new Error('unused'); },
    };
    const store = new ConfigurationStore(objects, scope);
    const resolve = async (bytes: string, digest = digestBytes(Buffer.from(bytes))) => {
      contents = Buffer.from(bytes);
      return store.resolveApplicationEnvironment(ConfigurationSchema.parse(documentWithEnvironment({ APP_KEY: { secretRef: { ...reference, digest } } })));
    };
    const valid = JSON.stringify({ scope, name: 'APP_KEY', value: 'sk-private-1' });
    expect((await resolve(valid)).lookup('APP_KEY')).toBe('sk-private-1');
    expect(requestedVersions).toEqual(['v1']);
    const invalidObjects = [
      JSON.stringify({ scope: { projectId: 'other', kind: 'preview' }, name: 'APP_KEY', value: 'sk-private-1' }),
      JSON.stringify({ scope: { ...scope, kind: 'live' }, name: 'APP_KEY', value: 'sk-private-1' }),
      JSON.stringify({ scope, name: 'OTHER_KEY', value: 'sk-private-1' }),
      JSON.stringify({ scope, name: 'APP_KEY', value: 'sk-private-1', extra: true }),
      JSON.stringify({ scope: { ...scope, extra: true }, name: 'APP_KEY', value: 'sk-private-1' }),
      JSON.stringify({ scope, name: 'APP_KEY', value: 'sk-private-1\0' }),
      `${valid} {}`,
      '{"value":"sk-private-1"',
      'sk-private-1',
    ];
    for (const bytes of invalidObjects) {
      const error = await rejection(resolve(bytes));
      expect((error as Error).message).toBe('application secret integrity or scope differs');
      expectNoSecret(error, 'sk-private-1');
    }
    expectNoSecret(await rejection(resolve(valid, `sha256:${'0'.repeat(64)}`)), 'sk-private-1');
    observedVersion = 'v2';
    expectNoSecret(await rejection(resolve(valid)), 'sk-private-1');
  });

  it('replaces unpaired surrogates as Go decoding does', async () => {
    const objects = new InMemoryObjectStore();
    const store = new ConfigurationStore(objects, scope);
    const key = `${scopePrefix(scope)}/app-secrets/APP_KEY/${'a'.repeat(32)}`;
    const contents = Buffer.from('{"scope":{"projectId":"project-1","kind":"preview"},"name":"APP_KEY","value":"x\\ud800"}');
    const object = await objects.createObject(key, contents);
    const configuration = ConfigurationSchema.parse(JSON.parse('{"schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2","scope":{"projectId":"project-1","kind":"preview"},"revision":1,"connections":[],"triggerBindings":[],"operationConfigurations":[],"environment":{"APP_LABEL":{"value":"\\udc00y"},"APP_KEY":{"secretRef":' +
      JSON.stringify({ key, version: object.version, digest: digestBytes(contents) }) + '}}}') as unknown);
    const environment = await store.resolveApplicationEnvironment(configuration);
    expect(environment.lookup('APP_LABEL')).toBe('�y');
    expect(environment.lookup('APP_KEY')).toBe('x�');
  });

  it('reconciles an unknown secret write by exact bytes', async () => {
    const memory = new InMemoryObjectStore();
    let shouldCorrupt = false;
    const objects: ObjectStore = {
      readObject: (key, version) => memory.readObject(key, version),
      async createObject(key, contents) {
        await memory.createObject(key, shouldCorrupt ? Buffer.from('{"other":true}') : contents);
        throw new OutcomeUnknownError();
      },
      compareAndSwapObject: (key, etag, contents) => memory.compareAndSwapObject(key, etag, contents),
    };
    const store = new ConfigurationStore(objects, scope);
    const reference = await store.createApplicationSecret('APP_KEY', 'sk-private-2');
    expect((await memory.readObject(reference.key, reference.version)).key).toBe(reference.key);
    shouldCorrupt = true;
    const error = await rejection(store.createApplicationSecret('APP_KEY', 'sk-private-2'));
    expect(error).toBeInstanceOf(OutcomeUnknownError);
    expectNoSecret(error, 'sk-private-2');
  });

  it('redacts values from formatting and refuses serialization', async () => {
    const environment = new ApplicationEnvironment(new Map([['APP_KEY', 'sk-private-3'], ['APP_LABEL', 'ordinary-private']]));
    const renderings = [
      environment.toString(), `${environment}`, String(environment), inspect(environment), inspect({ nested: [environment] }, { depth: 5 }),
      inspect(environment, { customInspect: false, showHidden: true }), JSON.stringify(Object.entries(environment)), JSON.stringify(structuredClone(environment)),
    ];
    expect(renderings[0]).toBe('projectconfig.ApplicationEnvironment{2 fields}');
    for (const rendering of renderings) {
      expect(rendering).not.toContain('sk-private-3');
      expect(rendering).not.toContain('ordinary-private');
    }
    expect(() => JSON.stringify(environment)).toThrow('private application environment cannot be serialized');
    expect(() => JSON.stringify({ nested: { environment } })).toThrow('private application environment cannot be serialized');
    expect(Object.keys(environment)).toEqual([]);
  });

  it('applies only application-owned names and refuses reserved names before setting any', () => {
    const target: NodeJS.ProcessEnv = { PATH: '/usr/bin' };
    new ApplicationEnvironment(new Map([['APP_KEY', 'sk-private-4'], ['APP_LABEL', '']])).apply(target);
    expect(target).toEqual({ PATH: '/usr/bin', APP_KEY: 'sk-private-4', APP_LABEL: '' });
    for (const reserved of ['PATH', 'NODE_OPTIONS', 'DEX_PROJECT_ID', 'AWS_REGION', 'LD_PRELOAD', 'GOFLAGS', 'GIT_DIR', 'SUPERVERSE_TOKEN', 'HTTPS_PROXY', 'lower']) {
      const untouched: NodeJS.ProcessEnv = { PATH: '/usr/bin' };
      const error = (() => { try { new ApplicationEnvironment(new Map([['APP_FIRST', 'sk-private-5'], [reserved, '/tmp/evil']])).apply(untouched); } catch (caught: unknown) { return caught; } return undefined; })();
      expect((error as Error).message).toBe('invalid application environment');
      expectNoSecret(error, 'sk-private-5', '/tmp/evil');
      expect(untouched).toEqual({ PATH: '/usr/bin' });
    }
    const untouched: NodeJS.ProcessEnv = {};
    expect(() => new ApplicationEnvironment(new Map([['APP_FIRST', 'ok'], ['APP_KEY', 'private\0value']])).apply(untouched)).toThrow('invalid application environment');
    expect(() => new ApplicationEnvironment(new Map([['APP_KEY', 'a'.repeat(32769)]])).apply(untouched)).toThrow('invalid application environment');
    expect(untouched).toEqual({});
  });

  it('applies to process.env by default', () => {
    const name = 'SDKTS_APPLICATION_ENVIRONMENT_TEST';
    try {
      new ApplicationEnvironment(new Map([[name, 'applied']])).apply();
      expect(process.env[name]).toBe('applied');
    } finally { delete process.env[name]; }
  });

  it('matches Go reserved-name rules', () => {
    const accepted = ['A', 'APP_KEY', 'A1', 'A'.repeat(128), 'AWS', 'DEX', 'GIT', 'HOMEX'];
    const rejected = ['', 'a', '_A', '1A', 'A'.repeat(129), 'A-B', 'É', 'http_proxy', 'PATH', 'HOME', 'PORT', 'HOST', 'NODE_OPTIONS', 'SSL_CERT_FILE', 'SSL_CERT_DIR',
      'PUBLIC_BASE_URL', 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'NO_PROXY', 'GO', 'GOOD', 'GOPATH', 'DEX_X', 'DEX_CREDENTIAL_BROKER_TOKEN', 'AWS_X', 'LD_PRELOAD', 'GIT_DIR', 'SUPERVERSE_X'];
    expect(accepted.filter(name => !isApplicationEnvironmentName(name))).toEqual([]);
    expect(rejected.filter(name => isApplicationEnvironmentName(name))).toEqual([]);
  });

  it('accepts and rejects the same configuration environments as Go', () => {
    const fields = (count: number) => Object.fromEntries(Array.from({ length: count }, (_, index) => [`APP_${String(index).padStart(3, '0')}`, { value: 'x' }]));
    const accepted: Record<string, unknown> = {
      ordinary: { APP_LABEL: { value: 'x' } }, emptyOrdinary: { APP_LABEL: { value: '' } }, secret: { APP_KEY: { secretRef: secretRef('APP_KEY') } },
      nullValueWithSecret: { APP_KEY: { value: null, secretRef: secretRef('APP_KEY') } }, valueWithNullSecret: { APP_LABEL: { value: 'x', secretRef: null } },
      value32768Bytes: { APP_LABEL: { value: 'a'.repeat(32768) } }, euro32766Bytes: { APP_LABEL: { value: '€'.repeat(10922) } },
      fields128: fields(128), empty: {}, absent: undefined, nullEnvironment: null,
    };
    const rejected: Record<string, unknown> = {
      both: { APP_KEY: { value: 'x', secretRef: secretRef('APP_KEY') } }, neither: { APP_KEY: {} }, bothNull: { APP_KEY: { value: null, secretRef: null } }, nullEntry: { APP_KEY: null },
      hex31: { APP_KEY: { secretRef: secretRef('APP_KEY', '0'.repeat(31)) } }, hex33: { APP_KEY: { secretRef: secretRef('APP_KEY', '0'.repeat(33)) } },
      hexUpper: { APP_KEY: { secretRef: secretRef('APP_KEY', 'A'.repeat(32)) } }, versionNull: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, 'null') } },
      versionEmpty: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, '') } }, versionNewline: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, 'a\nb') } },
      versionNUL: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, 'a\0b') } }, digestUpper: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, undefined, `sha256:${'D'.repeat(64)}`) } },
      digestShort: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, undefined, `sha256:${'d'.repeat(63)}`) } },
      otherProject: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, undefined, undefined, 'projects/other/preview/app-secrets/') } },
      otherKind: { APP_KEY: { secretRef: secretRef('APP_KEY', undefined, undefined, undefined, 'projects/project-1/live/app-secrets/') } },
      otherName: { APP_KEY: { secretRef: secretRef('OTHER_KEY') } }, valueNUL: { APP_LABEL: { value: 'a\0b' } }, value32769Bytes: { APP_LABEL: { value: 'a'.repeat(32769) } },
      euro32769Bytes: { APP_LABEL: { value: '€'.repeat(10923) } }, surrogates32769Bytes: { APP_LABEL: { value: '\ud800'.repeat(10923) } }, fields129: fields(129),
      reservedPATH: { PATH: { value: 'x' } }, reservedDEX: { DEX_PROJECT_ID: { value: 'x' } }, reservedGOOD: { GOOD: { value: 'x' } }, lowercase: { app_label: { value: 'x' } },
      extraField: { APP_LABEL: { value: 'x', extra: 1 } }, secretRefExtraField: { APP_KEY: { secretRef: { ...secretRef('APP_KEY'), extra: 1 } } }, valueNumber: { APP_LABEL: { value: 1 } },
    };
    expect(Object.entries(accepted).filter(([, environment]) => !ConfigurationSchema.safeParse(documentWithEnvironment(environment)).success).map(([name]) => name)).toEqual([]);
    expect(Object.entries(rejected).filter(([, environment]) => ConfigurationSchema.safeParse(documentWithEnvironment(environment)).success).map(([name]) => name)).toEqual([]);
    const prototypeKey = JSON.parse(`{"schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2","scope":${JSON.stringify(scope)},"revision":1,"connections":[],"triggerBindings":[],"operationConfigurations":[],"environment":{"__proto__":{"value":"x"}}}`) as unknown;
    expect(ConfigurationSchema.safeParse(prototypeKey).success).toBe(false);
    const issue = ConfigurationSchema.safeParse(documentWithEnvironment({ APP_LABEL: { value: 'sk-private-6\0' } }));
    expect(JSON.stringify(issue.error?.issues.map(entry => entry.message))).not.toContain('sk-private-6');
  });

  it('validates and sorts declarations as Go does', () => {
    const sorted = canonicalEnvironmentDeclarations([
      { name: 'B_FIELD', required: true, secret: false, minLength: 1, enum: ['z', '', '\u{1F600}', 'a'] },
      { name: 'A_FIELD', secret: true },
      { name: 'C_FIELD', enum: null },
    ]);
    expect(JSON.stringify(sorted)).toBe(JSON.stringify([
      { enum: [], minLength: 0, name: 'A_FIELD', required: false, secret: true },
      { enum: ['a', 'z', '', '\u{1F600}'], minLength: 1, name: 'B_FIELD', required: true, secret: false },
      { enum: [], minLength: 0, name: 'C_FIELD', required: false, secret: false },
    ]));
    const input = [{ name: 'A_FIELD', enum: ['b', 'a'] }];
    validateAndSortEnvironmentDeclarations(input);
    expect(input[0]?.enum).toEqual(['b', 'a']);
    expect(validateAndSortEnvironmentDeclarations([{ name: 'A_FIELD', minLength: 32768 }])[0]?.minLength).toBe(32768);
    expect(validateAndSortEnvironmentDeclarations([{ name: 'A_FIELD', minLength: 2, enum: ['\u{1F600}\u{1F600}'] }])[0]?.enum).toEqual(['\u{1F600}\u{1F600}']);
    expect(validateAndSortEnvironmentDeclarations([{ name: 'A_FIELD', enum: [''] }])[0]?.enum).toEqual(['']);
    expect(validateAndSortEnvironmentDeclarations(Array.from({ length: 128 }, (_, index) => ({ name: `F_${String(index).padStart(3, '0')}` })))).toHaveLength(128);
    expect(validateAndSortEnvironmentDeclarations([{ name: 'A_FIELD', enum: Array.from({ length: 128 }, (_, index) => `v${index}`) }])[0]?.enum).toHaveLength(128);
    expect(() => validateAndSortEnvironmentDeclarations(Array.from({ length: 129 }, (_, index) => ({ name: `F_${String(index).padStart(3, '0')}` })))).toThrow('application environment exceeds field limit');
    const invalidDeclarations = [
      [{ name: 'A_FIELD' }, { name: 'A_FIELD' }], [{ name: 'PATH' }], [{ name: 'A_FIELD', minLength: -1 }], [{ name: 'A_FIELD', minLength: 32769 }],
      [{ name: 'A_FIELD', minLength: 1.5 }], [{ name: 'A_FIELD', secret: true, enum: ['x'] }], [{ name: 'A_FIELD', enum: Array.from({ length: 129 }, (_, index) => `v${index}`) }],
    ];
    for (const declarations of invalidDeclarations) expect(() => validateAndSortEnvironmentDeclarations(declarations)).toThrow('application environment declaration is invalid');
    for (const declarations of [[{ name: 'A_FIELD', enum: ['x', 'x'] }], [{ name: 'A_FIELD', minLength: 2, enum: ['é'] }], [{ name: 'A_FIELD', enum: ['a\0'] }]]) {
      expect(() => validateAndSortEnvironmentDeclarations(declarations)).toThrow('application environment enum is invalid');
    }
  });

  it('validates values by code points and enum membership without exposing values', () => {
    const [field] = validateAndSortEnvironmentDeclarations([{ name: 'A_FIELD', minLength: 2 }]);
    const [choice] = validateAndSortEnvironmentDeclarations([{ name: 'B_FIELD', enum: ['a', 'b'] }]);
    expect(() => validateEnvironmentValue(field!, '\u{1F600}')).toThrow('application environment A_FIELD has invalid length or encoding');
    expect(() => validateEnvironmentValue(field!, 'éé')).not.toThrow();
    expect(() => validateEnvironmentValue(field!, '')).toThrow('invalid length or encoding');
    expect(() => validateEnvironmentValue(field!, `${'a'.repeat(32769)}`)).toThrow('invalid length or encoding');
    expect(() => validateEnvironmentValue(choice!, 'b')).not.toThrow();
    expect(() => validateEnvironmentValue(choice!, 'sk-private-7')).toThrow('application environment B_FIELD is not an allowed value');
  });

  it('validates configured fields against declarations', async () => {
    const objects = new InMemoryObjectStore();
    const store = new ConfigurationStore(objects, scope);
    const reference = await store.createApplicationSecret('REVIEW_API_KEY', 'sk-private-8');
    const declarations = [
      { name: 'REVIEW_API_KEY', required: true, secret: true, minLength: 12 },
      { name: 'SLACK_REVIEW_CHANNEL', required: true, enum: ['#reviews', '#ops'] },
      { name: 'OPTIONAL_LABEL' },
    ];
    const configurationWith = (environment: Configuration['environment']) => ConfigurationSchema.parse(documentWithEnvironment(environment));
    await store.validateApplicationEnvironment(declarations, configurationWith({ REVIEW_API_KEY: { secretRef: reference }, SLACK_REVIEW_CHANNEL: { value: '#ops' } }));
    const failures: Array<[Configuration['environment'], string]> = [
      [{ REVIEW_API_KEY: { secretRef: reference } }, 'application environment SLACK_REVIEW_CHANNEL is required'],
      [{ REVIEW_API_KEY: { value: 'sk-private-8' }, SLACK_REVIEW_CHANNEL: { value: '#ops' } }, 'application environment REVIEW_API_KEY classification changed'],
      [{ REVIEW_API_KEY: { secretRef: reference }, SLACK_REVIEW_CHANNEL: { value: 'sk-private-8' } }, 'application environment SLACK_REVIEW_CHANNEL is not an allowed value'],
      [{ REVIEW_API_KEY: { secretRef: reference }, SLACK_REVIEW_CHANNEL: { value: '#ops' }, UNDECLARED: { value: 'sk-private-8' } }, 'application environment contains undeclared fields'],
    ];
    for (const [environment, message] of failures) {
      const error = await rejection(store.validateApplicationEnvironment(declarations, configurationWith(environment)));
      expect((error as Error).message).toBe(message);
      expectNoSecret(error, 'sk-private-8');
    }
    const shortReference = await store.createApplicationSecret('REVIEW_API_KEY', 'sk-short');
    const error = await rejection(store.validateApplicationEnvironment(declarations, configurationWith({ REVIEW_API_KEY: { secretRef: shortReference }, SLACK_REVIEW_CHANNEL: { value: '#ops' } })));
    expect((error as Error).message).toBe('application environment REVIEW_API_KEY has invalid length or encoding');
    expectNoSecret(error, 'sk-short');
  });

  it('resolves zero fields for a local Dex Web connections file', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-application-environment-'));
    paths.push(directory);
    const path = join(directory, 'connections.json');
    await writeFile(path, JSON.stringify({ schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [] }), { mode: 0o600 });
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    const environment = await project.resolveApplicationEnvironment();
    expect(environment.size).toBe(0);
    const target: NodeJS.ProcessEnv = {};
    environment.apply(target);
    expect(target).toEqual({});
  });

  it('resolves and applies a hosted snapshot loaded from the environment', async () => {
    const kmsKeyId = 'arn:aws:kms:us-east-1:123456789012:key/abc123';
    const secretKey = `${scopePrefix(scope)}/app-secrets/REVIEW_API_KEY/${'b'.repeat(32)}`;
    const secretBytes = Buffer.from(JSON.stringify({ scope, name: 'REVIEW_API_KEY', value: 'sk-private-9' }));
    const configuration = documentWithEnvironment({
      REVIEW_API_KEY: { secretRef: { key: secretKey, version: 'secret-v1', digest: digestBytes(secretBytes) } },
      SLACK_REVIEW_CHANNEL: { value: '#reviews' },
    });
    const configurationBytes = Buffer.from(JSON.stringify(configuration));
    const requests: Array<{ Key?: string | undefined; VersionId?: string | undefined }> = [];
    const send = vi.spyOn(S3Client.prototype, 'send').mockImplementation(async command => {
      if (command instanceof GetBucketVersioningCommand) return { Status: 'Enabled' } as never;
      if (command instanceof GetObjectCommand) {
        requests.push({ Key: command.input.Key, VersionId: command.input.VersionId });
        const isSecret = command.input.Key === `prefix/${secretKey}`;
        return { VersionId: command.input.VersionId, ETag: 'etag', Body: Readable.from([isSecret ? secretBytes : configurationBytes]), ServerSideEncryption: 'aws:kms', SSEKMSKeyId: kmsKeyId } as never;
      }
      throw new Error('unexpected command');
    });
    try {
      const project = await loadFromEnvironment({ DEX_PROJECT_ID: scope.projectId, DEX_PROJECT_SCOPE: scope.kind, DEX_PROJECT_STORAGE_BUCKET: 'private-bucket',
        DEX_PROJECT_STORAGE_PREFIX: 'prefix', DEX_PROJECT_STORAGE_KMS_KEY_ARN: kmsKeyId, DEX_PROJECT_CONFIG_KEY: `${scopePrefix(scope)}/configuration/head`,
        DEX_PROJECT_CONFIG_VERSION: 'config-v1', DEX_PROJECT_CONFIG_DIGEST: digestBytes(configurationBytes) });
      const environment = await project.resolveApplicationEnvironment();
      const target: NodeJS.ProcessEnv = {};
      environment.apply(target);
      expect(target).toEqual({ REVIEW_API_KEY: 'sk-private-9', SLACK_REVIEW_CHANNEL: '#reviews' });
      expect(requests).toEqual([
        { Key: `prefix/${scopePrefix(scope)}/configuration/head`, VersionId: 'config-v1' },
        { Key: `prefix/${secretKey}`, VersionId: 'secret-v1' },
      ]);
    } finally { send.mockRestore(); }
  });
});

describe('hosted credential broker token', () => {
  const liveScope: Scope = { projectId: 'hosted', kind: 'live' };
  const kmsKeyId = 'arn:aws:kms:us-east-1:123456789012:key/abc123';
  const tokenKey = (identifier: string) => `${scopePrefix(liveScope)}/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${identifier.repeat(32)}`;
  const tokenObject = (name: string, value: string, objectScope: Scope = liveScope) => Buffer.from(JSON.stringify({ scope: objectScope, name, value }));
  const configurationBytes = (revision: number, reference: ApplicationSecretRef) => Buffer.from(JSON.stringify({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3', scope: liveScope, revision, triggerBindings: [], operationConfigurations: [],
    connections: [{ connectorId: 'sample', connectionName: 'shared', modulePath: 'sample', provider: 'sample', sharedConnectionId: 'conn-7', configuration: {} }],
    credentialBroker: { endpoint: 'https://broker.example.test/read', tokenSecretRef: reference },
  }));

  async function loadHostedProject(objectsByKey: Map<string, Map<string, Buffer>>, pinnedConfiguration: Buffer): Promise<LoadedProject> {
    vi.spyOn(S3Client.prototype, 'send').mockImplementation(async command => {
      if (command instanceof GetBucketVersioningCommand) return { Status: 'Enabled' } as never;
      if (command instanceof GetObjectCommand) {
        const versions = objectsByKey.get(command.input.Key ?? '');
        const version = command.input.VersionId ?? [...(versions?.keys() ?? [])].at(-1);
        const body = version ? versions?.get(version) : undefined;
        if (!body) throw Object.assign(new Error('missing'), { name: 'NoSuchKey' });
        return { VersionId: version, ETag: `etag-${version}`, Body: Readable.from([body]), ServerSideEncryption: 'aws:kms', SSEKMSKeyId: kmsKeyId } as never;
      }
      throw new Error('unexpected command');
    });
    return loadFromEnvironment({ DEX_PROJECT_ID: liveScope.projectId, DEX_PROJECT_SCOPE: liveScope.kind, DEX_PROJECT_STORAGE_BUCKET: 'private-bucket',
      DEX_PROJECT_STORAGE_KMS_KEY_ARN: kmsKeyId, DEX_PROJECT_CONFIG_KEY: `${scopePrefix(liveScope)}/configuration/head`,
      DEX_PROJECT_CONFIG_VERSION: 'config-v1', DEX_PROJECT_CONFIG_DIGEST: digestBytes(pinnedConfiguration) });
  }

  afterEach(() => { vi.restoreAllMocks(); });

  it('sends the secret value as the bearer token and renews it after 401', async () => {
    const firstToken = tokenObject('DEX_CREDENTIAL_BROKER_TOKEN', 'scope-token-1');
    const secondToken = tokenObject('DEX_CREDENTIAL_BROKER_TOKEN', 'scope-token-2');
    const firstReference = { key: tokenKey('a'), version: 'token-v1', digest: digestBytes(firstToken) };
    const secondReference = { key: tokenKey('b'), version: 'token-v2', digest: digestBytes(secondToken) };
    const pinnedConfiguration = configurationBytes(1, firstReference);
    const objectsByKey = new Map([
      [`${scopePrefix(liveScope)}/configuration/head`, new Map([['config-v1', pinnedConfiguration], ['config-v2', configurationBytes(2, secondReference)]])],
      [firstReference.key, new Map([['token-v1', firstToken]])],
      [secondReference.key, new Map([['token-v2', secondToken]])],
    ]);
    const authorizations: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const authorization = new Headers(init?.headers).get('Authorization') ?? '';
      authorizations.push(authorization);
      if (authorization === 'Bearer scope-token-1') return new Response('', { status: 401 });
      return Response.json({ status: 'READY', revision: 1, credential: { accessToken: 'shared-access' }, expiresAt: new Date(Date.now() + 3600_000).toISOString() });
    });
    const project = await loadHostedProject(objectsByKey, pinnedConfiguration);
    expect(await project.resolveCredential('sample', 'shared')).toEqual({ accessToken: 'shared-access' });
    expect(authorizations).toEqual(['Bearer scope-token-1', 'Bearer scope-token-2']);
  });

  it('refuses a token object with another name or scope without leaking it', async () => {
    const invalidTokens = [
      tokenObject('APP_KEY', 'scope-token-private'),
      tokenObject('DEX_CREDENTIAL_BROKER_TOKEN', 'scope-token-private', { projectId: 'other', kind: 'live' }),
      tokenObject('DEX_CREDENTIAL_BROKER_TOKEN', 'scope-token-private', { ...liveScope, kind: 'preview' }),
    ];
    for (const token of invalidTokens) {
      const reference = { key: tokenKey('a'), version: 'token-v1', digest: digestBytes(token) };
      const pinnedConfiguration = configurationBytes(1, reference);
      const objectsByKey = new Map([[`${scopePrefix(liveScope)}/configuration/head`, new Map([['config-v1', pinnedConfiguration]])], [reference.key, new Map([['token-v1', token]])]]);
      const transport = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => { throw new Error('the broker must not be called'); });
      const project = await loadHostedProject(objectsByKey, pinnedConfiguration);
      const error = await rejection(project.resolveCredential('sample', 'shared'));
      expect((error as Error).message).toBe('application secret integrity or scope differs');
      expectNoSecret(error, 'scope-token-private');
      expect(transport).not.toHaveBeenCalled();
      vi.restoreAllMocks();
    }
  });

  it('keeps the pinned token reference when the configuration head cannot be read during renewal', async () => {
    const firstToken = tokenObject('DEX_CREDENTIAL_BROKER_TOKEN', 'scope-token-1');
    const firstReference = { key: tokenKey('a'), version: 'token-v1', digest: digestBytes(firstToken) };
    const pinnedConfiguration = configurationBytes(1, firstReference);
    const headKey = `${scopePrefix(liveScope)}/configuration/head`;
    const objectsByKey = new Map([[headKey, new Map([['config-v1', pinnedConfiguration]])], [firstReference.key, new Map([['token-v1', firstToken]])]]);
    const authorizations: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      authorizations.push(new Headers(init?.headers).get('Authorization') ?? '');
      return new Response('', { status: 401 });
    });
    const project = await loadHostedProject(objectsByKey, pinnedConfiguration);
    const send = vi.mocked(S3Client.prototype.send);
    const serveObject = send.getMockImplementation()! as unknown as (command: unknown) => Promise<never>;
    const requests: Array<{ Key?: string | undefined; VersionId?: string | undefined }> = [];
    send.mockImplementation(async (command: unknown) => {
      if (command instanceof GetObjectCommand) {
        requests.push({ Key: command.input.Key, VersionId: command.input.VersionId });
        if (command.input.Key === headKey && !command.input.VersionId) throw Object.assign(new Error('transient'), { name: 'TimeoutError' });
      }
      return serveObject(command);
    });
    await expect(project.resolveCredential('sample', 'shared')).rejects.toBeInstanceOf(CredentialBrokerRejectedError);
    expect(authorizations).toEqual(['Bearer scope-token-1']);
    expect(requests).toEqual([
      { Key: firstReference.key, VersionId: 'token-v1' },
      { Key: headKey, VersionId: undefined },
      { Key: firstReference.key, VersionId: 'token-v1' },
    ]);
  });

  it('accepts a Go-written snapshot whose lists are null', async () => {
    // Exact bytes from Go ConfigurationStore.WriteConfiguration with only an application environment.
    const goSnapshot = Buffer.from('{"schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2","scope":{"projectId":"hosted","kind":"live"},"revision":1,"connections":null,"triggerBindings":null,"operationConfigurations":null,"environment":{"SLACK_REVIEW_CHANNEL":{"value":"#reviews"}}}');
    const objectsByKey = new Map([[`${scopePrefix(liveScope)}/configuration/head`, new Map([['config-v1', goSnapshot]])]]);
    const project = await loadHostedProject(objectsByKey, goSnapshot);
    expect(project.configuration.connections).toEqual([]);
    expect(project.configuration.triggerBindings).toEqual([]);
    expect(project.configuration.operationConfigurations).toEqual([]);
    expect((await project.resolveApplicationEnvironment()).lookup('SLACK_REVIEW_CHANNEL')).toBe('#reviews');
    const nullMembers = ConfigurationSchema.parse({ ...JSON.parse(goSnapshot.toString()) as Record<string, unknown>, credentialBroker: null,
      connections: [{ connectorId: 'sample', connectionName: 'main', modulePath: 'sample', provider: 'sample', authMethodId: null, sharedConnectionId: null, configuration: {} }] });
    expect(nullMembers.credentialBroker).toBeUndefined();
    expect(nullMembers.connections[0]?.authMethodId).toBeUndefined();
    expect(ConfigurationSchema.safeParse({ ...JSON.parse(goSnapshot.toString()) as Record<string, unknown>,
      connections: [{ connectorId: 'sample', connectionName: 'main', modulePath: 'sample', provider: 'sample', configuration: null }] }).success).toBe(false);
  });

  it('rejects broker token references that Go rejects', () => {
    const reference = { key: tokenKey('a'), version: 'token-v1', digest: `sha256:${'c'.repeat(64)}` };
    expect(ConfigurationSchema.safeParse(JSON.parse(configurationBytes(1, reference).toString())).success).toBe(true);
    const invalidReferences = [
      { ...reference, version: 'null' }, { ...reference, version: 'a\nb' }, { ...reference, version: 'a\rb' }, { ...reference, version: 'a\0b' },
      { ...reference, key: tokenKey('A') }, { ...reference, key: `${tokenKey('a')}0` }, { ...reference, key: reference.key.replace('/live/', '/preview/') },
      { ...reference, key: reference.key.replace('DEX_CREDENTIAL_BROKER_TOKEN', 'APP_KEY') },
    ];
    for (const invalid of invalidReferences) {
      const result = ConfigurationSchema.safeParse(JSON.parse(configurationBytes(1, invalid).toString()));
      expect(result.success).toBe(false);
    }
    const tooLong = JSON.parse(configurationBytes(1, reference).toString()) as { credentialBroker: { endpoint: string } };
    tooLong.credentialBroker.endpoint = `https://broker.example.test/${'a'.repeat(2048)}`;
    expect(ConfigurationSchema.safeParse(tooLong).success).toBe(false);
  });

  it('decodes a missing or null secret value as the empty string, as Go does', async () => {
    const objects = new InMemoryObjectStore();
    const store = new ConfigurationStore(objects, scope);
    for (const [index, contents] of [JSON.stringify({ scope, name: 'APP_KEY' }), JSON.stringify({ scope, name: 'APP_KEY', value: null })].entries()) {
      const key = `${scopePrefix(scope)}/app-secrets/APP_KEY/${String(index).repeat(32)}`;
      const object = await objects.createObject(key, Buffer.from(contents));
      const configuration = ConfigurationSchema.parse(documentWithEnvironment({ APP_KEY: { secretRef: { key, version: object.version, digest: digestBytes(object.contents) } } }));
      expect((await store.resolveApplicationEnvironment(configuration)).lookup('APP_KEY')).toBe('');
    }
  });
});

describe('credential broker token single flight', () => {
  function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
    let resolve!: (value: T) => void;
    return { promise: new Promise<T>(settle => { resolve = settle; }), resolve };
  }
  const readyAnswer = () => Response.json({ status: 'READY', revision: 1, credential: { accessToken: 'shared-access' } });

  it('reads the token once for concurrent first reads', async () => {
    const tokenRead = deferred<string>();
    const readToken = vi.fn(async () => tokenRead.promise);
    const authorizations: string[] = [];
    const broker = new CredentialBrokerClient('https://broker.example.test/read', readToken, async (_input, init) => {
      authorizations.push(new Headers(init?.headers).get('Authorization') ?? '');
      return readyAnswer();
    });
    const reads = [broker.read('conn-1'), broker.read('conn-2')];
    tokenRead.resolve('scope-token-1');
    await Promise.all(reads);
    expect(readToken).toHaveBeenCalledTimes(1);
    expect(authorizations).toEqual(['Bearer scope-token-1', 'Bearer scope-token-1']);
  });

  it('renews once when concurrent requests are refused and reuses the newer token', async () => {
    const tokenReads: boolean[] = [];
    const refusals = [deferred<Response>(), deferred<Response>()];
    let refusedCount = 0;
    const bothRefused = deferred<void>();
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async (renew = false) => {
      tokenReads.push(renew);
      return renew ? 'scope-token-2' : 'scope-token-1';
    }, async (_input, init) => {
      if (new Headers(init?.headers).get('Authorization') === 'Bearer scope-token-2') return readyAnswer();
      const refusal = refusals[refusedCount++]!;
      if (refusedCount === 2) bothRefused.resolve();
      return refusal.promise;
    });
    const reads = [broker.read('conn-1'), broker.read('conn-2')];
    await bothRefused.promise;
    refusals[0]!.resolve(new Response('', { status: 401 }));
    refusals[1]!.resolve(new Response('', { status: 401 }));
    const answers = await Promise.all(reads);
    expect(answers.map(answer => answer.credential)).toEqual([{ accessToken: 'shared-access' }, { accessToken: 'shared-access' }]);
    expect(tokenReads).toEqual([false, true]);
  });

  it('stops waiting for a stalled token read on cancellation and lets a later call retry', async () => {
    const tokenReads: string[] = [];
    let isStalled = true;
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => {
      tokenReads.push('read');
      return isStalled ? new Promise<string>(() => undefined) : 'scope-token-1';
    }, async () => readyAnswer());
    const holder = new AbortController();
    const waiter = new AbortController();
    const holderRead = broker.read('conn-1', 0, holder.signal);
    const waiterRead = broker.read('conn-2', 0, waiter.signal);
    await vi.waitFor(() => expect(tokenReads).toHaveLength(1));
    waiter.abort();
    await expect(waiterRead).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
    holder.abort();
    await expect(holderRead).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
    isStalled = false;
    expect((await broker.read('conn-3')).credential).toEqual({ accessToken: 'shared-access' });
    expect(tokenReads).toHaveLength(2);
  });
});

describe('credential broker endpoint', () => {
  it('accepts and rejects the same endpoints as Go validateCredentialBrokerEndpoint', () => {
    const accepted = ['https://broker.example.test/read', `https://b.example/${'\u00e9'.repeat(1010)}`, 'https://b.example/?', 'https://b.example/#', 'http://localhost:8080/x',
      'http://broker/', 'http://10.0.0.1/', 'http://[::1]:80/', 'http://foo.svc/', 'https://b.example/ a', 'HTTPS://b.example/', 'https://b.example:/', 'http://169.254.1.1/',
      'https://b.example', 'http://[fe80::1]/', 'http://LOCALHOST/', 'http://host.docker.internal:9/'];
    const rejected = ['https://@broker.example.test/read', 'https://user@b.example/', 'https://user:pw@b.example/', `https://b.example/${'\u00e9'.repeat(1100)}`,
      'https://b.example/?a=1', 'https://b.example/#x', 'http://broker.example.test/', 'http://Foo.SVC/', 'https://b.example:abc/', 'https:///p', 'ftp://b.example/',
      'https://b.example/%zz', 'http://8.8.8.8/', 'https:b.example', 'https://b.example/a\tb',
      // Hosts the WHATWG URL rewrites before the request: percent-encoded, non-canonical IPv4, and IDN.
      'http://public%2eexample%2ecom/read', 'https://b%2eexample/read', 'http://0x08080808/read', 'http://010.0.0.1/read', 'http://0300.0250.0.1/read',
      'http://167772161/read', 'http://10.1/read', 'http://10.0.0.01/read', 'http://b\u00fccher.svc/read', 'https://b\u00fccher.example/read'];
    const isAcceptedByConfiguration = (endpoint: string) => ConfigurationSchema.safeParse({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3', scope,
      revision: 1, connections: [], triggerBindings: [], operationConfigurations: [],
      credentialBroker: { endpoint, tokenSecretRef: secretRef('DEX_CREDENTIAL_BROKER_TOKEN') } }).success;
    const isAcceptedByClient = (endpoint: string) => { try { new CredentialBrokerClient(endpoint, async () => 'token'); return true; } catch { return false; } };
    expect(accepted.filter(endpoint => !isAcceptedByConfiguration(endpoint) || !isAcceptedByClient(endpoint))).toEqual([]);
    expect(rejected.filter(endpoint => isAcceptedByConfiguration(endpoint) || isAcceptedByClient(endpoint))).toEqual([]);
  });

  it('sends the request to the validated normalized URL', async () => {
    const targets: string[] = [];
    const broker = new CredentialBrokerClient('HTTPS://Broker.Example.Test:443/read', async () => 'scope-token-1', async input => {
      targets.push(String(input));
      return Response.json({ status: 'READY', revision: 1, credential: { accessToken: 'shared-access' } });
    });
    await broker.read('conn-1');
    expect(targets).toEqual(['https://broker.example.test/read']);
  });
});
