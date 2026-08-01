import next from "eslint-config-next";

const config = [
  {
    // Generated from the contracts by `make proto`. It is not ours to lint, and
    // it is rewritten on every generation.
    ignores: ["src/gen/**", ".next/**", "node_modules/**"],
  },
  ...next,
];

export default config;
