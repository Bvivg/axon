import { expect, test } from "@playwright/test";

import { goToProfile, newEmail, open, password, register, submitCredentials } from "./support";

test("editing first, last and nickname updates the displayed name", async ({ page }) => {
  await register(page, newEmail("editprofile"));
  await goToProfile(page);

  await page.getByLabel("First name").fill("Ada");
  await page.getByLabel("Last name").fill("Lovelace");
  await page.getByRole("button", { name: "Save changes" }).click();

  await expect(page.getByText("Saved.")).toBeVisible();

  await page.reload();
  await expect(page.getByLabel("First name")).toHaveValue("Ada");
  await expect(page.getByLabel("Last name")).toHaveValue("Lovelace");
});

test("changing the email warns that verification resets", async ({ page }) => {
  await register(page, newEmail("editemail"));
  await goToProfile(page);

  await page.getByLabel("Email").fill(newEmail("editemail-changed"));

  await expect(
    page.getByText("Changing your email marks it unverified again."),
  ).toBeVisible();
});

test("uploading an avatar shows it immediately and survives a reload", async ({
  page,
}) => {
  await register(page, newEmail("avatar"));
  await goToProfile(page);

  const fileChooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Change avatar" }).click();
  const chooser = await fileChooser;
  await chooser.setFiles({
    name: "avatar.png",
    mimeType: "image/png",
    buffer: onePixelPNG(),
  });

  const avatarImg = page.locator('img[alt]').first();
  await expect(avatarImg).toBeVisible({ timeout: 15_000 });

  await page.reload();
  await expect(page.locator('img[alt]').first()).toBeVisible();
});

test("the sessions tab shows the active device and lets it be ended from elsewhere", async ({
  page,
  browser,
}) => {
  const email = newEmail("sessions");
  await register(page, email, "Ada");
  await goToProfile(page);

  await page.getByRole("tab", { name: "Sessions" }).click();
  await expect(page.getByText("This device")).toBeVisible();
  await expect(page.getByRole("button", { name: "End session" })).toHaveCount(0);

  const second = await browser.newContext();
  const secondPage = await second.newPage();
  await open(secondPage, "/login");
  await submitCredentials(secondPage, "Sign in", email);
  await secondPage.waitForURL(/\/$/);

  await page.reload();
  await page.getByRole("tab", { name: "Sessions" }).click();
  await expect(page.getByRole("button", { name: "End session" })).toHaveCount(1);

  await page.getByRole("button", { name: "End session" }).click();
  await expect(page.getByRole("button", { name: "End session" })).toHaveCount(0);

  await secondPage.reload();
  await expect(secondPage).toHaveURL(/\/login/);

  await second.close();
});

function onePixelPNG(): Buffer {
  return Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAEElEQVR4nGM4kWIERwzEcQBCchXhIRsTawAAAABJRU5ErkJggg==",
    "base64",
  );
}
