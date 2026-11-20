import { expect, test } from "@playwright/test";

import { newPerson } from "./support";

test("the sidebar search survives an autofill keydown and finds a person by a fragment of their name", async ({
  browser,
}) => {
  const tag = Math.random().toString(36).slice(2, 8);
  const name = `Linus Palette${tag}`;
  const ada = await newPerson(browser, "palette", "Ada");
  const linus = await newPerson(browser, "palette-peer", name);

  const errors: Error[] = [];
  ada.page.on("pageerror", (error) => errors.push(error));

  await ada.page.evaluate(() => window.dispatchEvent(new Event("keydown")));
  await ada.page.keyboard.press("ControlOrMeta+k");
  const commands = ada.page.getByRole("dialog", { name: "Command palette" });
  const commandSearch = commands.getByRole("combobox", { name: "Search commands" });
  await expect(commandSearch).toBeFocused();
  await commandSearch.press("ArrowDown");
  await commandSearch.press("ArrowDown");
  await expect(commands.getByRole("option", { name: "Chats", exact: true })).toHaveAttribute("aria-selected", "true");
  await commandSearch.fill("search chats");
  await commandSearch.press("Enter");

  const search = ada.page.getByLabel("Search chats and people", { exact: true });
  await expect(search).toBeFocused();
  await search.fill(`lette${tag.slice(0, 3)}`);
  await expect(ada.page.getByText(name, { exact: true })).toBeVisible();
  await expect(ada.page.getByRole("dialog")).toHaveCount(0);
  await search.press("Escape");
  await expect(search).toHaveValue("");
  await ada.page.keyboard.press("ControlOrMeta+Shift+p");
  await commands.getByLabel("Search commands").fill("New chat");
  await commands.getByRole("option", { name: "New chat" }).click();
  await expect(ada.page.getByRole("heading", { name: "New message", exact: true })).toBeVisible();

  expect(errors).toEqual([]);

  await ada.close();
  await linus.close();
});
