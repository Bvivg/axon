"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { Code, ConnectError } from "@connectrpc/connect";

export function QueryProvider({ children }: { children: ReactNode }) {

  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {

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
