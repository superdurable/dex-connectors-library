// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BlockList, isIP } from 'node:net';

const loopback = new BlockList();
loopback.addSubnet('127.0.0.0', 8, 'ipv4');
loopback.addAddress('::1', 'ipv6');

const privateAddresses = new BlockList();
privateAddresses.addSubnet('127.0.0.0', 8, 'ipv4');
privateAddresses.addSubnet('10.0.0.0', 8, 'ipv4');
privateAddresses.addSubnet('172.16.0.0', 12, 'ipv4');
privateAddresses.addSubnet('192.168.0.0', 16, 'ipv4');
privateAddresses.addAddress('::1', 'ipv6');
privateAddresses.addSubnet('fc00::', 7, 'ipv6');

const linkLocal = new BlockList();
linkLocal.addSubnet('169.254.0.0', 16, 'ipv4');
linkLocal.addSubnet('fe80::', 10, 'ipv6');

const sharedAddressSpace = new BlockList();
sharedAddressSpace.addSubnet('100.64.0.0', 10, 'ipv4');

const multicast = new BlockList();
multicast.addSubnet('224.0.0.0', 4, 'ipv4');
multicast.addSubnet('ff00::', 8, 'ipv6');

/** Remove URL brackets without treating a hostname as an IP literal. */
export function unbracketHostname(hostname: string): string { return hostname.replace(/^\[|\]$/g, ''); }

/** Check an actual IP literal against a CIDR list. */
function contains(list: BlockList, hostname: string): boolean {
  const address = unbracketHostname(hostname);
  const family = isIP(address);
  return family !== 0 && list.check(address, family === 4 ? 'ipv4' : 'ipv6');
}

/** Whether a hostname is a parsed loopback IP literal. */
export function isLoopbackIPAddressLiteral(hostname: string): boolean { return contains(loopback, hostname); }

/** Whether a hostname is a parsed private or loopback IP literal. */
export function isPrivateIPAddressLiteral(hostname: string): boolean { return contains(privateAddresses, hostname); }

/** Whether an IP literal belongs to link-local space. */
export function isLinkLocalIPAddressLiteral(hostname: string): boolean { return contains(linkLocal, hostname); }

/** Whether an IP literal is always refused for provider requests. */
export function isAlwaysRefusedIPAddressLiteral(hostname: string): boolean {
  const address = unbracketHostname(hostname);
  return address === '0.0.0.0' || address === '::' || address.toLowerCase() === 'fd00:ec2::254' ||
    contains(linkLocal, address) || contains(multicast, address);
}

/** Whether a checked IP address may be used under one provider policy. */
export function isAllowedProviderIPAddress(address: string, protocol: string, policy: 'publicHTTPS' | 'privateNetworkHTTP'): boolean {
  if (isIP(unbracketHostname(address)) === 0 || isAlwaysRefusedIPAddressLiteral(address)) return false;
  if (policy === 'publicHTTPS') return protocol === 'https:' && !isPrivateIPAddressLiteral(address) && !contains(sharedAddressSpace, address);
  return protocol === 'https:' || isPrivateIPAddressLiteral(address);
}

/** Broker HTTP permits only Go's named internal hosts or private/link-local IP literals. */
export function isPrivateBrokerHost(hostname: string): boolean {
  if (isIP(unbracketHostname(hostname)) !== 0) return isPrivateIPAddressLiteral(hostname) || isLinkLocalIPAddressLiteral(hostname);
  return hostname === 'localhost' || hostname === 'host.docker.internal' || !hostname.includes('.') ||
    hostname.endsWith('.svc') || hostname.endsWith('.svc.cluster.local');
}

/** Local S3 endpoints permit only exact local hosts or private IP literals. */
export function isLocalStorageHost(hostname: string): boolean {
  if (isIP(unbracketHostname(hostname)) !== 0) return isPrivateIPAddressLiteral(hostname);
  return hostname === 'localhost' || hostname === 'host.docker.internal' || hostname.endsWith('.svc.cluster.local');
}
