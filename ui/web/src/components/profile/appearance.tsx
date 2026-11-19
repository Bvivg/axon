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
              <label
                key={choice.theme}
                className="relative flex cursor-pointer flex-col gap-2 text-left"
              >
                <input
                  type="radio"
                  name="appearance-theme"
                  value={choice.theme}
                  checked={checked}
                  onChange={() => setTheme(choice.theme)}
                  className="peer sr-only"
                />
                <span
                  className={cn(
                    "flex h-24 w-full overflow-hidden rounded-lg transition-shadow duration-[120ms] ease-signal peer-focus-visible:outline-2 peer-focus-visible:outline-offset-4 peer-focus-visible:outline-ring",
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
              </label>
            );
          })}
        </div>
      </section>
    </ProfilePage>
  );
}

function Preview({ scheme }: { scheme: "light" | "dark" }) {
  return (
    <span style={{ colorScheme: scheme }} className="flex flex-1 flex-col gap-[5px] bg-background p-2">
      <span className="h-2 w-3/5 rounded-[3px] bg-foreground" />
      <span className="h-1.5 w-[85%] rounded-[3px] bg-border" />
      <span className="h-1.5 w-[70%] rounded-[3px] bg-border" />
      <span className="mt-auto h-3.5 w-1/2 rounded-sm bg-primary" />
    </span>
  );
}
