// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { createRequest, decodeMeeting, pageToken, participantPage, updateRequest, zoomTime } from '../src/meeting.js';

describe('Zoom provider mapping', () => {
  it('sends only the absolute create fields and a UTC whole-second instant', () => {
    const built = createRequest({ topic: 'Consultation', startTime: '2036-02-24T09:00:00-08:00', timeZone: 'America/Los_Angeles', durationMinutes: 30, settings: { hostVideo: false } });
    expect(built.payload).toEqual({ topic: 'Consultation', type: 2, start_time: '2036-02-24T17:00:00Z', duration: 30, timezone: 'America/Los_Angeles', settings: { host_video: false } });
    expect(built.requested.id).toBe(0);
  });
  it('accepts a leap day and comma fractional seconds without changing the instant', () => {
    expect(zoomTime('2036-02-29T17:00:00,123456789-08:00')).toBe('2036-03-01T01:00:00.123456789Z');
    expect(zoomTime('0001-01-01T00:00:00Z')).toBe('0001-01-01T00:00:00Z');
  });
  it.each(['2035-02-29T17:00:00Z', '2036-02-30T17:00:00Z', '2036-02-24T24:00:00Z'])('rejects invalid calendar time %s', value => {
    expect(() => zoomTime(value)).toThrow();
  });
  it('rejects a non-whole-second start and invalid provider field types', () => {
    expect(() => createRequest({ topic: 'Consultation', startTime: '2036-02-24T17:00:00.001Z', timeZone: 'UTC', durationMinutes: 30 })).toThrow();
    expect(() => decodeMeeting({ id: 1, topic: 42 })).toThrow();
    expect(() => participantPage({ total_records: 1, participants: [{ duration: '20' }] }, 1)).toThrow();
  });
  it('keeps an explicitly cleared agenda and rejects non-printable page tokens', () => {
    expect(updateRequest({ meetingId: 1, agenda: '' }, new Date('2026-01-01'))).toEqual({ agenda: '' });
    expect(() => pageToken('bad token')).toThrow();
  });
});
