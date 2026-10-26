import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: "Peekaping Docs",
  tagline: "Peekaping is a monitoring tool for your servers",
  favicon: "img/favicon.ico",

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: true, // Improve compatibility with the upcoming Docusaurus v4
  },

  // Set the production url of your site here
  url: "https://docs.peekaping.com",
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: "/",

  // GitHub pages deployment config.
  // If you aren't using GitHub pages, you don't need these.
  organizationName: "0xfurai", // Usually your GitHub org/user name.
  projectName: "peekaping", // Usually your repo name.

  onBrokenLinks: "throw",
  onBrokenMarkdownLinks: "warn",

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          routeBasePath: "/",
          sidebarPath: "./sidebars.ts",
          // Remove this to remove the "edit this page" links.
          editUrl: "https://github.com/0xfurai/peekaping/tree/main/apps/docs",
        },
        blog: false,
        theme: {
          customCss: "./src/css/custom.css",
        },
        gtag: {
          trackingID: "G-DRM5LCBFK6",
          anonymizeIP: true,
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: "img/demo.webp",
    colorMode: {
      defaultMode: "dark",
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: "Peekaping",
      logo: {
        alt: "",
        src: "img/logo-mascot.webp",
      },
      hideOnScroll: false,
      items: [
        {
          type: "docSidebar",
          sidebarId: "tutorialSidebar",
          position: "left",
          label: "Docs",
        },
        {
          href: "https://peekaping.com",
          label: "Website",
          position: "left",
        },
        {
          href: "https://github.com/0xfurai/peekaping",
          position: "right",
          className: "header-github-link",
          "aria-label": "GitHub repository",
        },
        {
          href: "https://demo.peekaping.com",
          label: "Live demo",
          position: "right",
          className: "navbar-cta",
        },
      ],
    },
    footer: {
      style: "dark",
      logo: {
        alt: "Peekaping",
        src: "img/logo-mascot.webp",
        href: "https://peekaping.com",
        width: 48,
        height: 48,
      },
      links: [
        {
          title: "Docs",
          items: [
            { label: "Quick start", to: "/self-hosting/docker-with-sqlite" },
            { label: "Deployment modes", to: "/self-hosting/deployment-modes" },
            { label: "Architecture", to: "/architecture/overview" },
          ],
        },
        {
          title: "Product",
          items: [
            { label: "Website", href: "https://peekaping.com" },
            { label: "Live demo", href: "https://demo.peekaping.com" },
            { label: "Releases", href: "https://github.com/0xfurai/peekaping/releases" },
          ],
        },
        {
          title: "Community",
          items: [
            { label: "GitHub", href: "https://github.com/0xfurai/peekaping" },
            { label: "Issues", href: "https://github.com/0xfurai/peekaping/issues" },
            { label: "X", href: "https://x.com/0xfurai" },
          ],
        },
      ],
      copyright: "Open source under the MIT license.",
    },
    prism: {
      theme: prismThemes.oneLight,
      darkTheme: prismThemes.oneDark,
      additionalLanguages: ["bash", "nginx", "http", "php", "docker"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
