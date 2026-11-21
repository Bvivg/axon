import { describe, expect, it } from "vitest";

import { minPeopleQueryLength, searchablePeopleQuery } from "./people-search";

describe("people search queries", () => {
  it("needs two characters besides a leading @, like the server", () => {
    expect(minPeopleQueryLength).toBe(2);
    for (const query of ["", "  ", "a", "@a", "@@", " @ a", "😀"]) {
      expect(searchablePeopleQuery(query), query).toBe(false);
    }
    for (const query of ["ab", " ab ", "@ab", "жё", "ada lo", "😀😀"]) {
      expect(searchablePeopleQuery(query), query).toBe(true);
    }
  });
});
