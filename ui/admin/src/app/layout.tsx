import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { headers } from "next/headers";
import type { ReactNode } from "react";

import { Toaster } from "@/components/ui/sonner";
import { SessionProvider } from "@/lib/auth/session";
import { QueryProvider } from "@/lib/query/provider";

import "./globals.css";

const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin", "cyrillic"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin", "cyrillic"] });

export const metadata: Metadata = {
  title: { default: "Axon Admin", template: "%s · Axon Admin" },
  robots: { index: false, follow: false },
};

export const dynamic = "force-dynamic";

const themeInitScript =
  '(function(){try{var t=localStorage.getItem("axon-theme");' +
  'if(t==="light"||t==="dark"){document.documentElement.setAttribute("data-theme",t);}' +
  "}catch(e){}})();";

export default async function RootLayout({ children }: { children: ReactNode }) {
  const nonce = (await headers()).get("x-nonce") ?? undefined;
  return (
    <html lang="en" suppressHydrationWarning className={`${geistSans.variable} ${geistMono.variable}`}>
      <head>
        <script nonce={nonce} suppressHydrationWarning dangerouslySetInnerHTML={{ __html: themeInitScript }} />
      </head>
      <body className="min-h-dvh">
        <QueryProvider>
          <SessionProvider>
            {children}
            <Toaster />
          </SessionProvider>
        </QueryProvider>
      </body>
    </html>
  );
}
