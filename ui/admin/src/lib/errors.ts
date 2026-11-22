import { Code, ConnectError } from "@connectrpc/connect";

import { ViolationsSchema } from "@/gen/buf/validate/validate_pb";

export function describe(err: unknown): string {
  const connectErr = ConnectError.from(err);

  switch (connectErr.code) {
    case Code.Unauthenticated:
      return "Your session has ended. Sign in again.";

    case Code.ResourceExhausted:
      return `Too many requests. ${retryAfterMessage(connectErr)}`;

    case Code.NotFound:
      return connectErr.rawMessage ? capitalise(connectErr.rawMessage) : "Not found.";

    case Code.PermissionDenied:
      return connectErr.rawMessage ? capitalise(connectErr.rawMessage) : "You don't have permission to do that.";

    case Code.Aborted:
      return "Someone else changed this record. Reload it and apply your changes again.";

    case Code.InvalidArgument:
    case Code.FailedPrecondition:
    case Code.AlreadyExists:
      return capitalise(connectErr.rawMessage);

    case Code.Unavailable:
      return "Cannot reach the server. Check that the stack is running.";

    default:
      return "Something went wrong. Try again.";
  }
}

export function errorCode(err: unknown): string {
  const code = ConnectError.from(err).code;
  return (Code[code] ?? "unknown").replace(/([a-z])([A-Z])/g, "$1_$2").toLowerCase();
}

export function isCode(err: unknown, code: Code): boolean {
  return ConnectError.from(err).code === code;
}

export function isConflict(err: unknown): boolean {
  return isCode(err, Code.Aborted);
}

function camel(name: string): string {
  return name.replace(/_([a-z0-9])/g, (_, letter: string) => letter.toUpperCase());
}

export function fieldErrors(err: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  for (const violations of ConnectError.from(err).findDetails(ViolationsSchema)) {
    for (const violation of violations.violations) {
      const elements = violation.field?.elements ?? [];
      const name = elements.at(-1)?.fieldName;
      if (!name) continue;
      const key = camel(name);
      out[key] ??= capitalise(violation.message || "This value is not valid");
    }
  }
  return out;
}

function capitalise(text: string): string {
  if (!text) {
    return "Something went wrong. Try again.";
  }
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function retryAfterSeconds(err: unknown): number | undefined {
  const raw = ConnectError.from(err).metadata.get("Retry-After");
  const seconds = raw === null ? NaN : Number(raw);
  return Number.isFinite(seconds) && seconds > 0 ? Math.ceil(seconds) : undefined;
}

export function retryAfterMessage(err: unknown): string {
  const seconds = retryAfterSeconds(err);
  return seconds ? `Try again in ${seconds} ${seconds === 1 ? "second" : "seconds"}.` : "Try again shortly.";
}
