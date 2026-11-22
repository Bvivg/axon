import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";

import { rateLimitRetries, retryAfterMs, waitForRetry } from "@/lib/rate-limit";

export const retryRateLimit: Interceptor = (next) => async (request) => {
  if (request.stream) return next(request);
  for (let retry = 0; ; retry += 1) {
    request.signal.throwIfAborted();
    try {
      return await next(request);
    } catch (error) {
      const rpcError = ConnectError.from(error);
      if (rpcError.code !== Code.ResourceExhausted || retry >= rateLimitRetries) throw error;
      await waitForRetry(retryAfterMs(rpcError.metadata.get("Retry-After")), request.signal);
    }
  }
};
