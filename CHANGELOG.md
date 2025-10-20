# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

### Changed

### Fixed

## [0.0.42] - 2025-10-14

### Added

### Changed
> [!WARNING]
>Attention: Breaking change with the API Key !
>Change header from Authorization: pk_... to X-API-Key: pk_...

- feat!: refactor swagger API authentication, using X-API-Key header instead of Authorization for api key auth (#210) (Thanks @tafaust) 4b3adfa

- feat: automate version updates in release workflow (Thanks @0xfurai) 12cb83e

### Fixed

## [0.0.41] - 2025-10-11

### Added

- Feature: add api key auth (#204) (Thanks @tafaust) 0ad9871
- docs(README.md): add community terraform provider mention (#205) (Thanks @mail) 4c6a127

### Changed

- chore: update Makefile and add asdf configuration for Go environment (#202) (Thanks @tafaust) 1336d6c
- Update documentation headers for badges and intro pages by removing emojis for consistency (Thanks @0xfurai) e934fda

### Fixed

- fix(docs): update healthcheck command in Docker configurations to use wget with output option (#200) (Thanks @Shurco) a523953
- Fix/monitor partial update not update config (#194) (Thanks @0xfurai) a01404b
- Fix/update monitor 200 if monitor not found (#193) (Thanks @0xfurai) 3fab720
- refactor(monitor): update buildSetMapFromModel function to preserve created_at timestamp during updates (#192) (Thanks @0xfurai) a77456f
- fix(forms): preserve current name when resetting form in create mode across multiple components (#191) (Thanks @0xfurai) d0cc2ca

## [0.0.40] - 2025-09-25

### Added
- mask password field with toggle option for http monitor authentication block

### Changed

### Fixed
- update default data retention period to 365 days in ui and add settings navigation option

## [0.0.39] - 2025-09-08

### Added
- Chinese translation (thanks @MciG-ggg)
- General translations support (thanks @0xfurai)
-  LINE messaging channel for notifications (thanks @KarinaOliinyk)
- Keyword and JSON-over-HTTP support (thanks @0xfurai)

### Changed
- Domains are now unique across all status pages (thanks @sergeykobylchenko)

### Fixed
- Heartbeat retry interval labels now correctly show actual form values instead of the hardcoded 60/48 seconds (thanks @JustAnotherDevGuy)

## [0.0.38] - 2025-08-29

### Added
- feat: badges (#156) (Thanks @0xfurai)
- feat: enhance Playwright configuration and improve test coverage (Thanks @0xfurai)
- feat: pagertree (#152) (Thanks @KarinaOliinyk)
- feat: implement pushbullet notifications (#147) (Thanks @KarinaOliinyk)
- feat: add password visibility toggle to login form (#146) (Thanks @KarinaOliinyk and @AbhishekG-Codes)
- feat: improve landing page (Thanks @0xfurai)

### Changed
- refactor: remove unused proxy handling from Push Monitor (Thanks @0xfurai)
- refactor: remove unused Card components from Push Monitor (Thanks @0xfurai)

### Fixed

## [0.0.37] - 2025-08-18

### Added
- add twilio to notification chanel

### Changed

### Fixed
- add the server name in message for pushover
- fix broken FindAll with tags

- Fix custom domain issues

## [0.0.36] - 2025-07-28

### Added

- implement rendering certificate information for https monitors

### Changed

- enhance changelog generation script to extract GitHub usernames

### Fixed

## [0.0.35] - 2025-07-27

### Added

- Add ability to set custom domain for status pages
- Add ability to check certificate expiration and notify about it

### Changed

- Change api url for web client in dev mode - now it is proxied via Vite

### Fixed

- Fix push monitor url

