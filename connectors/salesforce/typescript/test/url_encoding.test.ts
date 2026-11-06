// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { goPathEscape, goQuery } from '../src/client.js';

describe('Go net/url encoding', () => {
  it.each([
    [{ "a ~*!'()+": "A ~*!'()+" }, 'a+~%2A%21%27%28%29%2B=A+~%2A%21%27%28%29%2B'],
    [{ z: 'two', a: 'one' }, 'a=one&z=two'],
    [{ q: "SELECT Id FROM Contact WHERE Name = 'O\\'Brien'" }, 'q=SELECT+Id+FROM+Contact+WHERE+Name+%3D+%27O%5C%27Brien%27'],
  ] as const)('encodes sorted query values', (values, expected) => expect(goQuery(values)).toBe(expected));

  it.each([
    ["a ~*!'()+/@:$&=,?", 'a%20~%2A%21%27%28%29+%2F@:$&=%2C%3F'],
    ['ERP/88213', 'ERP%2F88213'],
    ['é', '%C3%A9'],
  ] as const)('escapes one path segment', (value, expected) => expect(goPathEscape(value)).toBe(expected));
});
