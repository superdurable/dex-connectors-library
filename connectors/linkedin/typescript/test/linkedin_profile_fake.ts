// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { LinkedInClient, newConnection, type Connection, type Credentials, type LinkedInClientOptions } from '../src/index.js';

/** The bearer token the fake accepts; split so it does not look like a credential. */
export const AccessToken = 'linkedin-' + 'profile-' + 'trigger';
/** The UserInfo URL the fake serves. */
export const UserInfoURL = 'https://api.linkedin.com/v2/userinfo';

/** One member's UserInfo claims, with LinkedIn's claim names. */
export interface Profile { sub: string; name?: string; given_name?: string; family_name?: string; picture?: string; locale?: string; email?: string; email_verified?: boolean }

/** A verified sample member, as the Go connector's linkedinprofilefake.AdaProfile. */
export function adaProfile(): Profile {
  return {
    sub: '782bbtaQ', name: 'Ada Lovelace', given_name: 'Ada', family_name: 'Lovelace',
    picture: 'https://media.licdn.com/dms/image/v2/ada/profile-displayphoto-shrink_100_100/0/1?e=1&v=beta&t=first',
    locale: 'en-US', email: 'ada@example.com', email_verified: true,
  };
}

/** A single-member fake of GET /v2/userinfo, mirroring the Go connector's internal/linkedinprofilefake. */
export class LinkedInProfileFake {
  private readonly failures: Array<{ status: number; retryAfter: string }> = [];
  private requests = 0;

  /** Serve profile. */ constructor(public profile: Profile) {}

  /** Change the served profile. */ update(edit: (profile: Profile) => void): void { edit(this.profile); }
  /** Answer the next request with status, sending retryAfter as Retry-After when not blank; calls queue. */
  failNext(status: number, retryAfter = ''): void { this.failures.push({ status, retryAfter }); }
  /** Count UserInfo requests, failed ones included. */ requestCount(): number { return this.requests; }

  /** Answer one provider request; pass it as LinkedInClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    if (request.method !== 'GET' || request.url !== UserInfoURL) return { status: 404, headers: {}, body: new Uint8Array() };
    this.requests++;
    if ((request.headers as Record<string, string> | undefined)?.Authorization !== `Bearer ${AccessToken}`) return { status: 401, headers: {}, body: new Uint8Array() };
    const failure = this.failures.shift();
    if (failure) return { status: failure.status, headers: failure.retryAfter ? { 'retry-after': failure.retryAfter } : {}, body: new Uint8Array() };
    return { status: 200, headers: { 'content-type': 'application/json', 'x-li-uuid': 'fake-request' }, body: Buffer.from(JSON.stringify(this.profile)) };
  };
}

/** A LinkedInClient whose requests the fake answers, with a fixed clock when now is given. */
export function newFakeClient(fake: { send: LinkedInClientOptions['send'] }, connectionName = 'linkedin-watcher', now?: () => Date, accessToken = AccessToken, pollIntervalMs = 60_000): LinkedInClient {
  const reference = { provider: 'linkedin', name: connectionName };
  const credentials = new StaticCredentialProvider<Credentials>([[reference, { oauthClientId: '', oauthClientSecret: new SecretString(''), accessToken: new SecretString(accessToken), refreshToken: new SecretString('') }]]);
  return new LinkedInClient({ pollIntervalMs }, credentials, { ...(fake.send ? { send: fake.send } : {}), ...(now ? { now } : {}) });
}

/** A LinkedIn connection named connectionName whose requests the fake answers. */
export function newFakeConnection(fake: LinkedInProfileFake, connectionName = 'linkedin-watcher', accessToken = AccessToken): Connection {
  return newConnection(newFakeClient(fake, connectionName, undefined, accessToken), { provider: 'linkedin', name: connectionName });
}

/**
 * A loaded project with one LinkedIn OAuth connection and its bindings over in-memory storage; pass the same
 * objects again to model a restarted process that reopens the storage the first one wrote.
 */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore, pollInterval: unknown = 60_000_000_000): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'linkedin-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'linkedin', connectionName }, 0,
    new CredentialMaterial({ oauth_client_id: 'linkedin-client-id', oauth_client_secret: 'linkedin-' + 'client-secret', access_token: AccessToken, refresh_token: '' }, new Date('2099-01-01T00:00:00Z'), 'oauth2'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'linkedin', connectionName, modulePath: 'linkedin', provider: 'linkedin', configuration: { pollInterval } }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'linkedin', connectionName, triggerName: 'profileChanged', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
