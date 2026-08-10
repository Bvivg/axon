import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";

import { getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";

/**
 * authenticate attaches the access token and recovers from it having expired.
 *
 * The retry is worth the complexity because the alternative is visible: an
 * access token lives fifteen minutes, so without it every user who leaves a tab
 * open over lunch is thrown back to the sign-in page by their next click.
 *
 * It runs at most once per call. If the refresh fails, the original error is
 * what the caller sees — a retry that hid it would turn "your session ended"
 * into a silent failure.
 */
export const authenticate: Interceptor = (next) => async (req) => {
  const token = getAccessToken();
  if (token) {
    req.header.set("Authorization", `Bearer ${token}`);
  }

  try {
    return await next(req);
  } catch (err) {
    // Streams are not retried: the request body has already been consumed, so
    // there is nothing left to send a second time.
    if (req.stream || ConnectError.from(err).code !== Code.Unauthenticated) {
      throw err;
    }

    if (!(await refreshSession())) {
      throw err;
    }

    req.header.set("Authorization", `Bearer ${getAccessToken() ?? ""}`);
    return await next(req);
  }
};
