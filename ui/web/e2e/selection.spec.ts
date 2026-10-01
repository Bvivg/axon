import { expect, test } from "@playwright/test";

import { chooseMessageAction, expectSaid, messageWith, openChatWith, say, startChat } from "./chat-support";
import { newPerson } from "./support";

test("several messages go to two chats with a comment, and own ones are deleted together", async ({ browser }) => {
  const ada = await newPerson(browser, "selection", "Ada");
  const grace = await newPerson(browser, "selection-grace", "Grace");
  const hal = await newPerson(browser, "selection-hal", "Hal");

  await startChat(ada.page, { email: hal.email, username: hal.username, name: "Hal" }, "hi hal");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "point one");
  await say(ada.page, "point two");
  await say(ada.page, "point three");

  await chooseMessageAction(ada.page, "point one", "Select");
  const bar = ada.page.getByRole("toolbar", { name: "Selected messages" });
  await expect(bar).toContainText("1 selected");
  await ada.page.getByRole("checkbox", { name: "Select message from you" }).nth(1).click();
  await ada.page.getByRole("checkbox", { name: "Select message from you" }).nth(2).click();
  await expect(bar).toContainText("3 selected");

  await bar.getByRole("button", { name: "Forward" }).click();
  const dialog = ada.page.getByRole("dialog");
  await dialog.getByRole("checkbox", { name: /Grace/ }).click();
  await dialog.getByRole("checkbox", { name: /Hal/ }).click();
  await dialog.getByLabel("Comment").fill("for the record");
  await dialog.getByRole("button", { name: "Forward to 2 chats" }).click();
  await expect(ada.page.getByText("Forwarded to 2 chats")).toBeVisible();
  await expect(bar).toHaveCount(0);

  await openChatWith(hal.page, "Ada");
  await expectSaid(hal.page, "Ada", "for the record");
  const forwarded = hal.page.getByRole("article", { name: "Message from Ada" }).filter({ hasText: "Forwarded from" });
  await expect(forwarded).toHaveCount(3);
  await expect(forwarded.nth(0)).toContainText("point one");
  await expect(forwarded.nth(2)).toContainText("point three");

  await openChatWith(grace.page, "Ada");
  await expect(grace.page.getByRole("article", { name: "Message from Ada" }).filter({ hasText: "Forwarded from" })).toHaveCount(3);

  await chooseMessageAction(ada.page, "point three", "Read by");
  await expect(ada.page.getByRole("dialog")).toContainText(/Read (Today|Yesterday)/);
  await ada.page.keyboard.press("Escape");

  await chooseMessageAction(ada.page, "point one", "Select");
  await ada.page.getByRole("checkbox", { name: "Select message from you" }).nth(1).click();
  await bar.getByRole("button", { name: "Delete" }).click();
  await ada.page.getByRole("alertdialog").getByRole("button", { name: "Delete" }).click();
  await expect(messageWith(grace.page, "Message deleted")).toHaveCount(2);
  await expect(messageWith(grace.page, "point three")).toHaveCount(2);
});
