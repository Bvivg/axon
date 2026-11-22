import { expect, test, type Page } from "@playwright/test";
import pg from "pg";

import { newPerson, open } from "./support";

const dsn = process.env.ADMIN_POSTGRES_DSN;

async function grant(email: string, role: string): Promise<void> {
  const client = new pg.Client({ connectionString: dsn });
  await client.connect();
  try {
    const result = await client.query(
      "INSERT INTO user_roles (user_id, role_id) SELECT u.id, r.id FROM users u, roles r WHERE lower(u.email) = lower($1) AND r.name = $2 ON CONFLICT DO NOTHING",
      [email, role],
    );
    expect(result.rowCount).toBe(1);
  } finally {
    await client.end();
  }
}

async function confirmWithReason(page: Page, button: string, reason: string): Promise<void> {
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused();
  await dialog.getByLabel("Reason").fill(reason);
  await dialog.getByRole("button", { name: button }).click();
  await expect(dialog).toBeHidden();
}

test.skip(!dsn, "ADMIN_POSTGRES_DSN is not set; run via make test-web-e2e");

test("an administrator suspends and restores an account, and the audit log records it", async ({ browser }) => {
  const admin = await newPerson(browser, "admin-console", "Console Admin");
  const target = await newPerson(browser, "admin-target", "Reported Person");
  const page = admin.page;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  try {
    await grant(admin.email, "admin");
    await open(page, "/admin");
    await expect(page.locator("#console-content").getByRole("heading", { name: "Administration", level: 1 })).toBeVisible();
    const sidebar = page.getByRole("navigation", { name: "Administration" });
    for (const section of ["Users", "Roles", "Games", "Match queue", "Tournaments", "Ratings", "Analysis", "Chats", "Uploads", "Calls", "Audit log", "Gateway"]) {
      await expect(sidebar.getByRole("link", { name: section, exact: true })).toBeVisible();
    }

    await page.goto(`/admin/users?query=${encodeURIComponent(target.email)}`);
    const users = page.getByRole("table", { name: "Users" });
    await expect(users.getByRole("row")).toHaveCount(2);
    await users.getByRole("link", { name: /Reported Person/ }).click();
    await expect(page).toHaveURL(/\/admin\/users\/[0-9a-f-]{36}$/);
    const userID = page.url().split("/").at(-1)!;
    await expect(page.getByRole("heading", { name: "Reported Person", level: 1 })).toBeVisible();

    await page.getByRole("button", { name: "Suspend" }).click();
    const dialog = page.getByRole("alertdialog");
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused();
    await dialog.getByLabel("Reason").fill("x");
    await dialog.getByRole("button", { name: "Suspend account" }).click();
    await expect(dialog.getByLabel("Reason")).toHaveAttribute("aria-invalid", "true");
    await dialog.getByLabel("Reason").fill("");
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();

    await page.getByRole("button", { name: "Suspend" }).click();
    await confirmWithReason(page, "Suspend account", "spam reported by three people");
    await expect(page.getByText("Suspended", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("definition").filter({ hasText: "spam reported by three people" })).toBeVisible();

    await target.page.goto("/chat");
    await expect(target.page).toHaveURL(/\/login/);

    await page.getByRole("button", { name: "Restore" }).click();
    await confirmWithReason(page, "Restore account", "appeal accepted after review");
    await expect(page.getByText("Active", { exact: true }).first()).toBeVisible();

    await page.goto(`/admin/audit?f.target_type=user&f.target=${userID}`);
    const audit = page.getByRole("table", { name: "Audit events" });
    await expect(async () => {
      await page.reload();
      await expect(audit.getByRole("button", { name: "user_suspended" })).toBeVisible({ timeout: 2_000 });
      await expect(audit.getByRole("button", { name: "user_restored" })).toBeVisible({ timeout: 2_000 });
    }).toPass({ timeout: 45_000 });
    await audit.getByRole("button", { name: "user_suspended" }).click();
    const event = page.getByRole("dialog");
    await expect(event.getByText("spam reported by three people")).toBeVisible();
    await event.getByRole("button", { name: "Close" }).click();

    await audit.getByRole("columnheader", { name: /^Outcome/ }).getByRole("button").click();
    await page.getByRole("group", { name: "Outcome" }).getByRole("checkbox", { name: "Denied" }).check();
    await expect(page).toHaveURL(/f\.outcome=denied/);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("heading", { name: "Nothing matches these filters" })).toBeVisible();
    await page.getByRole("button", { name: "Clear filters" }).click();
    await expect(page).toHaveURL(/\/admin\/audit$/);

    await page.goto("/admin/users");
    const everyone = page.getByRole("table", { name: "Users" });
    await everyone.getByRole("columnheader", { name: /^Joined/ }).getByRole("button").click();
    await page.getByRole("button", { name: "Ascending" }).click();
    await expect(page).toHaveURL(/sort=joined/);
    await expect(everyone.getByRole("columnheader", { name: /^Joined/ })).toHaveAttribute("aria-sort", "ascending");
    const pages = page.getByRole("navigation", { name: "Pages" });
    await expect(pages.getByText(/^1–\d+ of [\d,]+$/)).toBeVisible();

    for (const [path, title] of [["/admin/games", "Games"], ["/admin/queue", "Match queue"], ["/admin/tournaments", "Tournaments"], ["/admin/ratings", "Ratings"], ["/admin/ratings?view=changes", "Ratings"], ["/admin/analysis", "Analysis"], ["/admin/chats", "Chats"], ["/admin/uploads", "Uploads"], ["/admin/calls", "Calls"], ["/admin/roles", "Roles"]] as const) {
      await page.goto(path);
      await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible();
      await expect(page.getByText(/^Could not load/)).toHaveCount(0);
    }
    expect(errors).toEqual([]);
  } finally {
    await admin.close();
    await target.close();
  }
});

