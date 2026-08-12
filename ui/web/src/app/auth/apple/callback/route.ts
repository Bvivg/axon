const callbackPage = "/auth/callback/apple";

interface AppleUser {
  name?: { firstName?: string; lastName?: string };
}

const maxNameLength = 128;

export async function POST(request: Request): Promise<Response> {
  let form: FormData;
  try {
    form = await request.formData();
  } catch {
    return seeOther(new URLSearchParams({ error: "invalid_request" }));
  }

  const query = new URLSearchParams();

  const state = field(form, "state");
  if (state) {
    query.set("state", state);
  }

  const error = field(form, "error");
  if (error) {
    query.set("error", error);
    return seeOther(query);
  }

  const code = field(form, "code");
  if (!code) {
    query.set("error", "invalid_request");
    return seeOther(query);
  }

  query.set("code", code);

  const name = displayName(field(form, "user"));
  if (name) {
    query.set("name", name);
  }

  return seeOther(query);
}

function displayName(raw: string): string {
  if (!raw) {
    return "";
  }

  let user: AppleUser;
  try {
    user = JSON.parse(raw) as AppleUser;
  } catch {

    return "";
  }

  const name = [user.name?.firstName, user.name?.lastName]
    .filter((part): part is string => typeof part === "string" && part.trim() !== "")
    .join(" ")
    .trim();

  return name.slice(0, maxNameLength);
}

function field(form: FormData, name: string): string {
  const value = form.get(name);
  return typeof value === "string" ? value.trim() : "";
}

function seeOther(query: URLSearchParams): Response {
  return new Response(null, {
    status: 303,
    headers: { Location: `${callbackPage}?${query.toString()}` },
  });
}
