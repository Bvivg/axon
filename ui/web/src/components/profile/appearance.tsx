"use client";

import { ProfilePage } from "@/components/profile/profile-page";
import { setTheme, Theme, useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const choices: { theme: Theme; label: string }[] = [
  { theme: Theme.Light, label: "Light" },
  { theme: Theme.Dark, label: "Dark" },
  { theme: Theme.System, label: "System" },
];

export function Appearance() {
  const current = useTheme();

  return (
    <ProfilePage title="Appearance">
      <section aria-labelledby="theme-heading" className="flex flex-col gap-3 pt-6">
        <h2 id="theme-heading" className="eyebrow">
          Theme
        </h2>
        <div role="radiogroup" aria-labelledby="theme-heading" className="grid grid-cols-3 gap-2.5 sm:gap-4">
          {choices.map((choice) => {
            const checked = current === choice.theme;
            return (
              <button
                key={choice.theme}
                type="button"
                role="radio"
                aria-checked={checked}
                onClick={() => setTheme(choice.theme)}
                className="flex flex-col gap-2 text-left"
              >
                <span
                  className={cn(
                    "flex h-24 w-full overflow-hidden rounded-lg transition-shadow duration-[120ms] ease-signal",
                    checked ? "ring-2 ring-signal" : "ring-1 ring-border hover:ring-muted-foreground",
                  )}
                >
                  {choice.theme !== Theme.Dark ? <Preview scheme="light" /> : null}
                  {choice.theme !== Theme.Light ? <Preview scheme="dark" /> : null}
                </span>
                <span className="flex items-center gap-2 text-sm font-medium">
                  <span
                    aria-hidden
                    className={cn(
                      "size-4 shrink-0 rounded-full",
                      checked ? "border-[5px] border-signal" : "border-[1.5px] border-muted-foreground",
                    )}
                  />
                  {choice.label}
                </span>
              </button>
            );
          })}
        </div>
      </section>
    </ProfilePage>
  );
}

const previewPalettes = {
  light: {
    surface: "bg-[oklch(0.99_0.002_90)]",
    ink: "bg-[oklch(0.18_0.005_90)]",
    rule: "bg-[oklch(0.915_0.004_90)]",
    action: "bg-[oklch(0.2_0.005_90)]",
  },
  dark: {
    surface: "bg-[oklch(0.155_0.004_90)]",
    ink: "bg-[oklch(0.96_0.003_90)]",
    rule: "bg-[oklch(1_0_0/9%)]",
    action: "bg-[oklch(0.96_0.003_90)]",
  },
};

function Preview({ scheme }: { scheme: "light" | "dark" }) {
  const palette = previewPalettes[scheme];
  return (
    <span className={cn("flex flex-1 flex-col gap-[5px] p-2", palette.surface)}>
      <span className={cn("h-2 w-3/5 rounded-[3px]", palette.ink)} />
      <span className={cn("h-1.5 w-[85%] rounded-[3px]", palette.rule)} />
      <span className={cn("h-1.5 w-[70%] rounded-[3px]", palette.rule)} />
      <span className={cn("mt-auto h-3.5 w-1/2 rounded-sm", palette.action)} />
    </span>
  );
}
