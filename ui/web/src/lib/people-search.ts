export const minPeopleQueryLength = 2;

export const peopleQueryHint = "Type at least 2 characters to find people.";

export function searchablePeopleQuery(query: string): boolean {
  return [...query.trim().replace(/^@+/, "").trim()].length >= minPeopleQueryLength;
}
