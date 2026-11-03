// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BlockList, isIP } from 'node:net';

const loopback = new BlockList();
loopback.addSubnet('127.0.0.0', 8, 'ipv4');
loopback.addAddress('::1', 'ipv6');
const refused = new BlockList();
for (const [address, prefix] of [
  ['0.0.0.0', 8], ['10.0.0.0', 8], ['100.64.0.0', 10],
  ['127.0.0.0', 8], ['169.254.0.0', 16], ['172.16.0.0', 12],
  ['192.168.0.0', 16], ['224.0.0.0', 4],
] as const) refused.addSubnet(address, prefix, 'ipv4');
refused.addSubnet('fc00::', 7, 'ipv6');
refused.addSubnet('fe80::', 10, 'ipv6');
refused.addSubnet('ff00::', 8, 'ipv6');
refused.addAddress('::', 'ipv6');
refused.addAddress('::1', 'ipv6');
refused.addAddress('fd00:ec2::254', 'ipv6');

function plainAddress(value: string): string {
  const address = value.replace(/^\[|\]$/g, '');
  const mapped = /^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/i.exec(address);
  if (!mapped) return address;
  const high = Number.parseInt(mapped[1]!, 16);
  const low = Number.parseInt(mapped[2]!, 16);
  return `${high >>> 8}.${high & 255}.${low >>> 8}.${low & 255}`;
}

/** Whether Go netip accepts a resolved loopback address. */
export function isLoopbackAddress(value: string): boolean {
  const address = plainAddress(value);
  const family = isIP(address);
  return family === 4 ? loopback.check(address, 'ipv4') : family === 6 && loopback.check(address, 'ipv6');
}

/** Match Go's OpenAI-compatible global-unicast address restriction after IP unmapping. */
export function isAllowedOpenAICompatibleAddress(value: string, isLoopback: boolean): boolean {
  const address = plainAddress(value);
  if (isLoopback) return isLoopbackAddress(address);
  const family = isIP(address);
  if (!family) return false;
  if (family === 4) return !refused.check(address, 'ipv4') && address !== '255.255.255.255';
  return !refused.check(address, 'ipv6');
}
