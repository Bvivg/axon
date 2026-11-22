import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { linkFailure, signInFailure } from "./oauth-failure";

describe("provider sign-in failures", () => {
  it("leads with the reason when the account cannot be linked", () => {
    const failure = signInFailure(new ConnectError("an account with this email already exists; sign in with your password, then connect this provider under Profile → Sessions", Code.FailedPrecondition));
    expect(failure).toEqual({
      title: "An account with this email already exists; sign in with your password, then connect this provider under Profile → Sessions",
      detail: "[failed_precondition]",
      retryable: false,
    });
  });

  it("keeps the generic title and offers a retry for other errors", () => {
    const failure = signInFailure(new ConnectError("oauth state is not valid", Code.InvalidArgument));
    expect(failure).toEqual({ title: "Sign-in failed", detail: "[invalid_argument] Oauth state is not valid", retryable: true });
  });
});

describe("provider connection failures", () => {
  it("says the provider account belongs to someone else", () => {
    const failure = linkFailure(new ConnectError("this provider account is already linked to another user", Code.AlreadyExists), "GitHub");
    expect(failure).toEqual({ title: "This GitHub account is connected to another Axon account", detail: "[already_exists]", retryable: false });
  });

  it("offers a retry when the connection simply failed", () => {
    const failure = linkFailure(new ConnectError("oauth state is not valid", Code.InvalidArgument), "GitHub");
    expect(failure).toEqual({ title: "Couldn’t connect GitHub", detail: "[invalid_argument] Oauth state is not valid", retryable: true });
  });
});
