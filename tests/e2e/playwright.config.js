const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: ".",
  timeout: 90_000,
  retries: 1,
  reporter: "list",
  use: { headless: true },
});
