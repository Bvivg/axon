import { expect, test, type Page } from "@playwright/test";

import {
  chatList,
  chooseChatAction,
  expectReady,
  expectSaid,
  goToChats,
  say,
  systemLine,
} from "./chat-support";
import { newPerson } from "./support";

async function pickPerson(page: Page, username: string, name: string): Promise<void> {
  const composer = chatList(page).getByRole("region", { name: "New conversation" });
  await composer.getByLabel("Search people").fill(`@${username}`);
  await composer.getByRole("button", { name, exact: false }).filter({ hasNotText: "Remove" }).click();
  await expect(composer.getByRole("list", { name: "Selected people" })).toContainText(name);
}

async function openGroup(page: Page, name: string): Promise<void> {
  await goToChats(page);
  await chatList(page).getByRole("link", { name: new RegExp(name) }).click();
  await expect(page.getByRole("heading", { name })).toBeVisible();
  await expectReady(page);
}

test("a group is built from contacts and run by its owner", async ({ browser }, testInfo) => {
  const ada = await newPerson(browser, "group", "Ada");
  const grace = await newPerson(browser, "group-member", "Grace");
  const linus = await newPerson(browser, "group-other", "Linus");

  await goToChats(ada.page);
  await ada.page.getByRole("button", { name: "New chat" }).click();
  const composer = chatList(ada.page).getByRole("region", { name: "New conversation" });
  await composer.getByRole("button", { name: "Create group" }).click();
  await expect(composer.getByRole("button", { name: "Next" })).toBeDisabled();
  await pickPerson(ada.page, grace.username, "Grace");
  await pickPerson(ada.page, linus.username, "Linus");
  await composer.getByRole("button", { name: "Next · 2" }).click();
  await expect(composer.getByRole("button", { name: "Create group" })).toBeDisabled();
  await composer.getByLabel("Group name").fill("Weekend");
  await composer.getByRole("button", { name: "Back", exact: true }).click();
  await expect(composer.getByRole("list", { name: "Selected people" })).toContainText("Grace");
  await expect(composer.getByRole("list", { name: "Selected people" })).toContainText("Linus");
  await composer.getByRole("button", { name: "Next · 2" }).click();
  await expect(composer.getByLabel("Group name")).toHaveValue("Weekend");
  await ada.page.screenshot({ path: testInfo.outputPath("group-details.png") });
  await composer.getByRole("button", { name: "Create group" }).click();

  await expect(ada.page).toHaveURL(/\/chat#[0-9a-f-]{36}$/);
  await expect(ada.page.getByRole("heading", { name: "Weekend" })).toBeVisible();
  await expect(systemLine(ada.page, "You created the group “Weekend”")).toBeVisible();

  await openGroup(grace.page, "Weekend");
  await expect(systemLine(grace.page, "Ada created the group “Weekend”")).toBeVisible();
  await openGroup(linus.page, "Weekend");

  await say(ada.page, "hello group");
  await expectSaid(grace.page, "Ada", "hello group");
  await expectSaid(linus.page, "Ada", "hello group");
  await expect(grace.page.locator("[data-message-avatar]")).toHaveCount(1);
  await expect(linus.page.locator("[data-message-avatar]")).toHaveCount(1);

  await chooseChatAction(grace.page, "Group info");
  const memberView = grace.page.getByRole("dialog");
  await expect(memberView.getByRole("list", { name: "Members" }).getByRole("listitem")).toHaveCount(3);
  await expect(memberView.getByRole("button", { name: "Add people" })).toHaveCount(0);
  await expect(memberView.getByRole("button", { name: /^Remove / })).toHaveCount(0);
  await grace.page.keyboard.press("Escape");

  await chooseChatAction(ada.page, "Group info");
  const ownerView = ada.page.getByRole("dialog");
  await ownerView.getByRole("button", { name: "Remove Linus" }).click();
  await ada.page.getByRole("alertdialog").getByRole("button", { name: "Remove" }).click();
  await expect(ownerView.getByRole("list", { name: "Members" }).getByRole("listitem")).toHaveCount(2);

  await expect(linus.page).toHaveURL(/\/chat$/);
  await expect(chatList(linus.page).getByRole("link", { name: /Weekend/ })).toHaveCount(0);
  await expect(systemLine(grace.page, "Ada removed Linus")).toBeVisible();

  await ownerView.getByLabel("Group name").fill("Trip");
  await ownerView.getByRole("button", { name: "Save" }).click();
  await expect(grace.page.getByRole("heading", { name: "Trip" })).toBeVisible();
  await expect(systemLine(grace.page, "Ada renamed the group to “Trip”")).toBeVisible();
  await ada.page.keyboard.press("Escape");

  await chooseChatAction(grace.page, "Leave group");
  await grace.page.getByRole("alertdialog").getByRole("button", { name: "Leave" }).click();
  await expect(grace.page).toHaveURL(/\/chat$/);
  await expect(chatList(grace.page).getByRole("link", { name: /Trip/ })).toHaveCount(0);
  await expect(systemLine(ada.page, "Grace left the group")).toBeVisible();

  await ada.close();
  await grace.close();
  await linus.close();
});
