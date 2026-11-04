import { describe, expect, it } from "vitest";
import { isPageKey, navigationKeyDirection } from "@/utils/navigationKeys.js";

describe("navigationKeyDirection", () => {
  it("maps → and PgDn to next, ← and PgUp to previous", () => {
    expect(navigationKeyDirection("ArrowRight")).toBe("next");
    expect(navigationKeyDirection("PageDown")).toBe("next");
    expect(navigationKeyDirection("ArrowLeft")).toBe("previous");
    expect(navigationKeyDirection("PageUp")).toBe("previous");
  });

  it("ignores every other key", () => {
    for (const key of ["ArrowUp", "ArrowDown", "Home", "End", " ", "f", "Shift", "", undefined]) {
      expect(navigationKeyDirection(key)).toBeNull();
    }
  });
});

describe("isPageKey", () => {
  it("is true only for PgUp and PgDn", () => {
    expect(isPageKey("PageDown")).toBe(true);
    expect(isPageKey("PageUp")).toBe(true);
    expect(isPageKey("ArrowRight")).toBe(false);
    expect(isPageKey("End")).toBe(false);
  });
});
