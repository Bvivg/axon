"use client";

import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Theme, toggleTheme, useResolvedTheme } from "@/lib/theme";

export function ThemeToggle() {
  const resolved = useResolvedTheme();
  const dark = resolved === Theme.Dark;

  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-10"
      aria-label={dark ? "Switch to light theme" : "Switch to dark theme"}
      onClick={toggleTheme}
    >
      {dark ? <Sun strokeWidth={1.5} /> : <Moon strokeWidth={1.5} />}
    </Button>
  );
}
