# Changelog

## [0.1.18](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.17...impartus-cli-v0.1.18) (2026-07-02)


### Bug Fixes

* remediate security and quality review findings (P0 token leak, path traversal, network exposure) ([#83](https://github.com/rabesss/impartus-cli/issues/83)) ([b4ebcde](https://github.com/rabesss/impartus-cli/commit/b4ebcde24ccff870adbcae2dcef0070a8dddb2a1))


### CI/CD

* add ZAI Coding Plan OpenCode config ([#81](https://github.com/rabesss/impartus-cli/issues/81)) ([0c9f408](https://github.com/rabesss/impartus-cli/commit/0c9f4082a61155dd34bed4964d130a6247709b1d))

## [0.1.17](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.16...impartus-cli-v0.1.17) (2026-07-02)


### Documentation

* remove agent configs from repo and sync user-facing docs ([bca8927](https://github.com/rabesss/impartus-cli/commit/bca89276b3974643381400abd4a25537dab2a265))

## [0.1.16](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.15...impartus-cli-v0.1.16) (2026-06-30)


### Features

* add job persistence and idempotency keys ([54a3dd9](https://github.com/rabesss/impartus-cli/commit/54a3dd932f7b24640cbeeb8863e2d8ec47e6714d))
* add retryable and retryAfter hints to error responses ([2140e76](https://github.com/rabesss/impartus-cli/commit/2140e76f1c9eb4debf6ea73f9ecf1df658108d5e))
* add skip-no-audio filter for lectures ([ce8dc28](https://github.com/rabesss/impartus-cli/commit/ce8dc280ba833d76113a132d42f6f7c1270ac271))
* add upstream login token cache to APIServer ([1021edf](https://github.com/rabesss/impartus-cli/commit/1021edfac2dda26213d0c7598948db45f22d2dc6))
* **cli:** add play command for direct streaming with mpv ([#44](https://github.com/rabesss/impartus-cli/issues/44)) ([cd416ca](https://github.com/rabesss/impartus-cli/commit/cd416ca4e4f73756eafe076757c1382355ab3cb8))
* enhance health endpoint with structured status ([ef3a3a3](https://github.com/rabesss/impartus-cli/commit/ef3a3a3819b6b73e8727f390214a2efb72c7fa68))
* OpenClaw automation quality overhaul ([a92c49b](https://github.com/rabesss/impartus-cli/commit/a92c49ba6fc6afc1f086c3db00bd289d07fa61ab))
* OpenClaw automation quality overhaul ([a92c49b](https://github.com/rabesss/impartus-cli/commit/a92c49ba6fc6afc1f086c3db00bd289d07fa61ab))
* remove dead feature flags from codebase ([a4cece8](https://github.com/rabesss/impartus-cli/commit/a4cece8418f2640f64acf62d975821d8c5db2e83))
* standardize API response envelope with {success, data, error, meta} ([3f2e72c](https://github.com/rabesss/impartus-cli/commit/3f2e72c3f480080d362f9f27595fe5885ab9a436))


### Bug Fixes

* address Gemini CodeAssist review feedback ([fe8a825](https://github.com/rabesss/impartus-cli/commit/fe8a825aec28e60370d4d361c1e7ddbccbd9eb48))
* change 'cancelled' to 'canceled' in docs to match code ([6bc4404](https://github.com/rabesss/impartus-cli/commit/6bc4404973e4f051cc0c46adacab6b3d916962af))
* **ci:** correct Qodo pr_commands and Socket trigger paths from PR [#55](https://github.com/rabesss/impartus-cli/issues/55) review ([a14a136](https://github.com/rabesss/impartus-cli/commit/a14a1366709855b0cb5dc2219894ba6983f34990))
* **ci:** resolve CI pipeline failures after Go 1.25 upgrade ([7ba91ba](https://github.com/rabesss/impartus-cli/commit/7ba91bad45951dea425fa85ff4cd6c0a32af18e4))
* **docker:** bump Go base image from 1.24.7 to 1.25-bookworm ([83a5194](https://github.com/rabesss/impartus-cli/commit/83a51940d97f6a549a3cf857fb88849b1f8725f3))
* NewAPIServerWithPersistence always creates persistent store ([56733f3](https://github.com/rabesss/impartus-cli/commit/56733f33bf332eda9ee67f0772c137e0bdd4cfda))
* pin gosec to v2.21.4 and add pull-requests write permission ([d592e3a](https://github.com/rabesss/impartus-cli/commit/d592e3a002cc8a0eec4e8fa411ced32ab2de2ce0))
* **pullfrog:** pin model to zai/glm-5.2 for Z.AI Coding Plan BYOK ([#70](https://github.com/rabesss/impartus-cli/issues/70)) ([080d113](https://github.com/rabesss/impartus-cli/commit/080d113e12285d76f4e28dd589ae31c6158118a4))
* **pullfrog:** use zai-coding-plan/glm-5.2 model slug ([#71](https://github.com/rabesss/impartus-cli/issues/71)) ([918c153](https://github.com/rabesss/impartus-cli/commit/918c153e80be06133fae2d9aad7273304bcee50d))
* remove dead code and fix lint issues ([42d32e6](https://github.com/rabesss/impartus-cli/commit/42d32e6373988829b3309373b0883709bb28bc13))
* remove unused variable totalBeforeFilter in cli.go ([ed68752](https://github.com/rabesss/impartus-cli/commit/ed687520edc7c410b4508c860f641a52763c9e5e))
* resolve 5 code review issues from PR [#6](https://github.com/rabesss/impartus-cli/issues/6) ([ccd75c1](https://github.com/rabesss/impartus-cli/commit/ccd75c115308d42f8e70017e854eea725b9e7e6e))
* resolve CI workflow issues for ci-green-2 milestone ([a9bfad4](https://github.com/rabesss/impartus-cli/commit/a9bfad4f3cb7dc8691eec70dab31758dceecab8d))
* resolve golangci-lint errors for ci-green-2 milestone ([2e379eb](https://github.com/rabesss/impartus-cli/commit/2e379eb4213252f1b63f3e3ea0c44194a2ae56d8))
* resolve golangci-lint version conflicts ([eeb8ce1](https://github.com/rabesss/impartus-cli/commit/eeb8ce18a355209bff42191b63695025453aa935))
* restore applyLectureFilters regression + respondWithSuccess schema consistency ([b23b996](https://github.com/rabesss/impartus-cli/commit/b23b9960a17048a35348c933bfed4dc1d29ed2c6))
* revert Go 1.25 dependency bumps that break CI ([4de9955](https://github.com/rabesss/impartus-cli/commit/4de995570c73fd6e17264e81be8a7db70c5d41a5))
* **security:** remediate all 25 deepsec security audit findings ([#34](https://github.com/rabesss/impartus-cli/issues/34)) ([49717ef](https://github.com/rabesss/impartus-cli/commit/49717ef5a7b0950c7f3083ea637057d9c3ff0e94))


### Performance

* **downloader:** optimize bounded fanout downloads ([431d5b5](https://github.com/rabesss/impartus-cli/commit/431d5b520ae128811a44b5da6fe4b7da0077a7f3))


### Refactoring

* address code-quality findings and expand test coverage ([#54](https://github.com/rabesss/impartus-cli/issues/54)) ([47edc39](https://github.com/rabesss/impartus-cli/commit/47edc3937c8db53006aff3880a92d19665a7a5cf))
* extract upstream reachability check to reduce gocyclo ([5e82cfa](https://github.com/rabesss/impartus-cli/commit/5e82cfa3cb43865dfb53768e38c71a33c55c2ad6))
* fix 7 structural code quality issues from audit ([#52](https://github.com/rabesss/impartus-cli/issues/52)) ([e83f59c](https://github.com/rabesss/impartus-cli/commit/e83f59c65121ce60e0bef07de4fd229d6b215c4d))


### Documentation

* add contributing guide ([df82944](https://github.com/rabesss/impartus-cli/commit/df829444d069df175c9ad9b91bc892f3f54e435a))
* add MIT license ([cbec725](https://github.com/rabesss/impartus-cli/commit/cbec725b4211741e8981d2a37fd01804293a696c))
* add security policy ([0d3b0de](https://github.com/rabesss/impartus-cli/commit/0d3b0dea6837eb4fc939cb6456a029e3449060a7))
* documentation accuracy overhaul ([#10](https://github.com/rabesss/impartus-cli/issues/10)) ([d4e468c](https://github.com/rabesss/impartus-cli/commit/d4e468ceca1dcea5ef26f34e23daa605a072a3fd))
* fix 2 documentation inaccuracies found by user-testing validator ([2d9c3a4](https://github.com/rabesss/impartus-cli/commit/2d9c3a4ae2858a4f76093ed7b3902a07d83f4ea3))
* fix 3 blocking inaccuracies + 2 non-blocking cleanup items ([5672c57](https://github.com/rabesss/impartus-cli/commit/5672c57b8548d053f091f524277ff1865aecf402))
* fix rendering issues in README and docs ([4600cb5](https://github.com/rabesss/impartus-cli/commit/4600cb57584968ea9927868b6695a33dd52dd70f))
* update documentation for milestones 1-3 features ([d1bcbd6](https://github.com/rabesss/impartus-cli/commit/d1bcbd6f4a26b7c1b4f64b88de9ec9f8840cf7c7))
* update project documentation to reflect current CI and tooling ([63f515c](https://github.com/rabesss/impartus-cli/commit/63f515c55f9a719e842a32dd438b810d57839ef7))


### Testing

* **downloader:** tolerate rate limiter deadline errors ([77ddb48](https://github.com/rabesss/impartus-cli/commit/77ddb48abd2763cd81e35c16358abee0d464aa1a))


### Build System

* **deps:** bump debian base image digest ([#49](https://github.com/rabesss/impartus-cli/issues/49)) ([67ad4da](https://github.com/rabesss/impartus-cli/commit/67ad4dae84ce3f5045986868963c34a709c5519b))
* **deps:** bump debian from `0104b33` to `96e378d` ([#66](https://github.com/rabesss/impartus-cli/issues/66)) ([a40b71f](https://github.com/rabesss/impartus-cli/commit/a40b71fed5a4fb75e099c6b4bb299bb8c18a09a0))
* **deps:** bump debian from `f065376` to `67b30a6` ([7268bc8](https://github.com/rabesss/impartus-cli/commit/7268bc8aa2b7b5f29a5260a7fcae5ce5fce1595d))
* **deps:** bump golang base image digest ([#50](https://github.com/rabesss/impartus-cli/issues/50)) ([b7bd74a](https://github.com/rabesss/impartus-cli/commit/b7bd74ac875e6a44967f5653ed372fb2cac32b5d))
* **deps:** bump golang from `386d475` to `5d2b868` ([#63](https://github.com/rabesss/impartus-cli/issues/63)) ([9bafa23](https://github.com/rabesss/impartus-cli/commit/9bafa23bd5ccf7b890efd309f15e53e8864df073))
* **deps:** bump golang from 1.25-bookworm to 1.26-bookworm ([18dc2bb](https://github.com/rabesss/impartus-cli/commit/18dc2bb5751de3f04ceec316a9323349a2d273ad))


### CI/CD

* add semantic PR title validation workflow and PR template ([8b5811d](https://github.com/rabesss/impartus-cli/commit/8b5811dae2c96ba996d979634f610d027db90f67))
* bump actions/checkout@v6, setup-go@v6, labeler@v6, and Go dependencies ([b796692](https://github.com/rabesss/impartus-cli/commit/b79669269985b3691dc13696c1dc52d17f9d7328))
* **deps:** bump actions/checkout from 6.0.2 to 6.0.3 ([#61](https://github.com/rabesss/impartus-cli/issues/61)) ([24ff5fd](https://github.com/rabesss/impartus-cli/commit/24ff5fdd518a34add488e999bf46fdd66921545d))
* **deps:** bump actions/upload-artifact from 4 to 7 ([cd49bac](https://github.com/rabesss/impartus-cli/commit/cd49baceb45e6de1c9d098a40ac125318817472a))
* **deps:** bump codecov/codecov-action from 6.0.0 to 6.0.1 ([#45](https://github.com/rabesss/impartus-cli/issues/45)) ([5ffed80](https://github.com/rabesss/impartus-cli/commit/5ffed80123c413a817555765f3373b19fe54d4a8))
* **deps:** bump codecov/codecov-action from 6.0.1 to 7.0.0 ([#60](https://github.com/rabesss/impartus-cli/issues/60)) ([b029c65](https://github.com/rabesss/impartus-cli/commit/b029c654dfd826166833490e05f1640e930e4d58))
* **deps:** bump docker/login-action from 3 to 4 ([1fd66bd](https://github.com/rabesss/impartus-cli/commit/1fd66bde1b04b3984ace88d913e726d08ed35102))
* **deps:** bump docker/metadata-action from 5 to 6 ([776dec2](https://github.com/rabesss/impartus-cli/commit/776dec22699201d3df02bb64cbacb6a664345c44))
* **deps:** bump docker/setup-qemu-action from 3 to 4 ([c74f05c](https://github.com/rabesss/impartus-cli/commit/c74f05c50f714d6c0f3a00fe5c6e7aaddac57c2f))
* **deps:** bump github/codeql-action ([717d24e](https://github.com/rabesss/impartus-cli/commit/717d24e2af8ce50589a60f86d0d18f6e21dfbb65))
* **deps:** bump github/codeql-action from 4.35.5 to 4.36.0 ([#48](https://github.com/rabesss/impartus-cli/issues/48)) ([16041e5](https://github.com/rabesss/impartus-cli/commit/16041e5d8139f46501f648de9c0ba1a69e293b17))
* **deps:** bump github/codeql-action from 4.36.0 to 4.36.2 ([#62](https://github.com/rabesss/impartus-cli/issues/62)) ([0a3d183](https://github.com/rabesss/impartus-cli/commit/0a3d1839f92218f6ba8c081e22f810c7a7a2b15b))
* **deps:** bump gitleaks/gitleaks-action from 2.3.9 to 3.0.0 ([#58](https://github.com/rabesss/impartus-cli/issues/58)) ([a6304c4](https://github.com/rabesss/impartus-cli/commit/a6304c46b336dbea9d304fdc532f350a79c7bd9e))
* **deps:** bump golangci/golangci-lint-action from 9.2.0 to 9.2.1 ([#46](https://github.com/rabesss/impartus-cli/issues/46)) ([2aecf2f](https://github.com/rabesss/impartus-cli/commit/2aecf2f5b690ddfd85c61d87af0ae2c0b96fb6cc))
* **deps:** bump googleapis/release-please-action from 4 to 5 ([85dc281](https://github.com/rabesss/impartus-cli/commit/85dc281cf13efde8e7ae4927d69ec41b755ae0ea))
* fix Codecov v7 input, add GHCR Trivy scan, refresh Dockerfile dates ([#68](https://github.com/rabesss/impartus-cli/issues/68)) ([a438d17](https://github.com/rabesss/impartus-cli/commit/a438d17e20476bc1cf6ad20fbaaddd77a0780221))
* keep desloppify quality gate advisory ([351e935](https://github.com/rabesss/impartus-cli/commit/351e935b893555fd62db51c06bfcc7807e9ad17d))

## [0.1.15](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.14...impartus-cli-v0.1.15) (2026-06-30)


### Bug Fixes

* **pullfrog:** pin model to zai/glm-5.2 for Z.AI Coding Plan BYOK ([#70](https://github.com/rabesss/impartus-cli/issues/70)) ([080d113](https://github.com/rabesss/impartus-cli/commit/080d113e12285d76f4e28dd589ae31c6158118a4))
* **pullfrog:** use zai-coding-plan/glm-5.2 model slug ([#71](https://github.com/rabesss/impartus-cli/issues/71)) ([918c153](https://github.com/rabesss/impartus-cli/commit/918c153e80be06133fae2d9aad7273304bcee50d))


### Build System

* **deps:** bump debian from `96e378d` to `60eac75` ([#75](https://github.com/rabesss/impartus-cli/issues/75))
* **deps:** bump golang from `5d2b868` to `b305420` ([#76](https://github.com/rabesss/impartus-cli/issues/76))


### CI/CD

* fix Codecov v7 input, add GHCR Trivy scan, refresh Dockerfile dates ([#68](https://github.com/rabesss/impartus-cli/issues/68)) ([a438d17](https://github.com/rabesss/impartus-cli/commit/a438d17e20476bc1cf6ad20fbaaddd77a0780221))
* **deps:** bump actions/checkout from 6.0.2 to 7.0.0 ([#74](https://github.com/rabesss/impartus-cli/issues/74))
* **deps:** bump actions/setup-go from 6.4.0 to 6.5.0 ([#72](https://github.com/rabesss/impartus-cli/issues/72))
* **deps:** bump softprops/action-gh-release from 3.0.0 to 3.0.1 ([#73](https://github.com/rabesss/impartus-cli/issues/73))

## [0.1.14](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.13...impartus-cli-v0.1.14) (2026-06-27)


### Build System

* **deps:** bump debian from `0104b33` to `96e378d` ([#66](https://github.com/rabesss/impartus-cli/issues/66)) ([a40b71f](https://github.com/rabesss/impartus-cli/commit/a40b71fed5a4fb75e099c6b4bb299bb8c18a09a0))
* **deps:** bump golang from `386d475` to `5d2b868` ([#63](https://github.com/rabesss/impartus-cli/issues/63)) ([9bafa23](https://github.com/rabesss/impartus-cli/commit/9bafa23bd5ccf7b890efd309f15e53e8864df073))


### CI/CD

* **deps:** bump actions/checkout from 6.0.2 to 6.0.3 ([#61](https://github.com/rabesss/impartus-cli/issues/61)) ([24ff5fd](https://github.com/rabesss/impartus-cli/commit/24ff5fdd518a34add488e999bf46fdd66921545d))
* **deps:** bump codecov/codecov-action from 6.0.1 to 7.0.0 ([#60](https://github.com/rabesss/impartus-cli/issues/60)) ([b029c65](https://github.com/rabesss/impartus-cli/commit/b029c654dfd826166833490e05f1640e930e4d58))
* **deps:** bump github/codeql-action from 4.36.0 to 4.36.2 ([#62](https://github.com/rabesss/impartus-cli/issues/62)) ([0a3d183](https://github.com/rabesss/impartus-cli/commit/0a3d1839f92218f6ba8c081e22f810c7a7a2b15b))

## [0.1.13](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.12...impartus-cli-v0.1.13) (2026-06-02)


### CI/CD

* **deps:** bump gitleaks/gitleaks-action from 2.3.9 to 3.0.0 ([#58](https://github.com/rabesss/impartus-cli/issues/58)) ([a6304c4](https://github.com/rabesss/impartus-cli/commit/a6304c46b336dbea9d304fdc532f350a79c7bd9e))

## [0.1.12](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.11...impartus-cli-v0.1.12) (2026-06-01)


### Documentation

* add contributing guide ([df82944](https://github.com/rabesss/impartus-cli/commit/df829444d069df175c9ad9b91bc892f3f54e435a))
* add MIT license ([cbec725](https://github.com/rabesss/impartus-cli/commit/cbec725b4211741e8981d2a37fd01804293a696c))
* add security policy ([0d3b0de](https://github.com/rabesss/impartus-cli/commit/0d3b0dea6837eb4fc939cb6456a029e3449060a7))

## [0.1.11](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.10...impartus-cli-v0.1.11) (2026-05-30)


### Bug Fixes

* **ci:** correct Qodo pr_commands and Socket trigger paths from PR [#55](https://github.com/rabesss/impartus-cli/issues/55) review ([a14a136](https://github.com/rabesss/impartus-cli/commit/a14a1366709855b0cb5dc2219894ba6983f34990))


### Refactoring

* address code-quality findings and expand test coverage ([#54](https://github.com/rabesss/impartus-cli/issues/54)) ([47edc39](https://github.com/rabesss/impartus-cli/commit/47edc3937c8db53006aff3880a92d19665a7a5cf))

## [0.1.10](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.9...impartus-cli-v0.1.10) (2026-05-29)


### Refactoring

* fix 7 structural code quality issues from audit ([#52](https://github.com/rabesss/impartus-cli/issues/52)) ([e83f59c](https://github.com/rabesss/impartus-cli/commit/e83f59c65121ce60e0bef07de4fd229d6b215c4d))

## [0.1.9](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.8...impartus-cli-v0.1.9) (2026-05-25)


### Features

* **cli:** add play command for direct streaming with mpv ([#44](https://github.com/rabesss/impartus-cli/issues/44)) ([cd416ca](https://github.com/rabesss/impartus-cli/commit/cd416ca4e4f73756eafe076757c1382355ab3cb8))


### Build System

* **deps:** bump debian base image digest ([#49](https://github.com/rabesss/impartus-cli/issues/49)) ([67ad4da](https://github.com/rabesss/impartus-cli/commit/67ad4dae84ce3f5045986868963c34a709c5519b))
* **deps:** bump golang base image digest ([#50](https://github.com/rabesss/impartus-cli/issues/50)) ([b7bd74a](https://github.com/rabesss/impartus-cli/commit/b7bd74ac875e6a44967f5653ed372fb2cac32b5d))


### CI/CD

* **deps:** bump codecov/codecov-action from 6.0.0 to 6.0.1 ([#45](https://github.com/rabesss/impartus-cli/issues/45)) ([5ffed80](https://github.com/rabesss/impartus-cli/commit/5ffed80123c413a817555765f3373b19fe54d4a8))
* **deps:** bump github/codeql-action from 4.35.5 to 4.36.0 ([#48](https://github.com/rabesss/impartus-cli/issues/48)) ([16041e5](https://github.com/rabesss/impartus-cli/commit/16041e5d8139f46501f648de9c0ba1a69e293b17))
* **deps:** bump golangci/golangci-lint-action from 9.2.0 to 9.2.1 ([#46](https://github.com/rabesss/impartus-cli/issues/46)) ([2aecf2f](https://github.com/rabesss/impartus-cli/commit/2aecf2f5b690ddfd85c61d87af0ae2c0b96fb6cc))

## [0.1.8](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.7...impartus-cli-v0.1.8) (2026-05-17)


### Bug Fixes

* **security:** remediate all 25 deepsec security audit findings ([#34](https://github.com/rabesss/impartus-cli/issues/34)) ([49717ef](https://github.com/rabesss/impartus-cli/commit/49717ef5a7b0950c7f3083ea637057d9c3ff0e94))


### Build System

* **deps:** bump debian from `f065376` to `67b30a6` ([7268bc8](https://github.com/rabesss/impartus-cli/commit/7268bc8aa2b7b5f29a5260a7fcae5ce5fce1595d))
* **deps:** bump golang from 1.25-bookworm to 1.26-bookworm ([18dc2bb](https://github.com/rabesss/impartus-cli/commit/18dc2bb5751de3f04ceec316a9323349a2d273ad))


### CI/CD

* **deps:** bump docker/login-action from 3 to 4 ([1fd66bd](https://github.com/rabesss/impartus-cli/commit/1fd66bde1b04b3984ace88d913e726d08ed35102))
* **deps:** bump docker/setup-qemu-action from 3 to 4 ([c74f05c](https://github.com/rabesss/impartus-cli/commit/c74f05c50f714d6c0f3a00fe5c6e7aaddac57c2f))
* **deps:** bump github/codeql-action ([717d24e](https://github.com/rabesss/impartus-cli/commit/717d24e2af8ce50589a60f86d0d18f6e21dfbb65))

## [0.1.7](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.6...impartus-cli-v0.1.7) (2026-05-16)


### Testing

* **downloader:** tolerate rate limiter deadline errors ([77ddb48](https://github.com/rabesss/impartus-cli/commit/77ddb48abd2763cd81e35c16358abee0d464aa1a))

## [0.1.6](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.5...impartus-cli-v0.1.6) (2026-05-16)


### Performance

* **downloader:** optimize bounded fanout downloads ([431d5b5](https://github.com/rabesss/impartus-cli/commit/431d5b520ae128811a44b5da6fe4b7da0077a7f3))


### Documentation

* fix rendering issues in README and docs ([4600cb5](https://github.com/rabesss/impartus-cli/commit/4600cb57584968ea9927868b6695a33dd52dd70f))
* update project documentation to reflect current CI and tooling ([63f515c](https://github.com/rabesss/impartus-cli/commit/63f515c55f9a719e842a32dd438b810d57839ef7))


### CI/CD

* **deps:** bump actions/upload-artifact from 4 to 7 ([cd49bac](https://github.com/rabesss/impartus-cli/commit/cd49baceb45e6de1c9d098a40ac125318817472a))
* **deps:** bump docker/metadata-action from 5 to 6 ([776dec2](https://github.com/rabesss/impartus-cli/commit/776dec22699201d3df02bb64cbacb6a664345c44))
* **deps:** bump googleapis/release-please-action from 4 to 5 ([85dc281](https://github.com/rabesss/impartus-cli/commit/85dc281cf13efde8e7ae4927d69ec41b755ae0ea))
* keep desloppify quality gate advisory ([351e935](https://github.com/rabesss/impartus-cli/commit/351e935b893555fd62db51c06bfcc7807e9ad17d))

## [0.1.5](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.4...impartus-cli-v0.1.5) (2026-05-07)


### Bug Fixes

* **docker:** bump Go base image from 1.24.7 to 1.25-bookworm ([83a5194](https://github.com/rabesss/impartus-cli/commit/83a51940d97f6a549a3cf857fb88849b1f8725f3))

## [0.1.4](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.3...impartus-cli-v0.1.4) (2026-05-07)


### Bug Fixes

* **ci:** resolve CI pipeline failures after Go 1.25 upgrade ([7ba91ba](https://github.com/rabesss/impartus-cli/commit/7ba91bad45951dea425fa85ff4cd6c0a32af18e4))


### CI/CD

* add semantic PR title validation workflow and PR template ([8b5811d](https://github.com/rabesss/impartus-cli/commit/8b5811dae2c96ba996d979634f610d027db90f67))

## [0.1.3](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.2...impartus-cli-v0.1.3) (2026-04-23)


### Features

* add job persistence and idempotency keys ([54a3dd9](https://github.com/rabesss/impartus-cli/commit/54a3dd932f7b24640cbeeb8863e2d8ec47e6714d))
* add retryable and retryAfter hints to error responses ([2140e76](https://github.com/rabesss/impartus-cli/commit/2140e76f1c9eb4debf6ea73f9ecf1df658108d5e))
* add skip-no-audio filter for lectures ([ce8dc28](https://github.com/rabesss/impartus-cli/commit/ce8dc280ba833d76113a132d42f6f7c1270ac271))
* add upstream login token cache to APIServer ([1021edf](https://github.com/rabesss/impartus-cli/commit/1021edfac2dda26213d0c7598948db45f22d2dc6))
* enhance health endpoint with structured status ([ef3a3a3](https://github.com/rabesss/impartus-cli/commit/ef3a3a3819b6b73e8727f390214a2efb72c7fa68))
* OpenClaw automation quality overhaul ([a92c49b](https://github.com/rabesss/impartus-cli/commit/a92c49ba6fc6afc1f086c3db00bd289d07fa61ab))
* remove dead feature flags from codebase ([a4cece8](https://github.com/rabesss/impartus-cli/commit/a4cece8418f2640f64acf62d975821d8c5db2e83))
* standardize API response envelope with {success, data, error, meta} ([3f2e72c](https://github.com/rabesss/impartus-cli/commit/3f2e72c3f480080d362f9f27595fe5885ab9a436))


### Bug Fixes

* address Gemini CodeAssist review feedback ([fe8a825](https://github.com/rabesss/impartus-cli/commit/fe8a825aec28e60370d4d361c1e7ddbccbd9eb48))
* change 'cancelled' to 'canceled' in docs to match code ([6bc4404](https://github.com/rabesss/impartus-cli/commit/6bc4404973e4f051cc0c46adacab6b3d916962af))
* NewAPIServerWithPersistence always creates persistent store ([56733f3](https://github.com/rabesss/impartus-cli/commit/56733f33bf332eda9ee67f0772c137e0bdd4cfda))
* pin gosec to v2.21.4 and add pull-requests write permission ([d592e3a](https://github.com/rabesss/impartus-cli/commit/d592e3a002cc8a0eec4e8fa411ced32ab2de2ce0))
* remove dead code and fix lint issues ([42d32e6](https://github.com/rabesss/impartus-cli/commit/42d32e6373988829b3309373b0883709bb28bc13))
* remove unused variable totalBeforeFilter in cli.go ([ed68752](https://github.com/rabesss/impartus-cli/commit/ed687520edc7c410b4508c860f641a52763c9e5e))
* resolve 5 code review issues from PR [#6](https://github.com/rabesss/impartus-cli/issues/6) ([ccd75c1](https://github.com/rabesss/impartus-cli/commit/ccd75c115308d42f8e70017e854eea725b9e7e6e))
* resolve CI workflow issues for ci-green-2 milestone ([a9bfad4](https://github.com/rabesss/impartus-cli/commit/a9bfad4f3cb7dc8691eec70dab31758dceecab8d))
* resolve golangci-lint errors for ci-green-2 milestone ([2e379eb](https://github.com/rabesss/impartus-cli/commit/2e379eb4213252f1b63f3e3ea0c44194a2ae56d8))
* resolve golangci-lint version conflicts ([eeb8ce1](https://github.com/rabesss/impartus-cli/commit/eeb8ce18a355209bff42191b63695025453aa935))
* restore applyLectureFilters regression + respondWithSuccess schema consistency ([b23b996](https://github.com/rabesss/impartus-cli/commit/b23b9960a17048a35348c933bfed4dc1d29ed2c6))
* revert Go 1.25 dependency bumps that break CI ([4de9955](https://github.com/rabesss/impartus-cli/commit/4de995570c73fd6e17264e81be8a7db70c5d41a5))


### Refactoring

* extract upstream reachability check to reduce gocyclo ([5e82cfa](https://github.com/rabesss/impartus-cli/commit/5e82cfa3cb43865dfb53768e38c71a33c55c2ad6))


### Documentation

* documentation accuracy overhaul ([#10](https://github.com/rabesss/impartus-cli/issues/10)) ([d4e468c](https://github.com/rabesss/impartus-cli/commit/d4e468ceca1dcea5ef26f34e23daa605a072a3fd))
* fix 2 documentation inaccuracies found by user-testing validator ([2d9c3a4](https://github.com/rabesss/impartus-cli/commit/2d9c3a4ae2858a4f76093ed7b3902a07d83f4ea3))
* fix 3 blocking inaccuracies + 2 non-blocking cleanup items ([5672c57](https://github.com/rabesss/impartus-cli/commit/5672c57b8548d053f091f524277ff1865aecf402))
* update documentation for milestones 1-3 features ([d1bcbd6](https://github.com/rabesss/impartus-cli/commit/d1bcbd6f4a26b7c1b4f64b88de9ec9f8840cf7c7))


### CI/CD

* bump actions/checkout@v6, setup-go@v6, labeler@v6, and Go dependencies ([b796692](https://github.com/rabesss/impartus-cli/commit/b79669269985b3691dc13696c1dc52d17f9d7328))

## [0.1.2] (2026-03-29)


### Features

* add job persistence and idempotency keys ([115b3fd](https://github.com/rabesss/impartus-cli/commit/115b3fd4c74ccf5a4327611e7910e4ead911ab44))
* add mission infrastructure for OpenClaw automation quality overhaul ([a1075c1](https://github.com/rabesss/impartus-cli/commit/a1075c1e0e276461d9736543f904d31715331cfc))
* add retryable and retryAfter hints to error responses ([95e6278](https://github.com/rabesss/impartus-cli/commit/95e6278bed59922ab622973e6176946ddde87daf))
* add skip-no-audio filter for lectures ([0a7d3e6](https://github.com/rabesss/impartus-cli/commit/0a7d3e683fa4607a806b6232a5413b44835477e1))
* add upstream login token cache to APIServer ([0da9f04](https://github.com/rabesss/impartus-cli/commit/0da9f044af70e920b90cd9a27058135ce5b441d8))
* enhance health endpoint with structured status ([f75680f](https://github.com/rabesss/impartus-cli/commit/f75680f9422d2fc202bbd8acec0d9cdc63431487))
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
