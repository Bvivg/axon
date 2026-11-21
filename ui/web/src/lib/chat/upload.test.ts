import axios, { AxiosError, AxiosHeaders, type AxiosRequestConfig, type AxiosResponse } from "axios";
import { afterEach, describe, expect, it, vi } from "vitest";

import { UploadAs, uploadAttachment } from "./upload";

vi.mock("@/lib/chat/hash-file", () => ({ hashFile: async () => "ab".repeat(32) }));
vi.mock("@/lib/auth/tokens", () => ({ getAccessToken: () => "token" }));
vi.mock("@/lib/auth/refresh", () => ({ refreshSession: async () => false }));
vi.mock("@/lib/connect/transport", () => ({ apiBaseUrl: "http://gateway" }));

afterEach(() => { vi.restoreAllMocks(); });

function reply<T>(config: AxiosRequestConfig, status: number, data: T): AxiosResponse<T> {
  return { config: { ...config, headers: new AxiosHeaders() }, status, statusText: String(status), headers: {}, data };
}

function refuse(config: AxiosRequestConfig, status: number, data: unknown): never {
  const response = reply(config, status, data);
  throw new AxiosError(`Request failed with status code ${status}`, "ERR_BAD_REQUEST", response.config, null, response);
}

describe("upload refusals", () => {
  it("tells a spent daily budget apart from a file that is too large", async () => {
    vi.spyOn(axios, "request").mockImplementation(async (config) =>
      refuse(config, 413, { code: "resource_exhausted", message: "the daily upload limit is reached" }),
    );
    await expect(uploadAttachment(new Blob(["x"]), "a.png", UploadAs.Media)).rejects.toThrow("today's upload limit");

    vi.spyOn(axios, "request").mockImplementation(async (config) => refuse(config, 413, "the file is too large\n"));
    await expect(uploadAttachment(new Blob(["x"]), "a.png", UploadAs.Media)).rejects.toThrow("That file is too large.");
  });

  it("explains an animation that is too heavy to send as a photo", async () => {
    const session = { id: "s1", status: "uploading", part_size: 10, part_count: 1, parts: [] };
    vi.spyOn(axios, "request").mockImplementation(async (config) => {
      if (config.method === "POST" && config.url?.endsWith("/api/uploads")) return reply(config, 201, session);
      if (config.method === "PUT") return reply(config, 204, null);
      return reply(config, 200, { ...session, status: "failed", failure: "animation_too_large" });
    });
    await expect(uploadAttachment(new Blob(["x"]), "a.gif", UploadAs.Media)).rejects.toThrow("Send it as a file instead.");
  });
});
