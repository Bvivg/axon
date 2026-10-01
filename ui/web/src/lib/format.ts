import type { Timestamp } from "@bufbuild/protobuf/wkt";

export function initials(name: string): string {
  const words = name
    .replace(/[@#]/g, " ")
    .split(/[\s._-]+/)
    .filter(Boolean);
  if (words.length === 0) {
    return "?";
  }
  const letters = words.length === 1 ? words[0].slice(0, 2) : words[0][0] + words[1][0];
  return letters.toUpperCase();
}

export function fromTimestamp(ts?: Timestamp): Date | null {
  if (!ts) {
    return null;
  }
  const at = new Date(Number(ts.seconds) * 1000);
  return Number.isNaN(at.getTime()) ? null : at;
}

export function fromISO(iso: string): Date | null {
  if (!iso) {
    return null;
  }
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? null : at;
}

export function clockTime(at: Date): string {
  return at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

export function isSameDay(a: Date, b: Date): boolean {
  return a.toDateString() === b.toDateString();
}

function yesterdayOf(day: Date): Date {
  const yesterday = new Date(day);
  yesterday.setDate(day.getDate() - 1);
  return yesterday;
}

export function dayLabel(at: Date, now = new Date()): string {
  if (isSameDay(at, now)) {
    return "Today";
  }
  if (isSameDay(at, yesterdayOf(now))) {
    return "Yesterday";
  }
  return at.toLocaleDateString(undefined, {
    weekday: "short",
    day: "numeric",
    month: "short",
    year: at.getFullYear() === now.getFullYear() ? undefined : "numeric",
  });
}

export function shortWhen(at: Date, now = new Date()): string {
  if (isSameDay(at, now)) {
    return clockTime(at);
  }
  return at.toLocaleDateString(undefined, { day: "2-digit", month: "2-digit" });
}

export function lastSeen(at: Date | null, now = new Date()): string {
  if (!at) {
    return "";
  }
  const time = clockTime(at);
  if (isSameDay(at, now)) {
    return `last seen at ${time}`;
  }
  if (isSameDay(at, yesterdayOf(now))) {
    return `last seen yesterday at ${time}`;
  }
  const day = at.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: at.getFullYear() === now.getFullYear() ? undefined : "numeric",
  });
  return `last seen ${day} at ${time}`;
}

export function personName(user: {
  email: string;
  displayName?: string;
  nickname?: string;
  firstName?: string;
  lastName?: string;
}): string {
  const fullName = [user.firstName, user.lastName].filter(Boolean).join(" ");
  return user.displayName || fullName || user.nickname || user.email;
}

export function shortID(id?: string): string {
  return id ? id.slice(0, 8) : "?";
}

export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = String(total % 60).padStart(2, "0");
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, "0")}:${seconds}` : `${minutes}:${seconds}`;
}

const byteUnits = ["B", "KB", "MB", "GB"];

export function formatBytes(bytes: number): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1024 && unit < byteUnits.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const digits = unit === 0 || value >= 10 ? 0 : 1;
  return `${value.toFixed(digits)} ${byteUnits[unit]}`;
}
