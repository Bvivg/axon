import { create } from "@bufbuild/protobuf";

import { AvatarURLsSchema, type User, UserSchema } from "@/gen/axon/auth/v1/auth_pb";
import { apiBaseUrl } from "@/lib/connect/transport";
import { getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";

interface AvatarUploadResponse {
  avatar_url: string;
  avatar_urls: {
    small: string;
    medium: string;
    large: string;
    original: string;
  };
}

export async function uploadAvatar(file: File): Promise<AvatarUploadResponse> {
  let token = getAccessToken();
  if (!token && !(await refreshSession())) {
    throw new Error("not authenticated");
  }
  token = getAccessToken();

  const resp = await fetch(`${apiBaseUrl}/api/avatar`, {
    method: "POST",
    credentials: "include",
    headers: {
      Authorization: `Bearer ${token ?? ""}`,
      "Content-Type": file.type || "application/octet-stream",
    },
    body: file,
  });

  if (!resp.ok) {
    const message = await resp.text();
    throw new Error(message || `avatar upload failed with status ${resp.status}`);
  }

  return (await resp.json()) as AvatarUploadResponse;
}

export function withUploadedAvatar(user: User, uploaded: AvatarUploadResponse): User {
  return create(UserSchema, {
    ...user,
    avatarUrl: uploaded.avatar_url,
    avatarUrls: create(AvatarURLsSchema, uploaded.avatar_urls),
  });
}
