// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CreateMeetingInput, Meeting, MeetingParticipant, MeetingSettingsInput, MeetingSummary, ParticipantPage, UpdateMeetingInput } from './generated/models.js';

const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/;

/** The zero Meeting emitted by Go on a pre-dispatch defect. */
export const emptyMeeting: Meeting = { id: 0, topic: '', settings: { hostVideo: false, participantVideo: false, joinBeforeHost: false, muteUponEntry: false, waitingRoom: false } };
/** The zero MeetingPage emitted by Go on a failed list. */
export const emptyMeetingPage = { meetings: [], totalRecords: 0 };

/** Require a Zoom JSON object. */
export function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Zoom returned an unusable object');
  return value as Record<string, unknown>;
}
/** Decode exactly one JSON object. */
export function responseObject(body: Uint8Array): Record<string, unknown> {
  return object(JSON.parse(Buffer.from(body).toString('utf8')));
}
function string(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('Zoom returned a non-string field'); return value; }
function integer(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Zoom returned a non-integer field'); return value; }
function boolean(value: unknown): boolean { if (value === undefined || value === null) return false; if (typeof value !== 'boolean') throw new TypeError('Zoom returned a non-boolean field'); return value; }

/** Parse the RFC 3339 subset accepted by Go time.Parse and return Go's UTC JSON spelling. */
export function zoomTime(value: unknown): string | undefined {
  const raw = string(value);
  if (!raw) return undefined;
  const match = timestampPattern.exec(raw);
  if (!match) throw new TypeError('Zoom returned an invalid timestamp');
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fraction, zone, sign, zoneHourText, zoneMinuteText] = match;
  const year = Number(yearText), month = Number(monthText), day = Number(dayText);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || Number(hourText) > 23 || Number(minuteText) > 59 || Number(secondText) > 59 ||
    (zone !== 'Z' && (Number(zoneHourText) > 24 || Number(zoneMinuteText) > 60))) throw new TypeError('Zoom returned an invalid timestamp');
  const offset = zone === 'Z' ? 0 : (Number(zoneHourText) * 60 + Number(zoneMinuteText)) * (sign === '+' ? 1 : -1);
  const calendar = new Date(0); calendar.setUTCFullYear(year, month - 1, day); calendar.setUTCHours(Number(hourText), Number(minuteText), Number(secondText), 0);
  const milliseconds = calendar.getTime() - offset * 60_000;
  const instant = new Date(milliseconds);
  if (!Number.isFinite(milliseconds)) throw new TypeError('Zoom returned an invalid timestamp');
  const nanoseconds = (fraction ?? '').slice(0, 9).padEnd(9, '0');
  const trimmed = nanoseconds.replace(/0+$/, '');
  return instant.toISOString().replace(/\.\d{3}Z$/, '') + (trimmed ? `.${trimmed}` : '') + 'Z';
}

