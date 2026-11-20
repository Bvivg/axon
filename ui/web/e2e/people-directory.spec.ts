import { expect, test, type Page, type Response } from "@playwright/test";

import { chatList, goToChats } from "./chat-support";
import { gatewayUrl, newPerson, newUsername, password } from "./support";

function searchResponse(page: Page, query: string, more: boolean): Promise<Response> {
  return page.waitForResponse((response) => {
    if (!response.ok() || !response.url().endsWith("/axon.auth.v1.AuthService/SearchUsers")) return false;
    const body = response.request().postDataJSON();
    return (body.query ?? "") === query && !!body.pageToken === more;
  });
}

async function scrollForMore(page: Page, label: string, query: string) {
  const next = searchResponse(page, query, true);
  await page.getByLabel(`${label} scroll`, { exact: true }).evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  return (await next).json();
}

test("people load immediately and paginate with or without a query in search and group creation", async ({ browser, request }, testInfo) => {
  test.setTimeout(120_000);
  const tag = Math.random().toString(36).slice(2, 9);
  const prefix = `Paging${tag}`;
  for (let index = 0; index < 45; index += 1) {
    const response = await request.post(`${gatewayUrl}/axon.auth.v1.AuthService/Register`, {
      data: {
        email: `paging-${tag}-${index}@axon.test`,
        username: newUsername("paging"),
        displayName: `${prefix} ${String(index).padStart(2, "0")}`,
        password,
      },
    });
    expect(response.ok()).toBe(true);
  }

  const ada = await newPerson(browser, "directory", "Ada");
  const list = chatList(ada.page);
  await goToChats(ada.page);
  const initial = searchResponse(ada.page, "", false);
  const search = list.getByLabel("Search chats and people", { exact: true });
  await search.click();
  const first = await (await initial).json();
  expect(first.users).toHaveLength(20);
  expect(first.nextPageToken).toBeTruthy();
  await expect(list.getByRole("region", { name: "Matching people" })).toBeVisible();
  const second = await scrollForMore(ada.page, "Chat search results", "");
  expect(second.users).toHaveLength(20);
  expect(second.users.some((person: { id: string }) => first.users.some((other: { id: string }) => other.id === person.id))).toBe(false);

  const filtered = searchResponse(ada.page, tag.toLowerCase(), false);
  await search.fill(tag);
  expect((await (await filtered).json()).users).toHaveLength(20);
  await scrollForMore(ada.page, "Chat search results", tag.toLowerCase());
  const last = await scrollForMore(ada.page, "Chat search results", tag.toLowerCase());
  expect(last.users).toHaveLength(5);
  expect(last.nextPageToken ?? "").toBe("");
  await list.getByLabel("Chat search results scroll").evaluate((element) => { element.scrollTop = element.scrollHeight; });
  await expect(list.getByText(`${prefix} 44`, { exact: true })).toBeVisible();

  await list.getByRole("button", { name: "New chat" }).click();
  await list.getByRole("button", { name: "Create group" }).click();
  await expect(list.getByRole("region", { name: "Available people" })).toBeVisible();
  await scrollForMore(ada.page, "People directory", "");
  const groupSearch = list.getByLabel("Search people", { exact: true });
  const groupFirst = searchResponse(ada.page, prefix, false);
  await groupSearch.fill(prefix);
  await groupFirst;
  await list.getByRole("button", { name: new RegExp(`${prefix} 00`) }).click();
  await scrollForMore(ada.page, "People directory", prefix);
  await scrollForMore(ada.page, "People directory", prefix);
  await list.getByLabel("People directory scroll").evaluate((element) => { element.scrollTop = element.scrollHeight; });
  await list.getByRole("button", { name: new RegExp(`${prefix} 44`) }).click();
  await groupSearch.fill("");
  await expect(list.getByRole("list", { name: "Selected people" })).toContainText(`${prefix} 00`);
  await expect(list.getByRole("list", { name: "Selected people" })).toContainText(`${prefix} 44`);
  await list.getByRole("button", { name: "Next · 2" }).click();
  await list.getByLabel("Group name").fill("Paged group");
  await ada.page.screenshot({ path: testInfo.outputPath("paged-group.png") });
  await list.getByRole("button", { name: "Create group" }).click();
  await expect(ada.page.getByRole("heading", { name: "Paged group", exact: true })).toBeVisible();
  await ada.close();
});
