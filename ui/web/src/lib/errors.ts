import { Code, ConnectError } from "@connectrpc/connect";

/**
 * Turns a failed call into something worth showing a person.
 *
 * The mapping is deliberately coarse. In particular a failed sign-in says only
 * that the pair did not work: the server goes out of its way not to reveal
 * whether the address exists, and a friendlier message here would give away
 * exactly what it withholds.
 */
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
      // These carry a reason the server wrote for a person to read — which
      // field was rejected, which provider is not available.
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
