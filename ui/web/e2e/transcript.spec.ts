import { expect, test, type Locator } from "@playwright/test";

import { chooseMessageAction, expectReady, goToChats, messageWith, openChatWith, say, startChat } from "./chat-support";
import { newPerson } from "./support";

const onScreen = (locator: Locator) =>
  locator.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return box.bottom > 0 && box.top < window.innerHeight;
  });

const tall = (n: number) => [`note ${String.fromCharCode(64 + n)}`, "second line", "third line", "fourth line", "fifth line", "sixth line"].join("\n");

test("a chat opens at the first unread message, and the latest is one click away", async ({ browser }) => {
  const ada = await newPerson(browser, "transcript", "Ada");
  const grace = await newPerson(browser, "transcript-peer", "Grace");

  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "hello there");
  await goToChats(ada.page);

  await openChatWith(grace.page, "Ada");
  for (let n = 1; n <= 12; n += 1) {
    await say(grace.page, tall(n));
  }

  await openChatWith(ada.page, "Grace");
  const log = ada.page.getByRole("log", { name: "Messages" });
  const divider = log.getByRole("separator").filter({ hasText: "Unread messages" });
  await expect(divider).toBeInViewport();
  await expect(messageWith(ada.page, "note A")).toBeInViewport();
  await expect(messageWith(ada.page, "note L")).not.toBeInViewport();

  await chooseMessageAction(ada.page, "note A", "Reply");

  const latest = ada.page.getByRole("button", { name: /^Scroll to latest, \d+ new$/ });
  await expect(latest).toBeVisible();
  await latest.click();
  await expect(messageWith(ada.page, "note L")).toBeInViewport();
  await expect(ada.page.getByRole("button", { name: /^Scroll to latest/ })).toHaveCount(0);

  await say(ada.page, "back to the start");
  await expect(messageWith(ada.page, "back to the start")).toBeInViewport();
  const original = ada.page.getByRole("article", { name: "Message from Grace" }).filter({ hasText: "note A" });
  await expect.poll(async () => (await original.count()) === 0 || !(await onScreen(original))).toBe(true);

  await messageWith(ada.page, "back to the start").getByRole("button", { name: "Replying to Grace" }).click();
  await expect(original).toBeInViewport();
});

test("Shift+Enter breaks the line, Enter sends, and links open safely", async ({ browser }) => {
  const ada = await newPerson(browser, "composer", "Ada");
  const grace = await newPerson(browser, "composer-peer", "Grace");

  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "hi");
  await openChatWith(grace.page, "Ada");
  await expectReady(ada.page);

  const input = ada.page.getByLabel("Message", { exact: true });
  await input.click();
  await input.pressSequentially("first line");
  await input.press("Shift+Enter");
  await input.pressSequentially("second line");
  await expect(input).toHaveValue("first line\nsecond line");
  await input.press("Enter");
  await expect(input).toHaveValue("");

  const received = messageWith(grace.page, "first line");
  await expect(received).toContainText("first line\nsecond line", { useInnerText: true });

  await input.pressSequentially("docs at axon.dev/docs.");
  await input.press("Enter");
  const link = messageWith(grace.page, "docs at").getByRole("link", { name: "axon.dev/docs" });
  await expect(link).toHaveAttribute("href", "https://axon.dev/docs");
  await expect(link).toHaveAttribute("rel", "noopener noreferrer");
  await expect(link).toHaveAttribute("target", "_blank");
});
