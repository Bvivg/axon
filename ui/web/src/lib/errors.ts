import { Code, ConnectError } from "@connectrpc/connect";

export function describe(err: unknown): string {
  const connectErr = ConnectError.from(err);

  switch (connectErr.code) {
    case Code.Unauthenticated:
      return "Incorrect email or password.";

    case Code.AlreadyExists:
      return "That email is already registered.";

    case Code.ResourceExhausted:
      return "Too many attempts. Wait a minute and try again.";

    case Code.InvalidArgument:
    case Code.FailedPrecondition:

      return capitalise(connectErr.rawMessage);

    case Code.Unavailable:
      return "Cannot reach the server. Check that the stack is running.";

    default:
      return "Something went wrong. Try again.";
  }
}

function capitalise(text: string): string {
  if (!text) {
    return "Something went wrong. Try again.";
  }
  return text.charAt(0).toUpperCase() + text.slice(1);
}
