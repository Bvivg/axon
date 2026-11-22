import { Code } from "@connectrpc/connect";

import { describe, errorCode, isCode } from "@/lib/errors";

export interface SignInFailure {
  title: string;
  detail: string;
  retryable: boolean;
}

export function signInFailure(err: unknown): SignInFailure {
  if (isCode(err, Code.FailedPrecondition)) {
    return { title: describe(err), detail: `[${errorCode(err)}]`, retryable: false };
  }
  return { title: "Sign-in failed", detail: `[${errorCode(err)}] ${describe(err)}`, retryable: true };
}

export function linkFailure(err: unknown, provider: string): SignInFailure {
  if (isCode(err, Code.AlreadyExists)) {
    return { title: `This ${provider} account is connected to another Axon account`, detail: `[${errorCode(err)}]`, retryable: false };
  }
  if (isCode(err, Code.FailedPrecondition)) {
    return { title: describe(err), detail: `[${errorCode(err)}]`, retryable: false };
  }
  return { title: `Couldn’t connect ${provider}`, detail: `[${errorCode(err)}] ${describe(err)}`, retryable: true };
}
