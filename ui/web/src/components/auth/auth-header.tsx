"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { BrandMark } from "@/components/nav/brand";
import { buttonVariants } from "@/components/ui/button";

export function AuthHeader() {
  const pathname = usePathname();
  const onRegister = pathname.startsWith("/register");

  return (
    <header className="relative flex h-14 shrink-0 items-center justify-between pr-2 pl-4 md:px-6">
      <span className="flex items-center gap-2.5">
        <BrandMark className="size-7 rounded-[7px]" />
        <span className="font-semibold">Axon</span>
      </span>
      <Link
        href={onRegister ? "/login" : "/register"}
        className={buttonVariants({ variant: "ghost", size: "default" })}
      >
        {onRegister ? "Sign in" : "Create account"}
      </Link>
    </header>
  );
}
