function hex(bytes: number) {
  const values = crypto.getRandomValues(new Uint8Array(bytes));
  return Array.from(values, (value) => value.toString(16).padStart(2, "0")).join("");
}

export function traceparent() {
  return `00-${hex(16)}-${hex(8)}-01`;
}
