import { expect, test, type APIResponse, type Route } from "@playwright/test";

import { chatList, goToChats, openChatWith, expectSaid, startChat } from "./chat-support";
import { expectHome, newPerson, submitCredentials } from "./support";

for (const method of ["password", "oauth"] as const) {
  test(`a ${method} sign-in replaces an older restoration before sending messages`, async ({ browser }, testInfo) => {
    const previous = await newPerson(browser, "old-session", "Old Account");
    const next = await newPerson(browser, "new-session", "New Account");
    const peer = await newPerson(browser, "identity-peer", "Peer");
    await startChat(previous.page, { ...peer, name: "Peer" }, "private message of the previous account");

    let captured!: (value: { route: Route; response: APIResponse }) => void;
    const held = new Promise<{ route: Route; response: APIResponse }>((resolve) => { captured = resolve; });
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    let finished!: () => void;
    const delivered = new Promise<void>((resolve) => { finished = resolve; });
    await previous.page.route("**/axon.auth.v1.AuthService/RefreshToken", async (route) => {
      const response = await route.fetch();
      captured({ route, response });
      await gate;
      await route.fulfill({ response }).catch(() => undefined);
      finished();
    }, { times: 1 });

    await previous.page.goto("/login");
    await held;
    let username = next.username;
    let displayName = "New Account";
    if (method === "password") {
      await submitCredentials(previous.page, "Sign in", next.email);
    } else {
      const complete = previous.page.waitForResponse((response) => response.url().endsWith("/axon.auth.v1.AuthService/CompleteOAuth"));
      await previous.page.getByRole("button", { name: "Continue with the test provider" }).click();
      const result = await (await complete).json();
      username = result.user.username;
      displayName = result.user.displayName || username;
    }
    await expectHome(previous.page);
    release();
    await delivered;
    await expect(previous.page.getByRole("link", { name: "My account", exact: true })).toContainText(username);
    await goToChats(previous.page);
    await expect(chatList(previous.page)).not.toContainText("private message of the previous account");

    await startChat(previous.page, { ...peer, name: "Peer" }, "sent by the new account");
    await expectSaid(previous.page, "You", "sent by the new account");
    await previous.page.screenshot({ path: testInfo.outputPath("own-message.png") });
    await openChatWith(peer.page, displayName);
    await expectSaid(peer.page, displayName, "sent by the new account");
    await previous.page.reload();
    await expectSaid(previous.page, "You", "sent by the new account");
    await expect(previous.page.getByRole("link", { name: "My account", exact: true })).toContainText(username);

    await previous.close();
    await next.close();
    await peer.close();
  });
}
