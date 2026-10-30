// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtemp, mkdir, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const directory = await mkdtemp(join(tmpdir(), 'sdkts-pack-smoke-'));
try {
  execFileSync('npm', ['pack', '--pack-destination', directory], { cwd: packageRoot, stdio: 'pipe' });
  const archives = (await readdir(directory)).filter(name => name.endsWith('.tgz'));
  assert.equal(archives.length, 1);
  const archive = join(directory, archives[0]);
  const members = execFileSync('tar', ['-tzf', archive], { encoding: 'utf8' }).trim().split('\n');
  assert.ok(members.every(member => member.startsWith('package/dist/') || ['package/README.md', 'package/LICENSE', 'package/package.json'].includes(member)), 'npm package includes an unexpected file');
  assert.ok(members.every(member => !member.endsWith('.cjs') && !member.endsWith('.d.cts')), 'npm package contains a CommonJS artifact');

  const consumer = join(directory, 'consumer');
  const packedPackage = join(consumer, 'node_modules', '@superdurable', 'connectors-sdk');
  await mkdir(packedPackage, { recursive: true });
  execFileSync('tar', ['-xzf', archive, '--strip-components=1', '-C', packedPackage]);
  await symlink(join(packageRoot, 'node_modules'), join(packedPackage, 'node_modules'), 'dir');
  await writeFile(join(consumer, 'check.mjs'), `// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const paths = ['', '/providerhttp', '/projectconfig', '/connectormock', '/oauthtoken', '/singledispatch'];
const requirePackage = createRequire(import.meta.url);
for (const path of paths) {
  const specifier = '@superdurable/connectors-sdk' + path;
  assert.ok(Object.keys(await import(specifier)).length > 0, specifier + ' did not import');
  assert.throws(() => requirePackage(specifier), error => {
    assert.equal(error.code, 'ERR_PACKAGE_PATH_NOT_EXPORTED');
    assert.match(error.message, /No "exports" main defined|Package subpath/);
    return true;
  });
}
const sdk = await import('@superdurable/connectors-sdk');
const oauth = await import('@superdurable/connectors-sdk/oauthtoken');
const endpoint = new oauth.TokenEndpoint({ providerName: 'fixture', url: 'https://example.test/token', terminalErrorCodes: ['invalid_grant'],
  fetch: async () => new Response('{"error":"invalid_grant"}', { status: 400 }) });
await assert.rejects(endpoint.exchangeRefreshToken({ id: 'client', authenticationMethod: 'none' }, new sdk.SecretString('renewal')),
  error => error instanceof sdk.ReauthorizationRequiredError);
`);
  execFileSync(process.execPath, [join(consumer, 'check.mjs')], { cwd: consumer, stdio: 'inherit' });
} finally {
  await rm(directory, { recursive: true, force: true });
}
