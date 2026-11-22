import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, it, expect } from "vitest";

import { ViolationsSchema } from "@/gen/buf/validate/validate_pb";

import { describe as describeError, fieldErrors, isConflict, retryAfterMessage } from "./errors";

describe("rate limit feedback", () => {
  it("uses Retry-After from the server", () => {
    const error = new ConnectError("too many requests", Code.ResourceExhausted, { "Retry-After": "6" });
    expect(describeError(error)).toBe("Too many requests. Try again in 6 seconds.");
  });
  it("handles one second and missing or malformed metadata", () => {
    expect(retryAfterMessage(new ConnectError("", Code.ResourceExhausted, { "Retry-After": "1" }))).toBe("Try again in 1 second.");
    for (const value of ["", "garbage", "0", "-1"]) {
      expect(retryAfterMessage(new ConnectError("", Code.ResourceExhausted, { "Retry-After": value }))).toBe("Try again shortly.");
    }
  });
});

function violation(path: string[], message: string) {
  return create(ViolationsSchema, {
    violations: [{ field: { elements: path.map((fieldName) => ({ fieldName })) }, message }],
  });
}

describe("server field errors", () => {
  it("maps the last path segment to the camelCase form field", () => {
    const error = new ConnectError("invalid", Code.InvalidArgument, undefined, [
      { desc: ViolationsSchema, value: violation(["role", "display_name"], "must be at least 2 characters") },
      { desc: ViolationsSchema, value: violation(["name"], "a role with this name already exists") },
    ]);
    expect(fieldErrors(error)).toEqual({ displayName: "Must be at least 2 characters", name: "A role with this name already exists" });
  });
  it("keeps the first message per field and ignores errors without details", () => {
    const error = new ConnectError("invalid", Code.InvalidArgument, undefined, [
      { desc: ViolationsSchema, value: violation(["title"], "first") },
      { desc: ViolationsSchema, value: violation(["title"], "second") },
    ]);
    expect(fieldErrors(error)).toEqual({ title: "First" });
    expect(fieldErrors(new ConnectError("boom", Code.Internal))).toEqual({});
  });
  it("treats aborted as an edit conflict", () => {
    expect(isConflict(new ConnectError("changed", Code.Aborted))).toBe(true);
    expect(isConflict(new ConnectError("gone", Code.NotFound))).toBe(false);
    expect(describeError(new ConnectError("changed", Code.Aborted))).toMatch(/Someone else changed/);
  });
});
