# Website

This website is built using [Docusaurus](https://docusaurus.io/), a modern static website generator.

## Installation

```bash
yarn
```

## Local Development

```bash
yarn start
```

This command starts a local development server and opens up a browser window. Most changes are reflected live without having to restart the server.

## Build

```bash
yarn build
```

This command generates static content into the `build` directory and can be served using any static contents hosting service.

## Deployment

The site is served from a Cloudflare Worker (`wrangler.jsonc`). `.github/workflows/build-docs.yml` builds it whenever `apps/docs` changes, and `.github/workflows/deploy-sites.yml` deploys the build to docs.peekaping.com from `main`, or posts a preview link on pull requests.
