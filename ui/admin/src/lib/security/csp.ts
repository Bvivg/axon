export type PolicySources = {
  gateway: string;
  assets?: string;
  development: boolean;
};

const origin = (value: string | undefined): string | undefined => {
  if (!value || value.startsWith("/")) return undefined;
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:" ? url.origin : undefined;
  } catch {
    return undefined;
  }
};

const socketOrigin = (httpOrigin: string) => httpOrigin.replace(/^http/, "ws");

export function newNonce(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(18));
  return btoa(String.fromCharCode(...bytes));
}

export function contentSecurityPolicy(nonce: string, { gateway, assets, development }: PolicySources): string {
  const api = origin(gateway);
  const store = origin(assets);
  const list = (...sources: (string | undefined | false)[]) => [...new Set(sources.filter((source): source is string => Boolean(source)))].join(" ");

  return [
    `default-src 'self'`,
    `script-src ${list("'self'", `'nonce-${nonce}'`, "'strict-dynamic'", development && "'unsafe-eval'")}`,
    `style-src 'self' 'unsafe-inline'`,
    `img-src ${list("'self'", "data:", "blob:", api, store)}`,
    `media-src ${list("'self'", "data:", "blob:", api)}`,
    `font-src 'self' data:`,
    `connect-src ${list("'self'", api, api && socketOrigin(api))}`,
    `worker-src 'self' blob:`,
    `manifest-src 'self'`,
    `frame-src 'self'`,
    `object-src 'none'`,
    `base-uri 'self'`,
    `form-action 'self'`,
    `frame-ancestors 'none'`,
  ].join("; ");
}