/** Validate a user-supplied positive meeting number. */
export function meetingID(value: number): void { if (!Number.isSafeInteger(value) || value < 1) throw new TypeError("meetingId must be Zoom's positive numeric meeting ID"); }
/** Validate one Zoom pagination token. */
export function pageToken(value = ''): void { if (Buffer.byteLength(value) > 1024 || !/^[!-~]*$/.test(value)) throw new TypeError('pageToken must be printable ASCII without spaces and at most 1024 bytes'); }
/** Resolve Zoom's documented page size. */
export function pageSize(value = 0): number { if (value === 0) return 30; if (!Number.isSafeInteger(value) || value < 1 || value > 300) throw new TypeError('pageSize must be between 1 and 300'); return value; }
function topic(value: string): void { if (!value.trim()) throw new TypeError('topic is required'); if ([...value].length > 200) throw new TypeError('topic must be valid UTF-8 of at most 200 characters'); }
function agenda(value: string): void { if ([...value].length > 2000) throw new TypeError('agenda must be valid UTF-8 of at most 2000 characters'); }
function duration(value: number): void { if (!Number.isSafeInteger(value) || value < 1 || value > 1440) throw new TypeError('durationMinutes must be between 1 and 1440'); }
function zone(value: string): void { if (!value || value !== value.trim() || value === 'Local') throw new TypeError('timeZone must be an IANA time zone name'); try { new Intl.DateTimeFormat('en-US', { timeZone: value }); } catch { throw new TypeError('timeZone is not a known IANA time zone name'); } }
function start(value: string, timeZone: string): string {
  zone(timeZone);
  const instant = zoomTime(value);
  if (!instant || !timestampPattern.test(value) || (timestampPattern.exec(value)?.[7] ?? '').replace(/0/g, '') !== '') throw new TypeError('startTime must be a whole-second RFC 3339 instant with an explicit offset');
  return instant;
}
/** Map only the meeting settings Zoom accepts. */
export function settings(input?: MeetingSettingsInput): Record<string, unknown> | undefined {
  if (!input) return undefined;
  if (input.autoRecording && !['none', 'local', 'cloud'].includes(input.autoRecording)) throw new TypeError('settings.autoRecording must be none, local, or cloud');
  const value = { ...(input.hostVideo !== undefined ? { host_video: input.hostVideo } : {}),
    ...(input.participantVideo !== undefined ? { participant_video: input.participantVideo } : {}),
    ...(input.joinBeforeHost !== undefined ? { join_before_host: input.joinBeforeHost } : {}),
    ...(input.muteUponEntry !== undefined ? { mute_upon_entry: input.muteUponEntry } : {}),
    ...(input.waitingRoom !== undefined ? { waiting_room: input.waitingRoom } : {}),
    ...(input.autoRecording ? { auto_recording: input.autoRecording } : {}) };
  return Object.keys(value).length ? value : undefined;
}
/** Build the exact Zoom request and secret-free requested Meeting echo. */
export function createRequest(input: CreateMeetingInput): { payload: Record<string, unknown>; requested: Meeting } {
  topic(input.topic); duration(input.durationMinutes); agenda(input.agenda ?? '');
  const instant = start(input.startTime, input.timeZone);
  const override = settings(input.settings);
  return { payload: { topic: input.topic, type: 2, start_time: instant, duration: input.durationMinutes, timezone: input.timeZone,
    ...(input.agenda ? { agenda: input.agenda } : {}), ...(override ? { settings: override } : {}) },
    requested: { id: 0, topic: input.topic, type: 2, startTime: instant, durationMinutes: input.durationMinutes,
      timeZone: input.timeZone, ...(input.agenda ? { agenda: input.agenda } : {}), settings: emptyMeeting.settings } };
}
/** Build a repeatable absolute-value PATCH. */
export function updateRequest(input: UpdateMeetingInput, now: Date): Record<string, unknown> {
  meetingID(input.meetingId);
  const payload: Record<string, unknown> = {};
  if (input.topic) { topic(input.topic); payload.topic = input.topic; }
  if (input.agenda !== undefined) { agenda(input.agenda); payload.agenda = input.agenda; }
  if (input.startTime) { const instant = start(input.startTime, input.timeZone ?? ''); if (Date.parse(instant) <= now.getTime()) throw new TypeError('startTime is not in the future'); payload.start_time = instant; payload.timezone = input.timeZone; }
  else if (input.timeZone) throw new TypeError('timeZone requires startTime');
  if (input.durationMinutes) { duration(input.durationMinutes); payload.duration = input.durationMinutes; }
  const override = settings(input.settings); if (override) payload.settings = override;
  if (!Object.keys(payload).length) throw new TypeError('updateMeeting needs at least one change');
  return payload;
}
/** Decode only safe meeting fields and reject wrong provider types. */
export function decodeMeeting(value: unknown): Meeting {
  const raw = object(value);
  const id = integer(raw.id), length = integer(raw.duration);
  if (id < 1 || length < 0) throw new TypeError('Zoom returned an unusable meeting');
  const joinUrl = string(raw.join_url);
  if (joinUrl) { const url = new URL(joinUrl); if (url.protocol !== 'https:' || !url.hostname || url.username || url.password) throw new TypeError('Zoom returned an unusable meeting'); }
  const rawSettings = raw.settings == null ? {} : object(raw.settings);
  const meeting: Meeting = { id, topic: string(raw.topic), settings: { hostVideo: boolean(rawSettings.host_video), participantVideo: boolean(rawSettings.participant_video),
    joinBeforeHost: boolean(rawSettings.join_before_host), muteUponEntry: boolean(rawSettings.mute_upon_entry), waitingRoom: boolean(rawSettings.waiting_room),
    ...(string(rawSettings.auto_recording) ? { autoRecording: string(rawSettings.auto_recording) } : {}) } };
  const fields: Array<[keyof Meeting, unknown]> = [['uuid', raw.uuid], ['hostId', raw.host_id], ['hostEmail', raw.host_email], ['status', raw.status], ['timeZone', raw.timezone], ['agenda', raw.agenda], ['joinUrl', raw.join_url]];
  for (const [key, rawValue] of fields) { const value = string(rawValue); if (value) (meeting as unknown as Record<string, unknown>)[key] = value; }
  const type = integer(raw.type); if (type) meeting.type = type;
  if (length) meeting.durationMinutes = length;
  const startTime = zoomTime(raw.start_time); if (startTime) meeting.startTime = startTime;
  const createdAt = zoomTime(raw.created_at); if (createdAt) meeting.createdAt = createdAt;
  return meeting;
}
/** Reduce the safe Meeting shape to Zoom's meeting-list output. */
export function meetingSummary(meeting: Meeting): MeetingSummary {
  const { id, uuid, hostId, topic, type, startTime, durationMinutes, timeZone, agenda, joinUrl, createdAt } = meeting;
  return { id, topic, ...(uuid ? { uuid } : {}), ...(hostId ? { hostId } : {}), ...(type ? { type } : {}), ...(startTime ? { startTime } : {}),
    ...(durationMinutes ? { durationMinutes } : {}), ...(timeZone ? { timeZone } : {}), ...(agenda ? { agenda } : {}), ...(joinUrl ? { joinUrl } : {}), ...(createdAt ? { createdAt } : {}) };
}
/** Decode a past participant listing. */
export function participantPage(value: unknown, id: number): ParticipantPage {
  const raw = object(value), total = integer(raw.total_records), token = string(raw.next_page_token);
  if (total < 0 || (raw.participants !== undefined && raw.participants !== null && !Array.isArray(raw.participants))) throw new TypeError('Zoom returned an unusable participant page');
  pageToken(token);
  const participants: MeetingParticipant[] = (raw.participants ?? []).map((value: unknown) => {
    const item = object(value), durationSeconds = integer(item.duration);
    if (durationSeconds < 0) throw new TypeError('Zoom returned an unusable participant page');
    const joinTime = zoomTime(item.join_time), leaveTime = zoomTime(item.leave_time);
    return { durationSeconds, isInternalUser: boolean(item.internal_user),
      ...(string(item.id) ? { id: string(item.id) } : {}), ...(string(item.name) ? { name: string(item.name) } : {}),
      ...(string(item.user_email) ? { email: string(item.user_email) } : {}), ...(joinTime ? { joinTime } : {}),
      ...(leaveTime ? { leaveTime } : {}), ...(string(item.status) ? { status: string(item.status) } : {}) };
  });
  return { meetingId: id, participants, totalRecords: total, ...(token ? { nextPageToken: token } : {}) };
}
