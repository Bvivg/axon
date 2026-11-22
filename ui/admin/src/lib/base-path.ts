export const basePath = "/admin";

export const withBasePath = (path: string) => `${basePath}${path}`;

export const appSignInHref = (next: string) => `/login?next=${encodeURIComponent(next)}`;

export const appHomeHref = "/";
