import Link from "next/link";

import { CredentialsForm } from "@/components/auth/credentials-form";
import { ProviderButtons } from "@/components/auth/provider-buttons";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function LoginPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Sign in</CardTitle>
        <CardDescription>Welcome back.</CardDescription>
      </CardHeader>

      <CardContent className="space-y-6">
        <CredentialsForm mode="login" />
        <ProviderButtons returnTo="/profile" />
      </CardContent>

      <CardFooter className="text-muted-foreground">
        No account?&nbsp;
        <Link className="underline underline-offset-4" href="/register">
          Create one
        </Link>
      </CardFooter>
    </Card>
  );
}
