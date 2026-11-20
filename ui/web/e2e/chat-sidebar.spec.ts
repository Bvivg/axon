import { expect, test } from "@playwright/test";

import { chatAt, chatList, expectSaid, goToChats, startChat } from "./chat-support";
import { newPerson } from "./support";

test("chat search retries the failed initial requests", async ({ browser }, testInfo) => {
  const ada = await newPerson(browser, "search-retry", "Ada");
  try {
    const rooms = "**/axon.chat.v1.ChatService/ListRooms";
    const people = "**/axon.auth.v1.AuthService/SearchUsers";
    for (const path of [rooms, people]) {
      await ada.page.route(path, (route) => route.fulfill({
        status: 503,
        json: { code: "unavailable", message: "Service unavailable" },
      }));
    }

    await ada.page.reload();
    await goToChats(ada.page);
    const list = chatList(ada.page);
    await list.getByLabel("Search chats and people").fill("absent_search_retry_49316");
    const directory = list.getByLabel("Chat search results", { exact: true });
    await expect(directory.getByText("Could not load chats.")).toBeVisible();
    await expect(directory.getByText("Could not load people.")).toBeVisible();
    await ada.page.screenshot({ path: testInfo.outputPath("search-retry.png"), animations: "disabled" });

    await ada.page.unroute(rooms);
    await ada.page.unroute(people);
    for (const [button, path] of [["Retry chats", rooms], ["Retry search", people]]) {
      const result = ada.page.waitForResponse((response) => response.url().endsWith(path.slice(2)) && response.ok());
      await directory.getByRole("button", { name: button }).click();
      await result;
    }
    await expect(directory.getByText("No chats or people found.", { exact: true })).toBeVisible();
  } finally {
    await ada.close();
  }
});

test("inline search keeps the conversation open, deduplicates contacts and opens new people", async ({ browser }, testInfo) => {
  const tag = Math.random().toString(36).slice(2, 8);
  const ada = await newPerson(browser, "sidebar", "Ada");
  const grace = await newPerson(browser, "sidebar-peer", `Grace ${tag}`);
  const linus = await newPerson(browser, "sidebar-new", `Linus ${tag}`);
  const list = chatList(ada.page);

  await startChat(ada.page, { ...grace, name: `Grace ${tag}` }, "keep this conversation visible");
  const search = list.getByLabel("Search chats and people");
  await search.fill(tag);
  await expect(list.getByRole("region", { name: "Matching chats" }).getByRole("link", { name: new RegExp(`Grace ${tag}`) })).toBeVisible();
  const linusChat = list.getByRole("button", { name: new RegExp(`Linus ${tag} @${linus.username}$`) });
  await expect(linusChat).toBeVisible();
  await expect(list.getByRole("button", { name: new RegExp(`Grace ${tag}`) })).toHaveCount(0);
  await ada.page.screenshot({ path: testInfo.outputPath("inline-search.png") });
  await expectSaid(ada.page, "You", "keep this conversation visible");
  await expect(ada.page).toHaveURL(chatAt(grace.username));
  await list.getByRole("button", { name: "Clear search" }).click();
  await expect(search).toHaveValue("");
  await expect(list.getByRole("region", { name: "Matching people" })).toBeVisible();

  await search.fill("no_such_person_987654321");
  await expect(list.getByRole("status")).toHaveText("No chats or people found.");
  await search.fill(`@${linus.username}`);
  await linusChat.click();
  await expect(ada.page).toHaveURL(chatAt(linus.username));
  await expect(search).toHaveValue("");
  await expect(ada.page.getByRole("dialog")).toHaveCount(0);

  await ada.close();
  await grace.close();
  await linus.close();
});

test("mobile search and group creation use the chat column and preserve the selected conversation", async ({ browser }) => {
  const ada = await newPerson(browser, "sidebar-mobile", "Ada");
  const grace = await newPerson(browser, "sidebar-mobile-peer", "Grace Mobile");
  const list = chatList(ada.page);
  await startChat(ada.page, { ...grace, name: "Grace Mobile" }, "mobile conversation");
  await ada.page.setViewportSize({ width: 390, height: 844 });
  await expect(list).toBeHidden();
  await ada.page.keyboard.press("ControlOrMeta+k");
  const commands = ada.page.getByRole("combobox", { name: "Search commands" });
  await commands.fill("search chats");
  await commands.press("Enter");
  await expect(list).toBeVisible();
  await expect(list.getByLabel("Search chats and people")).toBeFocused();
  await list.getByRole("button", { name: "Back to chats" }).click();
  await expect(list).toBeHidden();
  await expectSaid(ada.page, "You", "mobile conversation");

  await ada.page.getByRole("link", { name: "Back to chats", exact: true }).click();
  await list.getByRole("button", { name: "New chat" }).click();
  await list.getByRole("button", { name: "Create group" }).click();
  await list.getByLabel("Search people").fill(grace.email);
  await list.getByRole("button", { name: /Grace Mobile/ }).click();
  await list.getByRole("button", { name: "Next · 1" }).click();
  await list.getByLabel("Group name").fill("Mobile group");
  await list.getByRole("button", { name: "Remove Grace Mobile" }).click();
  await expect(list.getByRole("button", { name: "Create group" })).toBeDisabled();
  await list.getByRole("button", { name: "Back", exact: true }).click();
  await expect(list.getByRole("button", { name: "Next", exact: true })).toBeDisabled();
  await list.getByLabel("Search people").fill(grace.email);
  await list.getByRole("button", { name: /Grace Mobile/ }).click();
  await list.getByRole("button", { name: "Next · 1" }).click();
  await expect(list.getByLabel("Group name")).toHaveValue("Mobile group");
  await list.getByRole("button", { name: "Create group" }).click();
  await expect(ada.page.getByRole("heading", { name: "Mobile group", exact: true })).toBeVisible();
  await expect(list).toBeHidden();
  await expect(ada.page.getByRole("dialog")).toHaveCount(0);
  expect(await ada.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

  await ada.close();
  await grace.close();
});
