"use client";

import Image from "next/image";
import { useRef, useState, type ChangeEvent, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogClose, DialogContent, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { User } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { uploadAvatar, withUploadedAvatar } from "@/lib/avatar/upload";
import { describe } from "@/lib/errors";
import { useUpdateProfile } from "@/lib/query/profile";

const maxAvatarBytes = 5 * 1024 * 1024;

export function PersonalInfo({ user }: { user: User }) {
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
  const initial = displayName.slice(0, 1).toUpperCase();
  const avatarSrc = user.avatarUrls?.medium || user.avatarUrl;
  const avatarLarge = user.avatarUrls?.original || user.avatarUrls?.large || avatarSrc;

  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-12">
      <Card>
        <CardHeader>
          <CardTitle>Personal info</CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="flex items-center gap-4">
            {avatarSrc && avatarLarge ? (
              <Dialog>
                <DialogTrigger
                  aria-label="View avatar"
                  className="rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
                >
                  <Avatar
                    src={avatarSrc}
                    alt={displayName}
                    fallback={initial}
                    className="h-16 w-16 text-xl"
                    sizes="64px"
                    priority
                  />
                </DialogTrigger>
                <DialogContent className="w-[min(90vw,32rem)]">
                  <DialogClose
                    aria-label="Close"
                    className="absolute -top-10 right-0 text-sm text-white/80 outline-none hover:text-white"
                  >
                    Close
                  </DialogClose>
                  <div className="relative aspect-square w-full overflow-hidden rounded-2xl bg-muted">
                    <Image
                      src={avatarLarge}
                      alt={displayName}
                      fill
                      unoptimized
                      sizes="90vw"
                      className="object-cover"
                    />
                  </div>
                </DialogContent>
              </Dialog>
            ) : (
              <Avatar
                src={avatarSrc}
                alt={displayName}
                fallback={initial}
                className="h-16 w-16 text-xl"
              />
            )}
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
          </div>

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

      <Button
        variant="outline"
        className="mt-6 w-full"
        onClick={() => void signOut()}
      >
        Sign out
      </Button>
    </div>
  );
}
