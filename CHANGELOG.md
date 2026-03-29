# Changelog

## [0.1.2](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.1...impartus-cli-v0.1.2) (2026-03-29)


### Features

* add job persistence and idempotency keys ([115b3fd](https://github.com/rabesss/impartus-cli/commit/115b3fd4c74ccf5a4327611e7910e4ead911ab44))
* add mission infrastructure for OpenClaw automation quality overhaul ([a1075c1](https://github.com/rabesss/impartus-cli/commit/a1075c1e0e276461d9736543f904d31715331cfc))
* add retryable and retryAfter hints to error responses ([95e6278](https://github.com/rabesss/impartus-cli/commit/95e6278bed59922ab622973e6176946ddde87daf))
* add skip-no-audio filter for lectures ([0a7d3e6](https://github.com/rabesss/impartus-cli/commit/0a7d3e683fa4607a806b6232a5413b44835477e1))
* add upstream login token cache to APIServer ([0da9f04](https://github.com/rabesss/impartus-cli/commit/0da9f044af70e920b90cd9a27058135ce5b441d8))
* enhance health endpoint with structured status ([f75680f](https://github.com/rabesss/impartus-cli/commit/f75680f9422d2fc202bbd8acec0d9cdc63431487))
* OpenClaw automation quality overhaul ([513c728](https://github.com/rabesss/impartus-cli/commit/513c7284313447953b18b7fb4d2b695f05c236ee))
* OpenClaw automation quality overhaul ([513c728](https://github.com/rabesss/impartus-cli/commit/513c7284313447953b18b7fb4d2b695f05c236ee))
* remove dead feature flags from codebase ([f78524d](https://github.com/rabesss/impartus-cli/commit/f78524d90f8c00d292f1beaee7fd3a0fa4cb3d42))
* standardize API response envelope with {success, data, error, meta} ([76daaf1](https://github.com/rabesss/impartus-cli/commit/76daaf1408cc37bbf1c1daa999359177158c611e))


### Bug Fixes

* address Gemini CodeAssist review feedback ([d8b927f](https://github.com/rabesss/impartus-cli/commit/d8b927ffd8731126b2d87e1fd327302e924226c4))
* change 'cancelled' to 'canceled' in docs to match code ([d5e839b](https://github.com/rabesss/impartus-cli/commit/d5e839b5a2ed18d898060d219a96d52de3dc4846))
* NewAPIServerWithPersistence always creates persistent store ([fe1b4a7](https://github.com/rabesss/impartus-cli/commit/fe1b4a7105387913733043c37d70558002da52a2))
* pin gosec to v2.21.4 and add pull-requests write permission ([4bed5a6](https://github.com/rabesss/impartus-cli/commit/4bed5a6e10ee68dcc34e56664d942ac9b9187bec))
* remove dead code and fix lint issues ([7292722](https://github.com/rabesss/impartus-cli/commit/7292722af9f8a7cac93f64efbca7aeda541d36fc))
* remove unused variable totalBeforeFilter in cli.go ([9775508](https://github.com/rabesss/impartus-cli/commit/9775508e683807e43c230262b9c170ad01a86306))
* resolve 5 code review issues from PR [#6](https://github.com/rabesss/impartus-cli/issues/6) ([6b6d6fb](https://github.com/rabesss/impartus-cli/commit/6b6d6fb59cdfd657093444a97a7faf04d49cb0a9))
* resolve CI workflow issues for ci-green-2 milestone ([1b2b25f](https://github.com/rabesss/impartus-cli/commit/1b2b25f8e28da8f40af78525deb0504ac891b04f))
* resolve golangci-lint errors for ci-green-2 milestone ([63a9870](https://github.com/rabesss/impartus-cli/commit/63a9870540f6e402e21fe8b01734d00dd1ab2ef6))
* resolve golangci-lint version conflicts ([b52e718](https://github.com/rabesss/impartus-cli/commit/b52e718d5e1a66ebe305a5977940bdd3c6ad2e7f))
* restore applyLectureFilters regression + respondWithSuccess schema consistency ([a370fe6](https://github.com/rabesss/impartus-cli/commit/a370fe6b706dab491f8a5409b1d1635734165bf2))
* revert Go 1.25 dependency bumps that break CI ([0ce6cf3](https://github.com/rabesss/impartus-cli/commit/0ce6cf3264e6822c7cef40fe77c22145cbfae78d))


### Refactoring

* extract upstream reachability check to reduce gocyclo ([63a2d61](https://github.com/rabesss/impartus-cli/commit/63a2d612874899906685467206a73049dc4b8d36))


### Documentation

* documentation accuracy overhaul ([#10](https://github.com/rabesss/impartus-cli/issues/10)) ([edae46b](https://github.com/rabesss/impartus-cli/commit/edae46bc8907b9c29f43b6c97700f0cf6f0177b0))
* fix 2 documentation inaccuracies found by user-testing validator ([8eb78ea](https://github.com/rabesss/impartus-cli/commit/8eb78eab712e9cdc1c7c5208287fbf38a44ede1f))
* fix 3 blocking inaccuracies + 2 non-blocking cleanup items ([6b42cf0](https://github.com/rabesss/impartus-cli/commit/6b42cf0f2572b0ae8f23839a14c623787d033c58))
* update documentation for milestones 1-3 features ([4719f4d](https://github.com/rabesss/impartus-cli/commit/4719f4d95ac0a743bb24b245582aae7e2ad2bf23))


### CI/CD

* bump actions/checkout@v6, setup-go@v6, labeler@v6, and Go dependencies ([d388cde](https://github.com/rabesss/impartus-cli/commit/d388cde2f998d0f8c454c7ccc958b5328ae997f3))

## [0.1.1](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.0...impartus-cli-v0.1.1) (2026-03-29)


### Features

* add job persistence and idempotency keys ([115b3fd](https://github.com/rabesss/impartus-cli/commit/115b3fd4c74ccf5a4327611e7910e4ead911ab44))
* add mission infrastructure for OpenClaw automation quality overhaul ([a1075c1](https://github.com/rabesss/impartus-cli/commit/a1075c1e0e276461d9736543f904d31715331cfc))
* add retryable and retryAfter hints to error responses ([95e6278](https://github.com/rabesss/impartus-cli/commit/95e6278bed59922ab622973e6176946ddde87daf))
* add skip-no-audio filter for lectures ([0a7d3e6](https://github.com/rabesss/impartus-cli/commit/0a7d3e683fa4607a806b6232a5413b44835477e1))
* add upstream login token cache to APIServer ([0da9f04](https://github.com/rabesss/impartus-cli/commit/0da9f044af70e920b90cd9a27058135ce5b441d8))
* enhance health endpoint with structured status ([f75680f](https://github.com/rabesss/impartus-cli/commit/f75680f9422d2fc202bbd8acec0d9cdc63431487))
* OpenClaw automation quality overhaul ([513c728](https://github.com/rabesss/impartus-cli/commit/513c7284313447953b18b7fb4d2b695f05c236ee))
* OpenClaw automation quality overhaul ([513c728](https://github.com/rabesss/impartus-cli/commit/513c7284313447953b18b7fb4d2b695f05c236ee))
* remove dead feature flags from codebase ([f78524d](https://github.com/rabesss/impartus-cli/commit/f78524d90f8c00d292f1beaee7fd3a0fa4cb3d42))
* standardize API response envelope with {success, data, error, meta} ([76daaf1](https://github.com/rabesss/impartus-cli/commit/76daaf1408cc37bbf1c1daa999359177158c611e))


### Bug Fixes

* address Gemini CodeAssist review feedback ([d8b927f](https://github.com/rabesss/impartus-cli/commit/d8b927ffd8731126b2d87e1fd327302e924226c4))
* change 'cancelled' to 'canceled' in docs to match code ([d5e839b](https://github.com/rabesss/impartus-cli/commit/d5e839b5a2ed18d898060d219a96d52de3dc4846))
* NewAPIServerWithPersistence always creates persistent store ([fe1b4a7](https://github.com/rabesss/impartus-cli/commit/fe1b4a7105387913733043c37d70558002da52a2))
* pin gosec to v2.21.4 and add pull-requests write permission ([4bed5a6](https://github.com/rabesss/impartus-cli/commit/4bed5a6e10ee68dcc34e56664d942ac9b9187bec))
* remove dead code and fix lint issues ([7292722](https://github.com/rabesss/impartus-cli/commit/7292722af9f8a7cac93f64efbca7aeda541d36fc))
* remove unused variable totalBeforeFilter in cli.go ([9775508](https://github.com/rabesss/impartus-cli/commit/9775508e683807e43c230262b9c170ad01a86306))
* resolve 5 code review issues from PR [#6](https://github.com/rabesss/impartus-cli/issues/6) ([6b6d6fb](https://github.com/rabesss/impartus-cli/commit/6b6d6fb59cdfd657093444a97a7faf04d49cb0a9))
* resolve CI workflow issues for ci-green-2 milestone ([1b2b25f](https://github.com/rabesss/impartus-cli/commit/1b2b25f8e28da8f40af78525deb0504ac891b04f))
* resolve golangci-lint errors for ci-green-2 milestone ([63a9870](https://github.com/rabesss/impartus-cli/commit/63a9870540f6e402e21fe8b01734d00dd1ab2ef6))
* resolve golangci-lint version conflicts ([b52e718](https://github.com/rabesss/impartus-cli/commit/b52e718d5e1a66ebe305a5977940bdd3c6ad2e7f))
* restore applyLectureFilters regression + respondWithSuccess schema consistency ([a370fe6](https://github.com/rabesss/impartus-cli/commit/a370fe6b706dab491f8a5409b1d1635734165bf2))
* revert Go 1.25 dependency bumps that break CI ([0ce6cf3](https://github.com/rabesss/impartus-cli/commit/0ce6cf3264e6822c7cef40fe77c22145cbfae78d))


### Refactoring

* extract upstream reachability check to reduce gocyclo ([63a2d61](https://github.com/rabesss/impartus-cli/commit/63a2d612874899906685467206a73049dc4b8d36))


### Documentation

* fix 2 documentation inaccuracies found by user-testing validator ([8eb78ea](https://github.com/rabesss/impartus-cli/commit/8eb78eab712e9cdc1c7c5208287fbf38a44ede1f))
* fix 3 blocking inaccuracies + 2 non-blocking cleanup items ([6b42cf0](https://github.com/rabesss/impartus-cli/commit/6b42cf0f2572b0ae8f23839a14c623787d033c58))
* update documentation for milestones 1-3 features ([4719f4d](https://github.com/rabesss/impartus-cli/commit/4719f4d95ac0a743bb24b245582aae7e2ad2bf23))


### CI/CD

* bump actions/checkout@v6, setup-go@v6, labeler@v6, and Go dependencies ([d388cde](https://github.com/rabesss/impartus-cli/commit/d388cde2f998d0f8c454c7ccc958b5328ae997f3))

## Changelog

This file is managed by release automation.
