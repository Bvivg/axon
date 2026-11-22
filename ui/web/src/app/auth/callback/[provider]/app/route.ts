const appProviders = new Set(["google", "github", "fake"]);
const forwarded = ["code", "state", "error", "error_description"];
const maxValueLength = 2048;

export async function GET(request: Request, { params }: { params: Promise<{ provider: string }> }): Promise<Response> {
  const { provider } = await params;
  if (!appProviders.has(provider)) {
    return new Response(null, { status: 404 });
  }

  const incoming = new URL(request.url).searchParams;
  const target = new URL(`axon://oauth/${provider}`);
  for (const key of forwarded) {
    const value = incoming.get(key);
    if (value) {
      target.searchParams.set(key, value.slice(0, maxValueLength));
    }
  }
  if (!target.searchParams.has("code") && !target.searchParams.has("error")) {
    target.searchParams.set("error", "invalid_request");
  }

  return new Response(null, {
    status: 302,
    headers: { Location: target.toString(), "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" },
  });
}
