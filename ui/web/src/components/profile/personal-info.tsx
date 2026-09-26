"use client";

import { Check } from "lucide-react";
import Image from "next/image";
import { useRef, useState, type ChangeEvent, type FormEvent } from "react";

import { ProfilePage, ProfileSection } from "@/components/profile/profile-page";
import { Alert, FieldError } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/dialog";
import { Input, InputGroup, InputGroupInput } from "@/components/ui/input";
import { Field, FieldHint, Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/status";
import type { User } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { uploadAvatar, withUploadedAvatar } from "@/lib/avatar/upload";
import { describe } from "@/lib/errors";
import { initials, personName } from "@/lib/format";
import { useUpdateProfile } from "@/lib/query/profile";

const maxAvatarBytes = 5 * 1024 * 1024;

export function PersonalInfo({ user }: { user: User }) {
  const { updateUser } = useSession();
  const updateProfile = useUpdateProfile();

  const [email, setEmail] = useState(user.email);
  const [firstName, setFirstName] = useState(user.firstName ?? "");
  const [lastName, setLastName] = useState(user.lastName ?? "");
  const [nickname, setNickname] = useState(user.nickname ?? "");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [avatarError, setAvatarError] = useState<string | null>(null);
  const [avatarPending, setAvatarPending] = useState(false);
  const [viewing, setViewing] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setSaved(false);

    try {
      const next = await updateProfile.mutateAsync({
        email: email.trim() !== user.email ? email.trim() : undefined,
        firstName: firstName.trim() !== (user.firstName ?? "") ? firstName.trim() : undefined,
        lastName: lastName.trim() !== (user.lastName ?? "") ? lastName.trim() : undefined,
        nickname: nickname.trim() !== (user.nickname ?? "") ? nickname.trim() : undefined,
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
      setAvatarError(err instanceof Error ? err.message : "Could not upload that image.");
    } finally {
      setAvatarPending(false);
    }
  }

  const name = personName(user);
  const avatarSrc = user.avatarUrls?.medium || user.avatarUrl;
  const avatarLarge = user.avatarUrls?.original || user.avatarUrls?.large || avatarSrc;
  const emailChanged = email.trim() !== user.email;

  return (
    <ProfilePage title="Personal info">
      <ProfileSection id="photo-heading" label="Photo">
        <div className="flex items-center gap-4">
          <button
            type="button"
            aria-label="View photo"
            disabled={!avatarSrc}
            onClick={() => setViewing(true)}
            className="rounded-full disabled:cursor-default"
          >
            <Avatar
              self
              src={avatarSrc}
              alt={name}
              fallback={initials(name)}
              sizes="64px"
              priority
              className="size-16 text-lg"
            />
          </button>
          <div className="flex flex-col gap-2">
            <input
              ref={fileInput}
              type="file"
              accept="image/*"
              className="hidden"
              onChange={(e) => void pickAvatar(e)}
            />
            <Button
              variant="outline"
              disabled={avatarPending}
              aria-busy={avatarPending}
              onClick={() => fileInput.current?.click()}
            >
              {avatarPending ? <Spinner /> : null}
              {avatarPending ? "Uploading…" : "Change photo"}
            </Button>
            {avatarError ? <FieldError>{avatarError}</FieldError> : null}
          </div>
        </div>
      </ProfileSection>

      <ProfileSection id="account-heading" label="Account">
        <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
          {error ? <Alert>{error}</Alert> : null}

          <Field>
            <Label htmlFor="email">Email</Label>
            <InputGroup>
              <InputGroupInput
                id="email"
                type="email"
                required
                value={email}
                aria-describedby={emailChanged ? "email-hint" : undefined}
                onChange={(e) => setEmail(e.target.value)}
              />
              {emailChanged ? null : user.emailVerified ? (
                <Badge variant="success">Verified</Badge>
              ) : (
                <Badge variant="warning">Unverified</Badge>
              )}
            </InputGroup>
            {emailChanged ? (
              <FieldHint id="email-hint">Changing your email marks it unverified again.</FieldHint>
            ) : null}
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <Label htmlFor="firstName">First name</Label>
              <Input id="firstName" value={firstName} onChange={(e) => setFirstName(e.target.value)} />
            </Field>
            <Field>
              <Label htmlFor="lastName">Last name</Label>
              <Input id="lastName" value={lastName} onChange={(e) => setLastName(e.target.value)} />
            </Field>
          </div>

          <Field>
            <Label htmlFor="nickname">
              Nickname <span className="font-normal text-muted-foreground">· optional</span>
            </Label>
            <InputGroup>
              <span aria-hidden className="font-mono text-muted-foreground">
                @
              </span>
              <InputGroupInput id="nickname" value={nickname} onChange={(e) => setNickname(e.target.value)} />
            </InputGroup>
          </Field>

          <div className="flex items-center gap-3 pt-2">
            <Button type="submit" disabled={updateProfile.isPending} aria-busy={updateProfile.isPending}>
              {updateProfile.isPending ? <Spinner /> : null}
              {updateProfile.isPending ? "Saving…" : "Save changes"}
            </Button>
            {saved ? (
              <span role="status" className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <Check aria-hidden className="size-4 text-success" strokeWidth={2} />
                Changes saved
              </span>
            ) : null}
          </div>
        </form>
      </ProfileSection>

      {avatarLarge ? (
        <Dialog open={viewing} onOpenChange={setViewing}>
          <DialogContent aria-label="Photo" className="w-[min(calc(100vw-2rem),32rem)] p-2">
            <div className="relative aspect-square w-full overflow-hidden rounded-lg bg-muted">
              <Image src={avatarLarge} alt={name} fill unoptimized sizes="90vw" className="object-cover" />
            </div>
            <DialogClose className="absolute -top-9 right-0 text-sm text-white/85 hover:text-white">
              Close
            </DialogClose>
          </DialogContent>
        </Dialog>
      ) : null}
    </ProfilePage>
  );
}
