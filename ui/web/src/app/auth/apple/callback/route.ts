/**
 * Where Apple returns the browser.
 *
 * Apple is the one provider that does not come back to /auth/callback/<provider>
 * with the others. Asking for the name and email scopes makes its callback an
 * HTML form POST, and a client page cannot receive one — hence a route handler.
 *
 * It sits at its own path rather than at /auth/callback/apple deliberately: a
 * handler mounted on the page's path would also catch the redirect it issues,
 * and the flow would bounce off itself. Here the POST is turned into an ordinary
 * GET of the existing callback page, which then completes the sign-in exactly as
 * it does for every other provider.
 *
 * The address must match OAUTH_APPLE_* on the auth service, which derives it as
 * <web origin>/auth/apple/callback, and must be registered as a Return URL in
 * the Apple developer console.
 */

/** The page that finishes every provider sign-in. */
const callbackPage = "/auth/callback/apple";

/**
 * Apple's `user` field, sent with the first authorization only.
 *
 * It is a JSON string in the form body, and it is the single opportunity to
 * learn the person's name: Apple never repeats it, and the id_token does not
 * carry it. The email in it is ignored — the signed id_token is where the
 * address comes from.
 */
interface AppleUser {
  name?: { firstName?: string; lastName?: string };
}

/** Bounds the name so a hostile POST cannot produce an enormous redirect URL. */
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

  // Apple reports a refusal in the same POST rather than by failing the
  // exchange, and its own wording is what the page shows.
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

  query.set("code", packCode(code, displayName(field(form, "user"))));

  return seeOther(query);
}

/**
 * packCode carries the name through a contract that has room only for a code.
 *
 * CompleteOAuth takes a provider, a code and a state — there is no field for a
 * profile, and adding one for the single provider that needs it would change the
 * contract for every client. So the code the browser carries to the page is the
 * pair, packed as base64url(JSON{code, name}); the Apple provider in the auth
 * service unpacks it. The other side of this format is
 * core/services/auth/internal/oauth/apple.go — the two have to be changed
 * together.
 *
 * With no name there is nothing to carry, and the bare code goes as it arrived —
 * which is what every sign-in after the first one looks like.
 */
function packCode(code: string, name: string): string {
  if (!name) {
    return code;
  }
  return Buffer.from(JSON.stringify({ code, name }), "utf8").toString("base64url");
}

/** displayName reads the name out of Apple's `user` field, if it sent one. */
function displayName(raw: string): string {
  if (!raw) {
    return "";
  }

  let user: AppleUser;
  try {
    user = JSON.parse(raw) as AppleUser;
  } catch {
    // A malformed field costs the name, not the sign-in: the identity itself
    // comes from the id_token and does not depend on this at all.
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

/**
 * seeOther sends the browser on with a GET.
 *
 * 303 rather than 302 because the method has to change: the browser arrived
 * here with a POST, and 302 leaves re-issuing it as the request method up to the
 * browser. The location is relative so this works behind whatever host and
 * scheme the deployment terminates on.
 */
function seeOther(query: URLSearchParams): Response {
  return new Response(null, {
    status: 303,
    headers: { Location: `${callbackPage}?${query.toString()}` },
  });
}
