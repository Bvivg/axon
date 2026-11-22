import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";

import { getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";

export const authenticate: Interceptor = (next) => async (req) => {
  const token = getAccessToken();
  if (token) {
    req.header.set("Authorization", `Bearer ${token}`);
  }

  try {
    return await next(req);
  } catch (err) {

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
