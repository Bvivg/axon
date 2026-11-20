const mediaKey = /^[0-9a-f-]{36}\/[0-9a-f-]{36}(?:\.[a-z0-9]{1,8}){0,2}$/;

export function mediaURL(raw: string): string {
  if (!raw) return "";
  try {
    const url = new URL(raw, "http://local.invalid");
    const key = url.pathname.split("/chat-attachments/")[1];
    if (key && mediaKey.test(key)) {
      const base = (process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:18080").replace(/\/$/, "");
      return `${base === "/api" ? "" : base}/api/media/${key}`;
    }
    return url.protocol === "https:" || url.protocol === "http:" || url.protocol === "blob:" ? raw : "";
  } catch {
    return "";
  }
}
