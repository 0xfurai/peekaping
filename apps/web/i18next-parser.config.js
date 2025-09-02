export default {
  locales: ["en", "ua", "fr", "de"],
  output: "src/i18n/locales/$LOCALE.json",
  input: "src/**/*.{ts,tsx}",
  sort: true,
  createOldCatalogs: false,
};
