import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { signInFailure } from "./oauth-failure";

describe("provider sign-in failures", () => {
  it("leads with the reason when the account cannot be linked", () => {
    const failure = signInFailure(new ConnectError("an account with this email already exists; sign in with your password instead", Code.FailedPrecondition));
    expect(failure).toEqual({
      title: "An account with this email already exists; sign in with your password instead",
      detail: "[failed_precondition]",
      retryable: false,
    });
  });

  it("keeps the generic title and offers a retry for other errors", () => {
    const failure = signInFailure(new ConnectError("oauth state is not valid", Code.InvalidArgument));
    expect(failure).toEqual({ title: "Sign-in failed", detail: "[invalid_argument] Oauth state is not valid", retryable: true });
  });
});
