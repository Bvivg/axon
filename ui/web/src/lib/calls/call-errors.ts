import { Code, ConnectError } from "@connectrpc/connect";
import { retryAfterMessage } from "@/lib/errors";

export type CallIssue = {
  kind: "permission" | "device-busy" | "no-device" | "unsupported" | "peer-busy" | "no-answer" | "disconnected" | "rate-limit" | "unavailable" | "cleanup-pending";
  message: string;
};

export class CallMediaError extends Error {
  constructor(readonly issue: CallIssue) { super(issue.message); }
}

export function callIssue(error: unknown, source: "capture" | "control" = "control"): CallIssue {
  if (error instanceof CallMediaError) return error.issue;
  if (source === "capture" && (error instanceof Error || (typeof DOMException !== "undefined" && error instanceof DOMException))) {
    switch (error.name) {
      case "NotAllowedError":
      case "SecurityError":
        return { kind: "permission", message: "Allow microphone or camera access in your browser, then try again." };
      case "NotReadableError":
      case "AbortError":
        return { kind: "device-busy", message: "Your microphone or camera is busy. Close other apps using it and try again." };
      case "NotFoundError":
      case "OverconstrainedError":
        return { kind: "no-device", message: "The microphone or camera is unavailable. Connect a device and try again." };
      case "NotSupportedError":
        return { kind: "unsupported", message: "This browser does not support the requested media." };
    }
  }
  if (error instanceof ConnectError) {
    if (error.code === Code.ResourceExhausted) return { kind: "rate-limit", message: `Too many requests. ${retryAfterMessage(error)}` };
    if (error.code === Code.FailedPrecondition && error.rawMessage.includes("busy")) return { kind: "peer-busy", message: "You or the other person is already in a call." };
    if (error.code === Code.DeadlineExceeded || error.code === Code.Unavailable) return { kind: "unavailable", message: "Could not reach Axon. Check your connection and try again." };
  }
  return { kind: "unavailable", message: source === "capture" ? "Could not open the microphone or camera. Try again." : "Could not complete the call action. Try again." };
}
