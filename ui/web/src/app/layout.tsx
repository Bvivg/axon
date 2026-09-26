import type { Metadata } from "next";
import type { ReactNode } from "react";

import { PresenceBeacon } from "@/components/presence/presence-beacon";
import { SessionProvider } from "@/lib/auth/session";
import { QueryProvider } from "@/lib/query/provider";

import "./globals.css";

export const metadata: Metadata = {
  title: "Axon",
  description: "Games, chat and calls over one gateway.",
};

const themeInitScript =
  '(function(){try{var t=localStorage.getItem("axon-theme");' +
  'if(t==="light"||t==="dark"){document.documentElement.setAttribute("data-theme",t);}' +
  "}catch(e){}})();";

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInitScript }} />
      </head>
      <body className="min-h-screen antialiased">
        <QueryProvider>
          <SessionProvider>
            <PresenceBeacon />
            {children}
          </SessionProvider>
        </QueryProvider>
      </body>
    </html>
  );
}
