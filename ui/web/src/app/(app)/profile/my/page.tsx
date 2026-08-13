"use client";

import { PersonalInfo } from "@/components/profile/personal-info";
import { useSession } from "@/lib/auth/session";

export default function PersonalInfoPage() {
  const { user } = useSession();

  if (!user) {
    return null;
  }

  return <PersonalInfo user={user} />;
}
