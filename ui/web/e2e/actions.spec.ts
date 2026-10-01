import { expect, test } from "@playwright/test";

import {
  chatList,
  chooseChatAction,
  chooseMessageAction,
  expectReady,
  expectSaid,
  messageWith,
  openChatWith,
  say,
  startChat,
} from "./chat-support";
import { newPerson } from "./support";

test("a reply quotes what it answers, and an edit and a delete reach the other person", async ({ browser }) => {
  const ada = await newPerson(browser, "actions", "Ada");
  const grace = await newPerson(browser, "actions-peer", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "the first words");
  await openChatWith(grace.page, "Ada");

  await chooseMessageAction(grace.page, "the first words", "Reply");
  await expect(grace.page.getByText("Reply to Ada")).toBeVisible();
  await say(grace.page, "agreed");
  await expect(grace.page.getByText("Reply to Ada")).toHaveCount(0);

  const reply = messageWith(ada.page, "agreed");
  await expect(reply.getByRole("button", { name: "Replying to You" })).toContainText("the first words");

  await chooseMessageAction(ada.page, "the first words", "Edit");
  const input = ada.page.getByLabel("Message", { exact: true });
  await expect(input).toHaveValue("the first words");
  await input.fill("the first words, fixed");
  await ada.page.getByRole("button", { name: "Save", exact: true }).click();

  const edited = messageWith(grace.page, "the first words, fixed").first();
  await expect(edited).toBeVisible();
  await expect(edited.getByText("edited", { exact: true })).toBeVisible();
  const quote = messageWith(grace.page, "agreed").getByRole("button", { name: "Replying to Ada" });
  await expect(quote).toContainText("the first words, fixed");

  await chooseMessageAction(ada.page, "the first words, fixed", "Delete");
  await ada.page.getByRole("alertdialog").getByRole("button", { name: "Delete" }).click();

  await expect(messageWith(grace.page, "Message deleted")).toBeVisible();
  await expect(quote).toContainText("Deleted message");
  await expect(grace.page.getByText("the first words, fixed")).toHaveCount(0);

  await grace.page.reload();
  await expectReady(grace.page);
  await expect(messageWith(grace.page, "Message deleted")).toBeVisible();
  await expect(messageWith(grace.page, "agreed").getByRole("button", { name: "Replying to Ada" })).toContainText(
    "Deleted message",
  );

  await ada.close();
  await grace.close();
});

test("a forward lands in another chat credited to whoever said it first", async ({ browser }) => {
  const ada = await newPerson(browser, "forward", "Ada");
  const grace = await newPerson(browser, "forward-peer", "Grace");
  const linus = await newPerson(browser, "forward-third", "Linus");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "worth sharing");
  await startChat(grace.page, { email: linus.email, name: "Linus" }, "hi linus");

  await openChatWith(grace.page, "Ada");
  await chooseMessageAction(grace.page, "worth sharing", "Forward");
  await grace.page.getByRole("dialog").getByRole("button", { name: "Linus" }).click();

  await expect(grace.page.getByRole("heading", { name: "Linus" })).toBeVisible();
  const forwarded = messageWith(grace.page, "Forwarded from Ada");
  await expect(forwarded).toContainText("worth sharing");

  await openChatWith(linus.page, "Grace");
  await expect(messageWith(linus.page, "Forwarded from Ada")).toContainText("worth sharing");

  await ada.close();
  await grace.close();
  await linus.close();
});

test("a deleted chat is cleared for you until a new message brings it back", async ({ browser }) => {
  const ada = await newPerson(browser, "hide", "Ada");
  const grace = await newPerson(browser, "hide-peer", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "before the clean slate");
  await openChatWith(grace.page, "Ada");
  await expectSaid(grace.page, "Ada", "before the clean slate");

  await chooseChatAction(grace.page, "Delete chat");
  await grace.page.getByRole("alertdialog").getByRole("button", { name: "Delete chat" }).click();

  await expect(grace.page).toHaveURL(/\/chat$/);
  await expect(chatList(grace.page).getByRole("link", { name: /Ada/ })).toHaveCount(0);

  await say(ada.page, "after the clean slate");

  await expect(chatList(grace.page).getByRole("link", { name: /Ada/ })).toBeVisible();
  await openChatWith(grace.page, "Ada");
  await expectSaid(grace.page, "Ada", "after the clean slate");
  await expect(grace.page.getByText("before the clean slate")).toHaveCount(0);

  await ada.close();
  await grace.close();
});
