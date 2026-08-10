"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { Code, ConnectError } from "@connectrpc/connect";

export function QueryProvider({ children }: { children: ReactNode }) {
  // Created in state, not at module scope: a module-level client would be one
  // cache shared by every request the server renders.
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            // The transport already retries a call whose access token expired.
            // Retrying here as well would repeat calls the server refused on
            // purpose — a rate limit is not a transient failure.
            retry: (failureCount, error) => {
              const code = ConnectError.from(error).code;
              if (
                code === Code.Unauthenticated ||
                code === Code.PermissionDenied ||
                code === Code.InvalidArgument ||
                code === Code.ResourceExhausted
              ) {
                return false;
              }
              return failureCount < 2;
            },
            staleTime: 30_000,
          },
        },
      }),
  );

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
