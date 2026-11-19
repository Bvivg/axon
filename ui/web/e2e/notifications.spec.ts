import { expect, test } from "@playwright/test";

import { chatAt, expectReady, openChatWith, say, startChat } from "./chat-support";
import { newPerson } from "./support";

test("a message in another chat shows a toast that opens it, and the tab counts what is unread", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "toast", "Ada");
  const grace = await newPerson(browser, "toast-peer", "Grace");
  const cid = await newPerson(browser, "toast-other", "Cid");

  await startChat(grace.page, { email: ada.email, username: ada.username, name: "Ada" }, "hello ada");
  await startChat(cid.page, { email: ada.email, username: ada.username, name: "Ada" }, "from cid");

  await openChatWith(ada.page, "Grace");
  await expect(ada.page).toHaveTitle("(1) Axon");

  await say(grace.page, "in this chat");
  await say(cid.page, "are you there?");
  await ada.page.bringToFront();

  const toast = ada.page.locator("[data-sonner-toast]").filter({ hasText: "Cid" });
  await expect(toast).toContainText("are you there?");
  await expect(ada.page.locator("[data-sonner-toast]").filter({ hasText: "in this chat" })).toHaveCount(0);
  await expect(ada.page).toHaveTitle("(2) Axon");

  await toast.getByRole("button", { name: "Open" }).click();
  await expect(ada.page).toHaveURL(chatAt(cid.username));
  await expectReady(ada.page);
  await expect(ada.page).toHaveTitle("Axon");
});

test("the profile offers to turn notifications on when the server has a key", async ({ browser }) => {
  const ada = await newPerson(browser, "push", "Ada");
  await ada.page.context().grantPermissions(["notifications"]);
  await ada.page.goto("/profile/notifications");
  await expect(ada.page.getByRole("heading", { name: "Notifications", exact: true })).toBeVisible();
  await expect(ada.page.getByText("New messages on this device")).toBeVisible();
  await expect(ada.page.getByText("When Axon isn't open")).toBeVisible();
  await expect(ada.page.getByRole("button", { name: "Turn on" })).toBeEnabled();
});
