import { afterEach, expect, it, vi } from "vitest";
import { sessionPersistence, StorageUnavailableError } from "./storage";

afterEach(() => vi.unstubAllGlobals());

it("loads without touching restricted browser storage and reports failures when used", () => {
  const browser = Object.defineProperty({}, "sessionStorage", {
    get() {
      throw new Error("Access denied");
    },
  });
  vi.stubGlobal("window", browser);
  expect(() => sessionPersistence.getItem("identity")).toThrow(
    StorageUnavailableError,
  );
  expect(() => sessionPersistence.setItem("pending", "{}")).toThrow(
    "Enable site storage",
  );
  expect(() => sessionPersistence.removeItem("pending")).toThrow(
    StorageUnavailableError,
  );
});
