// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from "node:net";
import type {
  Event,
  EventAttendeeInput,
  EventDateTime,
} from "./generated/models.js";
import { object, string } from "./http.js";

const instantPattern =
  /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)([.,]\d+)?(Z|[+-]\d\d:\d\d)$/;
const wallPattern = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)([.,]\d+)?$/;
function isGoDotAtom(value: string): boolean {
  if (!value || value.startsWith(".") || value.endsWith(".") || value.includes("..")) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0)!;
    if (codePoint < 33 || codePoint === 127 || codePoint >= 0xd800 && codePoint <= 0xdfff || "()<>[]:;@\\,\"".includes(character)) return false;
  }
  return true;
}
function isBareAddress(value: string): boolean {
  const separator = value.indexOf("@");
  if (separator < 1 || separator !== value.lastIndexOf("@") || !isGoDotAtom(value.slice(0, separator))) return false;
  const domain = value.slice(separator + 1);
  if (domain.startsWith("[") && domain.endsWith("]")) return isIP(domain.slice(1, -1)) !== 0;
  return isGoDotAtom(domain);
}
export function emptyEvent(): Event {
  return {
    id: "",
    start: { dateTime: "", timeZone: "" },
    end: { dateTime: "", timeZone: "" },
    isAllDay: false,
    isCancelled: false,
    isOrganizer: false,
    showAs: "",
  };
}
export function emptyCreate() {
  return { event: emptyEvent(), wasAlreadyCreated: false };
}
export function emptyUpdate() {
  return { event: emptyEvent(), wasAlreadyApplied: false };
}
function dateParts(
  value: RegExpMatchArray,
  name: string,
): {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
  fraction: string;
} {
  const [year, month, day, hour, minute, second] = value
    .slice(1, 7)
    .map(Number);
  if (
    year! > 9999 ||
    month! < 1 ||
    month! > 12 ||
    day! < 1 ||
    day! > 31 ||
    hour! > 23 ||
    minute! > 59 ||
    second! > 59
  )
    throw new TypeError(`${name} is not a valid time`);
  const fraction = value[7]?.slice(1) ?? "";
  const check = new Date(0);
  check.setUTCFullYear(year!, month! - 1, day!);
  check.setUTCHours(hour!, minute!, second!, 0);
  if (
    check.getUTCFullYear() !== year ||
    check.getUTCMonth() + 1 !== month ||
    check.getUTCDate() !== day
  )
    throw new TypeError(`${name} is not a valid time`);
  return {
    year: year!,
    month: month!,
    day: day!,
    hour: hour!,
    minute: minute!,
    second: second!,
    fraction,
  };
}
export function instant(name: string, value: string): number {
  const match = value.match(instantPattern);
  if (!match)
    throw new TypeError(`${name} must be RFC 3339 with an explicit offset`);
  const parts = dateParts(match, name);
  const zone = match[8]!;
  let offset = 0;
  if (zone !== "Z") {
    const hh = Number(zone.slice(1, 3)),
      mm = Number(zone.slice(4, 6));
    if (hh > 23 || mm > 59)
      throw new TypeError(`${name} has an invalid offset`);
    offset = (hh * 60 + mm) * 60_000 * (zone[0] === "+" ? 1 : -1);
  }
  const candidate = new Date(0);
  candidate.setUTCFullYear(parts.year, parts.month - 1, parts.day);
  candidate.setUTCHours(
    parts.hour,
    parts.minute,
    parts.second,
    Number(parts.fraction.slice(0, 3).padEnd(3, "0")),
  );
  return candidate.getTime() - offset;
}
export function validateZone(name: string, zone: string): string {
  if (!zone || zone.trim() !== zone || zone === "Local")
    throw new TypeError(`${name} must be an IANA time zone`);
  if (/^[+-]\d\d:\d\d$/.test(zone)) throw new TypeError(`${name} is not a known IANA time zone`);
  try {
    const canonical = new Intl.DateTimeFormat("en-US", { timeZone: zone }).resolvedOptions().timeZone;
    if (zone.toLowerCase() === canonical.toLowerCase() && zone !== canonical) throw new TypeError();
  } catch {
    throw new TypeError(`${name} is not a known IANA time zone`);
  }
  return zone;
}
export function displayZone(zone: string | undefined): string {
  return validateZone("timeZone", zone || "UTC");
}
export function windowTimes(start: string, end: string): [number, number] {
  if (!start || !end)
    throw new TypeError("timeMin and timeMax are both required");
  const first = instant("timeMin", start),
    last = instant("timeMax", end);
  if (last <= first) throw new TypeError("timeMax must be after timeMin");
  return [first, last];
}
function localParts(epoch: number, zone: string): Record<string, string> {
  const formatter = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    hourCycle: "h23",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "longOffset",
  });
  return Object.fromEntries(
    formatter
      .formatToParts(new Date(epoch))
      .map((part) => [part.type, part.value]),
  );
}
function offsetMs(parts: Record<string, string>): number {
  const value = parts.timeZoneName ?? "GMT";
  if (value === "GMT") return 0;
  const match = value.match(/^GMT([+-])(\d\d?):(\d\d)$/);
  if (!match) throw new TypeError("time zone offset is unavailable");
  return (
    (Number(match[2]) * 60 + Number(match[3])) *
    60_000 *
    (match[1] === "+" ? 1 : -1)
  );
}
export function formatInstant(epoch: number, zone: string): string {
  const parts = localParts(epoch, zone);
  const offset = offsetMs(parts);
  const sign = offset >= 0 ? "+" : "-";
  const hh = String(Math.floor(Math.abs(offset) / 3_600_000)).padStart(2, "0");
  const mm = String(
    Math.floor((Math.abs(offset) % 3_600_000) / 60_000),
  ).padStart(2, "0");
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}:${parts.second}${offset === 0 ? "Z" : `${sign}${hh}:${mm}`}`;
}
export function boundary(name: string, value: EventDateTime): number {
  const result = instant(name + ".dateTime", value.dateTime);
  validateZone(name + ".timeZone", value.timeZone);
  return result;
}
export function validateEventTimes(
  start: EventDateTime,
  end: EventDateTime,
  isAllDay: boolean,
): void {
  const first = boundary("start", start),
    last = boundary("end", end);
  if (last <= first) throw new TypeError("end must be after start");
  if (isAllDay) {
    if (start.timeZone !== end.timeZone)
      throw new TypeError(
        "an all-day event needs start and end in the same timeZone",
      );
    for (const [name, epoch] of [
      ["start", first],
      ["end", last],
    ] as const) {
      const parts = localParts(epoch, start.timeZone);
      if (
        parts.hour !== "00" ||
        parts.minute !== "00" ||
        parts.second !== "00" ||
        epoch % 1000 !== 0 || /[.,]\d*[1-9]\d*(?:Z|[+-]\d\d:\d\d)$/.test((name === "start" ? start : end).dateTime)
      )
        throw new TypeError(
          `an all-day ${name} must be midnight in ${start.timeZone}`,
        );
      if (isAmbiguousWallTime(epoch, start.timeZone))
        throw new TypeError(`an all-day ${name} falls on an ambiguous midnight in ${start.timeZone}`);
    }
  }
}
function isAmbiguousWallTime(epoch: number, zone: string): boolean {
  const parts = localParts(epoch, zone);
  const wall = `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}:${parts.second}`;
  const originalOffset = offsetMs(parts);
  for (const probe of [-12 * 3600_000, 12 * 3600_000]) {
    const probeOffset = offsetMs(localParts(epoch + probe, zone));
    if (probeOffset === originalOffset) continue;
    const other = localParts(epoch + originalOffset - probeOffset, zone);
    if (`${other.year}-${other.month}-${other.day}T${other.hour}:${other.minute}:${other.second}` === wall) return true;
  }
  return false;
}
export function graphBoundary(value: EventDateTime): {
  dateTime: string;
  timeZone: string;
} {
  const epoch = boundary("dateTime", value);
  const parts = localParts(epoch, value.timeZone);
  const wall = `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}:${parts.second}`;
  if (isAmbiguousWallTime(epoch, value.timeZone)) return graphUTC(epoch);
  return { dateTime: wall, timeZone: value.timeZone };
}
export function graphUTC(epoch: number): {
  dateTime: string;
  timeZone: string;
} {
  return {
    dateTime: new Date(epoch).toISOString().slice(0, 19),
    timeZone: "UTC",
  };
}
export function returnedBoundary(
  name: string,
  raw: unknown,
  display: string,
): { value: EventDateTime; epoch: number } {
  const item = object(raw);
  const wall = string(item.dateTime);
  const match = wall.match(wallPattern);
  if (!match) throw new TypeError(`${name}.dateTime is not a Graph wall time`);
  const parts = dateParts(match, name + ".dateTime");
  const zone = validateZone(name + ".timeZone", string(item.timeZone) || "UTC");
  const candidate = new Date(0);
  candidate.setUTCFullYear(parts.year, parts.month - 1, parts.day);
  candidate.setUTCHours(
    parts.hour,
    parts.minute,
    parts.second,
    Number(parts.fraction.slice(0, 3).padEnd(3, "0")),
  );
  const naive = candidate.getTime();
  let epoch = naive;
  if (zone !== "UTC") {
    const firstOffset = offsetMs(localParts(naive, zone));
    epoch = naive - firstOffset;
    const secondOffset = offsetMs(localParts(epoch, zone));
    epoch = naive - secondOffset;
  }
  return {
    value: { dateTime: formatInstant(epoch, display), timeZone: display },
    epoch,
  };
}
export function event(
  raw: unknown,
  calendarId: string,
  display: string,
): Event {
  const item = object(raw);
  const id = string(item.id);
  if (!id) throw new TypeError("event response has no ID");
  const start = returnedBoundary("start", item.start, display),
    end = returnedBoundary("end", item.end, display);
  if (end.epoch < start.epoch)
    throw new TypeError(`event ${id} ends before it starts`);
  const attendees = Array.isArray(item.attendees)
    ? item.attendees.flatMap((rawAttendee) => {
        try {
          const attendee = object(rawAttendee),
            address = object(attendee.emailAddress);
          return [
            {
              email: string(address.address),
              ...(address.name ? { name: string(address.name) } : {}),
              ...(attendee.type ? { type: string(attendee.type) } : {}),
              ...(attendee.status && object(attendee.status).response
                ? { responseStatus: string(object(attendee.status).response) }
                : {}),
            },
          ];
        } catch {
          return [];
        }
      })
    : [];
  const online = item.isOnlineMeeting || item.onlineMeeting
    ? {
        provider: string(item.onlineMeetingProvider),
        ...(item.onlineMeeting && object(item.onlineMeeting).joinUrl
          ? { joinUrl: string(object(item.onlineMeeting).joinUrl) } : {}),
      }
    : undefined;
  return {
    id,
    ...(calendarId ? { calendarId } : {}),
    ...(item.subject ? { subject: string(item.subject) } : {}),
    ...(item.body && object(item.body).content ? { body: string(object(item.body).content) } : {}),
    ...(item.bodyPreview ? { bodyPreview: string(item.bodyPreview) } : {}),
    ...(item.location && object(item.location).displayName
      ? { location: string(object(item.location).displayName) }
      : {}),
    start: start.value,
    end: end.value,
    ...(item.originalStartTimeZone
      ? { originalStartTimeZone: string(item.originalStartTimeZone) }
      : {}),
    ...(item.originalEndTimeZone
      ? { originalEndTimeZone: string(item.originalEndTimeZone) }
      : {}),
    isAllDay: item.isAllDay === true,
    isCancelled: item.isCancelled === true,
    isOrganizer: item.isOrganizer === true,
    showAs: string(item.showAs) || "unknown",
    ...(item.sensitivity ? { sensitivity: string(item.sensitivity) } : {}),
    ...(item.type ? { type: string(item.type) } : {}),
    ...(item.seriesMasterId
      ? { seriesMasterId: string(item.seriesMasterId) }
      : {}),
    ...(item.responseStatus
      ? { responseStatus: string(object(item.responseStatus).response) }
      : {}),
    ...(attendees.length ? { attendees } : {}),
    ...(item.organizer && object(item.organizer).emailAddress && object(object(item.organizer).emailAddress).address
      ? { organizerEmail: string(object(object(item.organizer).emailAddress).address) }
      : {}),
    ...(online ? { onlineMeeting: online } : {}),
    ...(item.webLink ? { webLink: string(item.webLink) } : {}),
    ...(item.iCalUId ? { iCalUId: string(item.iCalUId) } : {}),
    ...(item.changeKey ? { changeKey: string(item.changeKey) } : {}),
    ...(item["@odata.etag"] ? { etag: string(item["@odata.etag"]) } : {}),
    ...(item.transactionId
      ? { transactionId: string(item.transactionId) }
      : {}),
  };
}
export function validateAttendees(attendees: EventAttendeeInput[]): void {
  const seen = new Set<string>();
  for (const item of attendees) {
    if (!isBareAddress(item.email))
      throw new TypeError(
        "attendee email must be one bare address such as person@example.com",
      );
    const key = item.email.toLowerCase();
    if (seen.has(key)) throw new TypeError("attendee email is duplicated");
    seen.add(key);
  }
}
export function encodeAttendees(attendees: EventAttendeeInput[]): unknown[] {
  return attendees.map((item) => ({
    emailAddress: { address: item.email },
    type: item.isOptional ? "optional" : "required",
  }));
}
export function validateCalendarId(value: string): void {
  if (value !== value.trim())
    throw new TypeError(
      "calendarId cannot have surrounding whitespace; leave it blank for the mailbox's default calendar",
    );
}
export function validateEventId(value: string): void {
  if (!value.trim() || value !== value.trim())
    throw new TypeError("eventId is required");
}
export function validateAddress(value: string): void {
  if (!isBareAddress(value))
    throw new TypeError(
      "schedule must be one bare address such as person@example.com",
    );
}