test("an administrator creates, edits and deletes a role, with field errors and edit conflicts", async ({ browser }) => {
  const admin = await newPerson(browser, "admin-roles", "Role Admin");
  const page = admin.page;
  const second = await page.context().newPage();
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const name = `e2e_${Date.now().toString(36)}`;
  try {
    await grant(admin.email, "admin");
    await open(page, "/admin/roles");
    await expect(page.getByRole("table", { name: "Roles" }).getByRole("link", { name: /moderator/ })).toBeVisible();
    await page.getByRole("link", { name: "New role" }).click();
    await expect(page).toHaveURL(/\/admin\/roles\/new$/);

    const form = page.getByRole("form", { name: "New role" });
    await form.getByLabel("Name").fill("Bad Name");
    await form.getByLabel("Title").fill("x");
    await form.getByRole("button", { name: "Create role" }).click();
    await expect(form.getByLabel("Name")).toHaveAttribute("aria-invalid", "true");
    await expect(form.getByLabel("Title")).toHaveAttribute("aria-invalid", "true");
    await expect(form.getByLabel("Reason")).toHaveAttribute("aria-invalid", "true");

    await form.getByLabel("Name").fill("moderator");
    await form.getByLabel("Title").fill("Duplicate moderators");
    await form.getByRole("group", { name: "Permissions" }).getByRole("checkbox").first().check();
    await form.getByLabel("Reason").fill("checking the unique name rule");
    await form.getByRole("button", { name: "Create role" }).click();
    await expect(form.getByText("A role with this name already exists")).toBeVisible();
    await expect(form.getByLabel("Name")).toHaveAttribute("aria-invalid", "true");

    await form.getByLabel("Name").fill(name);
    await form.getByLabel("Title").fill("Support desk");
    await form.getByRole("button", { name: "Create role" }).click();
    await expect(page).toHaveURL(/\/admin\/roles\/[0-9a-f-]{36}$/);
    const roleURL = new URL(page.url()).pathname;
    await expect(page.getByRole("heading", { name: "Support desk", level: 1 })).toBeVisible();

    await page.getByRole("link", { name: "Edit" }).click();
    await second.goto(`${roleURL}/edit`);
    const mine = page.getByRole("form", { name: `Edit role ${name}` });
    const theirs = second.getByRole("form", { name: `Edit role ${name}` });
    await expect(mine.getByLabel("Name")).toBeDisabled();
    await expect(mine.getByRole("button", { name: "Save changes" })).toBeDisabled();

    await theirs.getByLabel("Title").fill("Support desk, nights");
    await theirs.getByLabel("Reason").fill("night shift needs its own title");
    await theirs.getByRole("button", { name: "Save changes" }).click();
    await expect(second.getByRole("heading", { name: "Support desk, nights", level: 1 })).toBeVisible();

    await mine.getByLabel("Title").fill("Support desk, days");
    await mine.getByLabel("Reason").fill("day shift wants a clearer title");
    await mine.getByRole("button", { name: "Save changes" }).click();
    const conflict = mine.getByRole("alert").filter({ hasText: "Someone else saved this record" });
    await expect(conflict).toBeVisible();
    await expect(conflict.getByText("theirs: Support desk, nights · yours: Support desk, days")).toBeVisible();
    await conflict.getByRole("button", { name: "Keep my values and reload" }).click();
    await expect(conflict).toBeHidden();
    await mine.getByRole("button", { name: "Save changes" }).click();
    await expect(page.getByRole("heading", { name: "Support desk, days", level: 1 })).toBeVisible();

    const roleID = roleURL.split("/").pop();
    await page.goto(`/admin/audit?f.target_type=role&f.target=${roleID}&f.action=role_updated`);
    const audit = page.getByRole("table", { name: "Audit events" });
    await expect(async () => {
      await page.reload();
      await expect(audit.getByRole("button", { name: "role_updated" })).toHaveCount(2, { timeout: 2_000 });
    }).toPass({ timeout: 45_000 });
    await audit.getByRole("button", { name: "role_updated" }).first().click();
    const changes = page.getByRole("dialog").getByRole("table", { name: "Changed fields" });
    await expect(changes.getByRole("row", { name: /title Support desk, nights Support desk, days/ })).toBeVisible();
    await page.getByRole("dialog").getByRole("button", { name: "Close" }).click();

    await page.goto(roleURL);
    await page.getByRole("button", { name: "Delete" }).click();
    await confirmWithReason(page, "Delete role", "the experiment is over");
    await expect(page).toHaveURL(/\/admin\/roles$/);
    await expect(page.getByRole("table", { name: "Roles" }).getByText(name)).toHaveCount(0);
    expect(errors).toEqual([]);
  } finally {
    await admin.close();
  }
});

test("a moderator sees moderation pages but not account suspension or gateway telemetry", async ({ browser }) => {
  const moderator = await newPerson(browser, "admin-moderator", "Shift Moderator");
  const target = await newPerson(browser, "admin-moderated", "Quiet Person");
  const page = moderator.page;
  try {
    await grant(moderator.email, "moderator");
    await open(page, "/admin");
    const sidebar = page.getByRole("navigation", { name: "Administration" });
    await expect(sidebar.getByRole("link", { name: "Chats", exact: true })).toBeVisible();
    await expect(sidebar.getByRole("link", { name: "Gateway", exact: true })).toHaveCount(0);

    await page.goto(`/admin/users?query=${encodeURIComponent(target.email)}`);
    await page.getByRole("table", { name: "Users" }).getByRole("link", { name: /Quiet Person/ }).click();
    await expect(page.getByRole("heading", { name: "Quiet Person", level: 1 })).toBeVisible();
    await expect(page.getByRole("button", { name: "Suspend" })).toHaveCount(0);

    await page.goto("/admin/gateway");
    await expect(page.getByRole("heading", { name: "Only administrators can open this page" })).toBeVisible();
  } finally {
    await moderator.close();
    await target.close();
  }
});
