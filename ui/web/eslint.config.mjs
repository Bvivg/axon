import next from "eslint-config-next";

const config = [
  {

    ignores: ["src/gen/**", ".next/**", "node_modules/**"],
  },
  ...next,
];

export default config;
