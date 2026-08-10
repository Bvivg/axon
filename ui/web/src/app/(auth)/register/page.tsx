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

export default function RegisterPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Create account</CardTitle>
        <CardDescription>
          Registering signs you in straight away — there is no email to confirm
          first.
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-6">
        <CredentialsForm mode="register" />
        <ProviderButtons returnTo="/profile" />
      </CardContent>

      <CardFooter className="text-muted-foreground">
        Already registered?&nbsp;
        <Link className="underline underline-offset-4" href="/login">
          Sign in
        </Link>
      </CardFooter>
    </Card>
  );
}
