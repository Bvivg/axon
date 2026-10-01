import { expect, test } from "@playwright/test";

import { chatList, expectReady, expectSaid, goToChats, openChatWith, say, startChat } from "./chat-support";
import { goToProfile, newPerson, open, signOut, submitCredentials } from "./support";

test("a chat starts from a contact and both people see each other's messages as they are sent", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "ada", "Ada");
  const grace = await newPerson(browser, "grace", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "did that reach you");

  await openChatWith(grace.page, "Ada");
  await expectSaid(grace.page, "Ada", "did that reach you");

  await say(grace.page, "loud and clear");
  await expectSaid(grace.page, "You", "loud and clear");
  await expectSaid(ada.page, "Grace", "loud and clear");

  await say(ada.page, "good to hear");
  await expectSaid(grace.page, "Ada", "good to hear");

  await ada.close();
  await grace.close();
});

test("a page opened cold shows what was said before it existed", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "history", "Ada");
  const grace = await newPerson(browser, "history-peer", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "said before the reload");

  await ada.page.reload();
  await expectReady(ada.page);
  await expectSaid(ada.page, "You", "said before the reload");

  await say(ada.page, "said after it");
  await expectSaid(ada.page, "You", "said after it");
  await expect(ada.page.getByRole("article")).toHaveCount(2);
  await expect(ada.page.getByText("Today", { exact: true })).toHaveCount(1);

  await goToChats(ada.page);
  await expect(chatList(ada.page).getByRole("link", { name: /Grace/ })).toBeVisible();

  await ada.close();
  await grace.close();
});

test("a chat is out of reach without a session, and reachable again after signing in", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "guarded", "Ada");
  const grace = await newPerson(browser, "guarded-peer", "Grace");

  const roomID = await startChat(ada.page, { email: grace.email, name: "Grace" }, "members only");

  await goToProfile(ada.page);
  await signOut(ada.page);

  await open(ada.page, `/chat/${roomID}`);
  await expect(ada.page).toHaveURL(new RegExp(`/login\\?next=%2Fchat%2F${roomID}$`));

  await submitCredentials(ada.page, "Sign in", ada.email);

  await expect(ada.page).toHaveURL(new RegExp(`/chat/${roomID}$`));
  await expectReady(ada.page);
  await expectSaid(ada.page, "You", "members only");

  await ada.close();
  await grace.close();
});
