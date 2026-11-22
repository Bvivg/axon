import type { ReactNode } from "react";

export interface Option {
  value: string;
  label: string;
  icon?: ReactNode;
}

export enum FilterKind {
  Text = "text",
  Choice = "choice",
  Number = "number",
  Date = "date",
  Person = "person",
  Flag = "flag",
  Id = "id",
}

export interface FilterDef {
  field: string;
  label: string;
  kind: FilterKind;
  options?: Option[];
}

export interface SortState {
  field: string;
  desc: boolean;
}

export const pageSizes = [25, 50, 100] as const;
export const defaultPageSize = 25;
export const rangeSeparator = "..";
export const filterPrefix = "f.";

export function quote(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

export function splitRange(value: string): [string, string] {
  const at = value.indexOf(rangeSeparator);
  if (at < 0) return [value, ""];
  return [value.slice(0, at), value.slice(at + rangeSeparator.length)];
}

export function joinRange(from: string, to: string): string {
  return from || to ? `${from}${rangeSeparator}${to}` : "";
}

export function splitChoices(value: string): string[] {
  return value ? value.split(",").filter(Boolean) : [];
}

function startOfDay(date: string): string | undefined {
  const [y, m, d] = date.split("-").map(Number);
  if (!y || !m || !d) return undefined;
  return new Date(y, m - 1, d).toISOString();
}

function startOfNextDay(date: string): string | undefined {
  const [y, m, d] = date.split("-").map(Number);
  if (!y || !m || !d) return undefined;
  return new Date(y, m - 1, d + 1).toISOString();
}

function isWholeNumber(value: string): boolean {
  return /^-?\d+$/.test(value);
}

export function filterTerm(def: FilterDef, value: string): string | undefined {
  if (!value) return undefined;
  switch (def.kind) {
    case FilterKind.Text:
      return value.trim() ? `${def.field}:${quote(value.trim())}` : undefined;
    case FilterKind.Choice: {
      const choices = splitChoices(value);
      if (choices.length === 0) return undefined;
      const terms = choices.map((choice) => `${def.field} = ${quote(choice)}`);
      return terms.length === 1 ? terms[0] : `(${terms.join(" OR ")})`;
    }
    case FilterKind.Number: {
      const [from, to] = splitRange(value);
      const terms = [
        isWholeNumber(from) ? `${def.field} >= ${from}` : undefined,
        isWholeNumber(to) ? `${def.field} <= ${to}` : undefined,
      ].filter(Boolean);
      return terms.length ? terms.join(" AND ") : undefined;
    }
    case FilterKind.Date: {
      const [from, to] = splitRange(value);
      const start = from ? startOfDay(from) : undefined;
      const end = to ? startOfNextDay(to) : undefined;
      const terms = [
        start ? `${def.field} >= ${quote(start)}` : undefined,
        end ? `${def.field} < ${quote(end)}` : undefined,
      ].filter(Boolean);
      return terms.length ? terms.join(" AND ") : undefined;
    }
    case FilterKind.Person:
    case FilterKind.Id:
      return `${def.field} = ${quote(value)}`;
    case FilterKind.Flag:
      return value === "true" || value === "false" ? `${def.field} = ${value}` : undefined;
  }
}

export function toFilter(defs: readonly FilterDef[], values: Readonly<Record<string, string>>): string {
  return defs
    .map((def) => filterTerm(def, values[def.field] ?? ""))
    .filter((term): term is string => !!term)
    .join(" AND ");
}

export function parseSort(value: string): SortState | null {
  if (!value) return null;
  return value.startsWith("-") ? { field: value.slice(1), desc: true } : { field: value, desc: false };
}

export function formatSort(sort: SortState | null): string {
  if (!sort) return "";
  return sort.desc ? `-${sort.field}` : sort.field;
}

export function toOrderBy(sort: SortState | null): string {
  if (!sort) return "";
  return sort.desc ? `${sort.field} desc` : sort.field;
}

export function pageNumber(value: string): number {
  const n = Number.parseInt(value, 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export function pageSize(value: string): number {
  const n = Number.parseInt(value, 10);
  return (pageSizes as readonly number[]).includes(n) ? n : defaultPageSize;
}

export function pageCount(total: number, size: number): number {
  return Math.max(1, Math.ceil(total / size));
}

export function pageWindow(page: number, count: number): (number | null)[] {
  if (count <= 7) return Array.from({ length: count }, (_, i) => i + 1);
  const pages = new Set([1, count, page - 1, page, page + 1].filter((n) => n >= 1 && n <= count));
  if (page <= 3) [2, 3, 4].forEach((n) => pages.add(n));
  if (page >= count - 2) [count - 3, count - 2, count - 1].forEach((n) => pages.add(n));
  const sorted = [...pages].sort((a, b) => a - b);
  const out: (number | null)[] = [];
  sorted.forEach((n, i) => {
    if (i > 0 && n - sorted[i - 1] > 1) out.push(null);
    out.push(n);
  });
  return out;
}

export function describeFilter(def: FilterDef, value: string, people?: (id: string) => string): string {
  switch (def.kind) {
    case FilterKind.Text:
      return `contains “${value}”`;
    case FilterKind.Choice:
      return splitChoices(value).map((choice) => def.options?.find((option) => option.value === choice)?.label ?? choice).join(", ");
    case FilterKind.Number:
    case FilterKind.Date: {
      const [from, to] = splitRange(value);
      if (from && to) return `${from} – ${to}`;
      return from ? `from ${from}` : `up to ${to}`;
    }
    case FilterKind.Person:
      return people?.(value) || value.slice(0, 8);
    case FilterKind.Id:
      return value.slice(0, 8);
    case FilterKind.Flag:
      return value === "true" ? "yes" : "no";
  }
}

export type LocalValue = string | number | boolean | Date | undefined;

function matchesLocal(def: FilterDef, value: string, actual: LocalValue): boolean {
  switch (def.kind) {
    case FilterKind.Text:
      return String(actual ?? "").toLowerCase().includes(value.trim().toLowerCase());
    case FilterKind.Choice:
      return splitChoices(value).includes(String(actual ?? ""));
    case FilterKind.Number: {
      const [from, to] = splitRange(value);
      const n = Number(actual ?? 0);
      return (!isWholeNumber(from) || n >= Number(from)) && (!isWholeNumber(to) || n <= Number(to));
    }
    case FilterKind.Date: {
      if (!(actual instanceof Date)) return false;
      const [from, to] = splitRange(value);
      const start = from ? startOfDay(from) : undefined;
      const end = to ? startOfNextDay(to) : undefined;
      return (!start || actual >= new Date(start)) && (!end || actual < new Date(end));
    }
    case FilterKind.Flag:
      return String(actual) === value;
    case FilterKind.Person:
    case FilterKind.Id:
      return String(actual ?? "") === value;
  }
}

function compareLocal(a: LocalValue, b: LocalValue): number {
  if (a === undefined || b === undefined) return a === b ? 0 : a === undefined ? 1 : -1;
  if (a instanceof Date && b instanceof Date) return a.getTime() - b.getTime();
  if (typeof a === "number" && typeof b === "number") return a - b;
  return String(a).localeCompare(String(b), undefined, { numeric: true, sensitivity: "base" });
}

export function localRows<T>(rows: readonly T[], defs: readonly FilterDef[], values: Record<string, string>, sort: SortState | null, read: (row: T, field: string) => LocalValue): T[] {
  const active = defs.filter((def) => values[def.field]);
  const out = rows.filter((row) => active.every((def) => matchesLocal(def, values[def.field], read(row, def.field))));
  if (!sort) return out;
  const direction = sort.desc ? -1 : 1;
  return out.sort((a, b) => {
    const x = read(a, sort.field);
    const y = read(b, sort.field);
    if (x === undefined || y === undefined) return compareLocal(x, y);
    return compareLocal(x, y) * direction;
  });
}
