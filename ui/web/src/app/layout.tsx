import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import type { ReactNode } from "react";

import { PresenceBeacon } from "@/components/presence/presence-beacon";
import { SessionProvider } from "@/lib/auth/session";
import { QueryProvider } from "@/lib/query/provider";

import "./globals.css";

const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin", "cyrillic"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin", "cyrillic"] });

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
    <html
      lang="en"
      suppressHydrationWarning
      className={`${geistSans.variable} ${geistMono.variable}`}
    >
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInitScript }} />
      </head>
      <body className="min-h-dvh">
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
