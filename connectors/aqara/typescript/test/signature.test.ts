// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { signDeviceRequest } from '../src/signature.js';

it('matches the published Aqara signature example', () => {
  // The published digest uses the Java sample's KeyID without the prose example's k. prefix.
  expect(signDeviceRequest(
    '532cad73c5493193d63d367016b98b27', '4e693d54d75db580a56d1263',
    '78784564654feda454557', 'C6wuzd0Qguxzelhb', '1618914078668',
    'gU7Qtxi4dWnYAdmudyxni52bWZ58b8uN',
  )).toBe('bfd8dd0e7c108353e6740d81e05982d8');
});
