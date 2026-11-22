import next from "eslint-config-next";

const config = [
  {

    ignores: ["src/gen/**", ".next/**", "node_modules/**", "public/calls-sw.js"],
  },
  ...next,
];

export default config;
