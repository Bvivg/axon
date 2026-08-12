"use client";

import { useRef, useState, type ChangeEvent, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { User } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { uploadAvatar, withUploadedAvatar } from "@/lib/avatar/upload";
import { describe } from "@/lib/errors";
import { useUpdateProfile } from "@/lib/query/profile";

const maxAvatarBytes = 5 * 1024 * 1024;

export function ProfileTab({ user }: { user: User }) {
  const { updateUser, signOut } = useSession();
  const updateProfile = useUpdateProfile();

  const [email, setEmail] = useState(user.email);
  const [firstName, setFirstName] = useState(user.firstName ?? "");
  const [lastName, setLastName] = useState(user.lastName ?? "");
  const [nickname, setNickname] = useState(user.nickname ?? "");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [avatarError, setAvatarError] = useState<string | null>(null);
  const [avatarPending, setAvatarPending] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setSaved(false);

    try {
      const next = await updateProfile.mutateAsync({
        email: email.trim() !== user.email ? email.trim() : undefined,
        firstName:
          firstName.trim() !== (user.firstName ?? "") ? firstName.trim() : undefined,
        lastName:
          lastName.trim() !== (user.lastName ?? "") ? lastName.trim() : undefined,
        nickname:
          nickname.trim() !== (user.nickname ?? "") ? nickname.trim() : undefined,
      });
      updateUser(next);
      setSaved(true);
    } catch (err) {
      setError(describe(err));
    }
  }

  async function pickAvatar(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }

    if (file.size > maxAvatarBytes) {
      setAvatarError("That image is larger than 5 MB.");
      return;
    }

    setAvatarError(null);
    setAvatarPending(true);
    try {
      const uploaded = await uploadAvatar(file);
      updateUser(withUploadedAvatar(user, uploaded));
    } catch (err) {
      setAvatarError(
        err instanceof Error ? err.message : "Could not upload that image.",
      );
    } finally {
      setAvatarPending(false);
    }
  }

  const displayName = user.displayName || user.email;

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Avatar</CardTitle>
          <CardDescription>
            Imported from Google or GitHub when you sign in, unless you upload
            your own.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex items-center gap-4">
          <Avatar
            src={user.avatarUrls?.medium || user.avatarUrl}
            alt={displayName}
            fallback={displayName.slice(0, 1).toUpperCase()}
            className="h-16 w-16 text-xl"
          />
          <div className="space-y-2">
            {avatarError ? <Alert>{avatarError}</Alert> : null}
            <input
              ref={fileInput}
              type="file"
              accept="image/*"
              className="hidden"
              onChange={(e) => void pickAvatar(e)}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={avatarPending}
              onClick={() => fileInput.current?.click()}
            >
              {avatarPending ? "Uploading…" : "Change avatar"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>My profile</CardTitle>
          <CardDescription>
            Update your email and how your name appears to others.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={(e) => void submit(e)}>
            {error ? <Alert>{error}</Alert> : null}
            {saved ? (
              <p className="text-sm text-muted-foreground">Saved.</p>
            ) : null}

            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
              {email.trim() !== user.email ? (
                <p className="text-xs text-muted-foreground">
                  Changing your email marks it unverified again.
                </p>
              ) : null}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="firstName">First name</Label>
                <Input
                  id="firstName"
                  value={firstName}
                  onChange={(e) => setFirstName(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="lastName">Last name</Label>
                <Input
                  id="lastName"
                  value={lastName}
                  onChange={(e) => setLastName(e.target.value)}
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="nickname">Nickname (optional)</Label>
              <Input
                id="nickname"
                value={nickname}
                onChange={(e) => setNickname(e.target.value)}
              />
            </div>

            <Button type="submit" disabled={updateProfile.isPending}>
              {updateProfile.isPending ? "Saving…" : "Save changes"}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Button variant="outline" className="w-full" onClick={() => void signOut()}>
        Sign out
      </Button>
    </div>
  );
}
