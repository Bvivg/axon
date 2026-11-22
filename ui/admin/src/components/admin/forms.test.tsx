import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import { reasonError } from "@/components/admin/resource-form";
import { changedRolePaths, checkRole, roleDifferences } from "@/components/admin/roles/role-form";
import { changedTournamentPaths, checkTournament } from "@/components/admin/tournaments/tournament-form";
import { AdminRoleSchema } from "@/gen/axon/auth/v1/admin_pb";
import { TournamentFormat, TournamentSchema } from "@/gen/axon/tournament/v1/tournament_pb";

const role = create(AdminRoleSchema, { id: "4f1c0d8e-2b3a-4c5d-8e9f-0a1b2c3d4e5f", name: "support", title: "Support", permissions: ["admin.users:read", "admin.audit:read"], etag: 'W/"3"' });

describe("role form", () => {
  it("masks only the fields that changed, ignoring permission order and padding", () => {
    expect(changedRolePaths(role, { name: "support", title: " Support ", permissions: ["admin.audit:read", "admin.users:read"], reason: "" })).toEqual([]);
    expect(changedRolePaths(role, { name: "support", title: "Support desk", permissions: ["admin.users:read"], reason: "" })).toEqual(["title", "permissions"]);
  });
  it("validates with the proto rules and the reason length", () => {
    expect(checkRole({ name: "Bad Name", title: "x", permissions: [], reason: "" })).toEqual({
      name: "Use 2 to 32 lowercase letters, digits or underscores, starting with a letter",
      title: "Must be at least 2 characters",
      reason: "Write at least 3 characters",
    });
    expect(checkRole({ name: "night_shift", title: "Night shift", permissions: ["admin.users:read"], reason: "new team" })).toEqual({});
  });
  it("lists what differs between the saved version and the form", () => {
    const theirs = create(AdminRoleSchema, { ...role, title: "Support, nights" });
    expect(roleDifferences(theirs, { name: "support", title: "Support, days", permissions: ["admin.users:read", "admin.audit:read"], reason: "" })).toEqual([
      { field: "title", theirs: "Support, nights", yours: "Support, days" },
    ]);
  });
});

describe("tournament form", () => {
  const swiss = create(TournamentSchema, { id: "8a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", name: "League", format: TournamentFormat.SWISS, capacity: 8, totalRounds: 3, playerCount: 5, etag: 'W/"2"' });
  const bracket = create(TournamentSchema, { ...swiss, format: TournamentFormat.SINGLE_ELIMINATION, capacity: 8, totalRounds: 0, playerCount: 2 });

  it("keeps capacity above the registered players and rounds below capacity", () => {
    expect(checkTournament(swiss)({ name: "League", capacity: 4, rounds: 3, reason: "fix" })).toEqual({ capacity: "5 players are already registered" });
    expect(checkTournament(swiss)({ name: "League", capacity: 8, rounds: 8, reason: "fix" })).toEqual({ rounds: "Play at least one round and fewer rounds than players" });
    expect(checkTournament(bracket)({ name: "Cup", capacity: 6, rounds: 0, reason: "fix" })).toEqual({ capacity: "A bracket holds 4, 8 or 16 players" });
    expect(checkTournament(bracket)({ name: " ", capacity: 16, rounds: 0, reason: "" })).toEqual({ name: "Name the tournament", reason: "Write at least 3 characters" });
  });
  it("sends rounds only for swiss tournaments", () => {
    expect(changedTournamentPaths(swiss, { name: "League", capacity: 10, rounds: 4, reason: "" })).toEqual(["capacity", "rounds"]);
    expect(changedTournamentPaths(bracket, { name: "Cup", capacity: 16, rounds: 4, reason: "" })).toEqual(["name", "capacity"]);
  });
  it("measures the reason in characters, not bytes", () => {
    expect(reasonError("ок")).toBe("Write at least 3 characters");
    expect(reasonError("эмо")).toBeUndefined();
  });
});
