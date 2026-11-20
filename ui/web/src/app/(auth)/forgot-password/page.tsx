import Link from "next/link";

import { Alert } from "@/components/ui/alert";

export default function ForgotPasswordPage() {
  return (
    <>
      <h1>Reset password</h1>
      <Alert variant="warning">Password reset is currently unavailable.</Alert>
      <Link href="/login" className="w-fit rounded-lg py-2 text-sm text-signal hover:underline">Back to sign in</Link>
    </>
  );
}
