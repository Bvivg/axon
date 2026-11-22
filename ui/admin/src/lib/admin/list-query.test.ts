import { describe, expect, it } from "vitest";

import { FilterKind, formatSort, localRows, pageWindow, parseSort, quote, toFilter, toOrderBy, type FilterDef } from "@/lib/admin/list-query";

const defs: FilterDef[] = [
  { field: "email", label: "Email", kind: FilterKind.Text },
  { field: "status", label: "Status", kind: FilterKind.Choice, options: [{ value: "active", label: "Active" }, { value: "suspended", label: "Suspended" }] },
  { field: "sessions", label: "Sessions", kind: FilterKind.Number },
  { field: "joined", label: "Joined", kind: FilterKind.Date },
  { field: "actor", label: "Actor", kind: FilterKind.Person },
  { field: "rated", label: "Rated", kind: FilterKind.Flag },
];

describe("toFilter", () => {
  it("is empty without values", () => {
    expect(toFilter(defs, {})).toBe("");
  });

  it("quotes text and escapes quotes and backslashes", () => {
    expect(quote('a "b" \\c')).toBe('"a \\"b\\" \\\\c"');
    expect(toFilter(defs, { email: '  x" OR 1=1  ' })).toBe('email:"x\\" OR 1=1"');
  });

  it("joins choices with OR inside parentheses", () => {
    expect(toFilter(defs, { status: "active" })).toBe('status = "active"');
    expect(toFilter(defs, { status: "active,suspended" })).toBe('(status = "active" OR status = "suspended")');
  });

  it("builds open and closed number ranges and ignores junk", () => {
    expect(toFilter(defs, { sessions: "2..5" })).toBe("sessions >= 2 AND sessions <= 5");
    expect(toFilter(defs, { sessions: "..5" })).toBe("sessions <= 5");
    expect(toFilter(defs, { sessions: "x..y" })).toBe("");
  });

  it("turns local dates into a half-open UTC range", () => {
    const from = new Date(2026, 10, 1).toISOString();
    const to = new Date(2026, 10, 21).toISOString();
    expect(toFilter(defs, { joined: "2026-11-01..2026-11-20" })).toBe(`joined >= "${from}" AND joined < "${to}"`);
  });

  it("combines several columns with AND", () => {
    expect(toFilter(defs, { actor: "e848b9e1-316b-4399-8740-61a7de46b5a9", rated: "true", email: "a" }))
      .toBe('email:"a" AND actor = "e848b9e1-316b-4399-8740-61a7de46b5a9" AND rated = true');
  });
});

describe("sorting", () => {
  it("round-trips through the URL form", () => {
    expect(parseSort("-joined")).toEqual({ field: "joined", desc: true });
    expect(formatSort({ field: "email", desc: false })).toBe("email");
    expect(toOrderBy(parseSort("-joined"))).toBe("joined desc");
    expect(toOrderBy(null)).toBe("");
  });
});

describe("pageWindow", () => {
  it("lists every page when there are few", () => {
    expect(pageWindow(2, 5)).toEqual([1, 2, 3, 4, 5]);
  });

  it("keeps the ends and neighbours with gaps between", () => {
    expect(pageWindow(10, 40)).toEqual([1, null, 9, 10, 11, null, 40]);
    expect(pageWindow(1, 40)).toEqual([1, 2, 3, 4, null, 40]);
    expect(pageWindow(40, 40)).toEqual([1, null, 37, 38, 39, 40]);
  });
});

describe("localRows", () => {
  const rows = [
    { name: "admin", members: 1, system: "builtin", updated: new Date(2026, 10, 1, 12) },
    { name: "Moderator", members: 12, system: "custom", updated: new Date(2026, 10, 20, 9) },
    { name: "support", members: 4, system: "custom", updated: undefined },
  ];
  const local: FilterDef[] = [
    { field: "name", label: "Name", kind: FilterKind.Text },
    { field: "kind", label: "Kind", kind: FilterKind.Choice },
    { field: "members", label: "Members", kind: FilterKind.Number },
    { field: "updated", label: "Updated", kind: FilterKind.Date },
  ];
  const read = (row: (typeof rows)[number], field: string) => (field === "kind" ? row.system : row[field as keyof typeof row]);

  it("filters text case-insensitively and by choice, range and day", () => {
    expect(localRows(rows, local, { name: "MOD" }, null, read).map((r) => r.name)).toEqual(["Moderator"]);
    expect(localRows(rows, local, { kind: "custom", members: "..10" }, null, read).map((r) => r.name)).toEqual(["support"]);
    expect(localRows(rows, local, { updated: "2026-11-20..2026-11-20" }, null, read).map((r) => r.name)).toEqual(["Moderator"]);
  });

  it("sorts both ways and keeps missing values last", () => {
    expect(localRows(rows, local, {}, { field: "members", desc: true }, read).map((r) => r.name)).toEqual(["Moderator", "support", "admin"]);
    expect(localRows(rows, local, {}, { field: "updated", desc: true }, read).map((r) => r.name)).toEqual(["Moderator", "admin", "support"]);
    expect(localRows(rows, local, {}, { field: "name", desc: false }, read).map((r) => r.name)).toEqual(["admin", "Moderator", "support"]);
  });
});
