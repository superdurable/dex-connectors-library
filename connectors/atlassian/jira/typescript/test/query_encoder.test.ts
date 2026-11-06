// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { encodeQuery } from '../src/client.js';

// Expected strings recorded from Go net/url.Values.Encode.
describe('Go query encoding', () => {
  it.each([
    [{ z: "~ * ! ' ( ) space + / ? & = café", a: ['one', 'two'] }, 'a=one&a=two&z=~+%2A+%21+%27+%28+%29+space+%2B+%2F+%3F+%26+%3D+caf%C3%A9'],
    [{ é: 'x', z: 'y' }, 'z=y&%C3%A9=x'],
    [{ '*': '!' }, '%2A=%21'],
  ])('matches net/url for %j', (values, expected) => {
    expect(encodeQuery(values)).toBe(expected);
  });
});
