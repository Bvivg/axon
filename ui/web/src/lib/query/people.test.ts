import { expect, it, vi } from "vitest";

vi.mock("@/lib/connect/clients", () => ({ apiClient: {} }));

import { fetchProfiles, maxProfilesPerRequest } from "./people";

const ids = (n: number) => Array.from({ length: n }, (_, i) => `user-${i}`);

it("splits a long id list into requests the gateway accepts", async () => {
  const lookup = vi.fn(async ({ userIds }: { userIds: string[] }) => ({ users: userIds.map((id) => ({ id, displayName: id })) }));

  const profiles = await fetchProfiles(lookup, ids(250));

  expect(lookup).toHaveBeenCalledTimes(3);
  for (const [request] of lookup.mock.calls) expect(request.userIds.length).toBeLessThanOrEqual(maxProfilesPerRequest);
  expect(profiles.size).toBe(250);
  expect(profiles.get("user-249")?.displayName).toBe("user-249");
});

it("sends a short list in one request", async () => {
  const lookup = vi.fn(async ({ userIds }: { userIds: string[] }) => ({ users: userIds.map((id) => ({ id })) }));

  await fetchProfiles(lookup, ids(maxProfilesPerRequest));

  expect(lookup).toHaveBeenCalledTimes(1);
});
