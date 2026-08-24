# Changelog

## [0.1.27](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.26...impartus-cli-v0.1.27) (2026-08-24)


### Features

* **cli:** add responsive workspace shell ([#178](https://github.com/rabesss/impartus-cli/issues/178)) ([9dfdf19](https://github.com/rabesss/impartus-cli/commit/9dfdf19f5efef21d37d6403a85ea6435d7ccfc20))
* **cli:** render startup authentication failures with safe recovery ([18046cd](https://github.com/rabesss/impartus-cli/commit/18046cd90c9bc945fdba2354845fd81359a4159f))
* **tui:** add safe authentication recovery ([89f9c37](https://github.com/rabesss/impartus-cli/commit/89f9c37c31727a7e44ab7cc4447417991439baa5))


### Bug Fixes

* **api:** preserve typed authentication failures ([ccf37ee](https://github.com/rabesss/impartus-cli/commit/ccf37ee4e90fc1543841d27bcbcac007b7efd749))
* **api:** preserve typed authentication failures ([10d8407](https://github.com/rabesss/impartus-cli/commit/10d84079c5a35d99c958a9e4706233be1abdca09))
* **ci:** preserve historical release replays ([0154311](https://github.com/rabesss/impartus-cli/commit/0154311841858038ef9e6dbc8e1abf7fc599854d))
* **ci:** publish versioned release images ([2b01c22](https://github.com/rabesss/impartus-cli/commit/2b01c226559ee218a271dcc7f55ced529f5d2fb6))
* **ci:** publish versioned release images ([973ed51](https://github.com/rabesss/impartus-cli/commit/973ed519454b3dbbe742e0b5a95bb1fc412cd82a))
* **ci:** reject empty package replays ([8f8aec1](https://github.com/rabesss/impartus-cli/commit/8f8aec1c8e75585904d06a0701cf90e049e152ac))
* **cli:** make command help explicit ([25b847d](https://github.com/rabesss/impartus-cli/commit/25b847de716f92175c15c7a0cd96b03b62df3ed5))
* **cli:** make command help explicit ([f7827ff](https://github.com/rabesss/impartus-cli/commit/f7827ff3e6f658077b3115222bf1fa979da267a2))
* **cli:** reject unknown nested help ([27841bf](https://github.com/rabesss/impartus-cli/commit/27841bf8be36001600f399820d637955a29e50dd))
* **downloader:** preserve aggregated auth failures ([f221e9d](https://github.com/rabesss/impartus-cli/commit/f221e9dafeb5dbcb96bb9adc668939a50d04d25a))
* **downloader:** type download-stage auth failures ([950b334](https://github.com/rabesss/impartus-cli/commit/950b334d21c09fcc065447caef86af4219cb292e))
* stamp source build metadata ([#177](https://github.com/rabesss/impartus-cli/issues/177)) ([9c6ece0](https://github.com/rabesss/impartus-cli/commit/9c6ece0948d53c72f48fccc6a969576418694375))
* **tui:** align retry state across views ([31bce69](https://github.com/rabesss/impartus-cli/commit/31bce693707978410485758833636ff8761ca8df))
* **tui:** finish authentication recovery review ([3abafd7](https://github.com/rabesss/impartus-cli/commit/3abafd7411b6aa48b3591fc22a6c6beed1941aad))
* **tui:** harden authentication retry recovery ([b6dde07](https://github.com/rabesss/impartus-cli/commit/b6dde07d7ab7051836ac3b2139c30b42f22c6215))
* **tui:** harden recovery edge cases ([1a140e8](https://github.com/rabesss/impartus-cli/commit/1a140e8afc83f4120326c3e243e506bbf3a6310e))
* **tui:** reconcile overlapping auth retries ([78c916b](https://github.com/rabesss/impartus-cli/commit/78c916b9d7136bb019e1110a3826a072b8c92745))
* **tui:** recover retry coordination after panic ([67e4bdd](https://github.com/rabesss/impartus-cli/commit/67e4bdd9554bb9d9b5cabf47cea7d948b0284849))
* **ui:** render unavailable catalog recovery ([9cc37ba](https://github.com/rabesss/impartus-cli/commit/9cc37bac116b8005a61460f4bd0c9e0bee77ec99))


### Documentation

* **api:** document job auth failure summary ([177f4ff](https://github.com/rabesss/impartus-cli/commit/177f4ff6d9836634a81b76e10d9e5960ed5c9f8c))
* **ci:** clarify historical package replays ([8c31552](https://github.com/rabesss/impartus-cli/commit/8c31552094934258a2bbec8f7cb5e88187fcad11))
* **cli:** clarify nested help identifiers ([01af298](https://github.com/rabesss/impartus-cli/commit/01af2985bbd3480e2ffd0a3bb91db40f0166d27d))
* **cli:** clarify root JSON help payload ([89223ed](https://github.com/rabesss/impartus-cli/commit/89223ed9cb300cede20eec5e97d0bf1d2ff0f8cf))
* **tui:** clarify recovery timing ([71a6224](https://github.com/rabesss/impartus-cli/commit/71a6224e3a40dca5d0e3ac790b08b5a5db9d5bbc))


### Testing

* **api:** compose chunk auth job sanitization ([80c6506](https://github.com/rabesss/impartus-cli/commit/80c6506581dfd8aefa3dbd361746a519be2c53a9))
* **api:** label persistent cleanup accurately ([cc4ee57](https://github.com/rabesss/impartus-cli/commit/cc4ee572a80f141c83beaf55449eb8808b0892d4))
* **api:** strengthen typed auth job coverage ([bbaa342](https://github.com/rabesss/impartus-cli/commit/bbaa34293fd2cdcf5a270e4c4a4eb3bb6e659285))
* **cli:** pin help surface contracts ([c5b9cfb](https://github.com/rabesss/impartus-cli/commit/c5b9cfbe28170bb0b2a58502e8ccf1c5d71042aa))
* **cli:** pin invalid parent help precedence ([7d6d255](https://github.com/rabesss/impartus-cli/commit/7d6d2557f635cca9fca2bf897187438171929a5f))
* **cli:** pin short JSON help aliases ([b4ac780](https://github.com/rabesss/impartus-cli/commit/b4ac7801d5ceb02f1ab4f17057c34c22ee76e2cd))
* **cli:** reconcile hosted help findings ([9dd765f](https://github.com/rabesss/impartus-cli/commit/9dd765f38486c32386b6e02d8f6e6059aeb688d6))
* **config:** isolate missing credential coverage ([b256ca2](https://github.com/rabesss/impartus-cli/commit/b256ca2abe4bd1bb38d8994cb3d76ce2e93a55b3))
* **downloader:** pin mixed auth precedence ([f50a0c6](https://github.com/rabesss/impartus-cli/commit/f50a0c699771a0f81fc01ad42575fac52703132c))

## [0.1.26](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.25...impartus-cli-v0.1.26) (2026-08-21)


### Features

* add exact lecture automation contracts ([dc231f0](https://github.com/rabesss/impartus-cli/commit/dc231f053fffcd47d92837a37db0a593b87671c0))
* add exact lecture selection and secure token caching ([0568403](https://github.com/rabesss/impartus-cli/commit/05684039f6eb66d696d6420d2de8003f90e79974))
* show lecture audio status in TUI ([e2616fa](https://github.com/rabesss/impartus-cli/commit/e2616fa31e05279bf3e19cb912e55c76400e092e))


### Bug Fixes

* harden cross-platform token caching ([4979ea9](https://github.com/rabesss/impartus-cli/commit/4979ea9153074d18ddb510e4d131fbcc959195d8))
* harden post-merge credential checks ([204083b](https://github.com/rabesss/impartus-cli/commit/204083baf62ca691f72e62022971526f03321534))
* harden post-merge credential checks ([56f0582](https://github.com/rabesss/impartus-cli/commit/56f0582f0514d9383f68e2c3a509daafc8624322))
* honor explicit false audio mode ([b2745e6](https://github.com/rabesss/impartus-cli/commit/b2745e6133012c1b4356426a1cef637e158a152a))
* keep audio badges terminal-safe ([3fab54f](https://github.com/rabesss/impartus-cli/commit/3fab54fb5beff65a086b607ce16d27cede9285d6))
* keep legacy token cache portable on Windows ([463e133](https://github.com/rabesss/impartus-cli/commit/463e1334b9a1c355591ff540589b7f62721451b1))
* preserve events after exact TTID selection ([91175a3](https://github.com/rabesss/impartus-cli/commit/91175a3c6d0ed2b18184f68f390ac1e6de1faf11))
* preserve portable token cache paths ([fbec741](https://github.com/rabesss/impartus-cli/commit/fbec7413cefcb2f8e075c3e7e5110f6c6bf974d5))


### Documentation

* align token cache validation comment ([b339369](https://github.com/rabesss/impartus-cli/commit/b339369f333cf5a07d0cd3e8f3d8a9092d51f506))


### Testing

* close PR 167 review gaps ([60f3aad](https://github.com/rabesss/impartus-cli/commit/60f3aad982a3ca8376bae2405cf5ae1f09ed411e))
* pin final review contracts ([2b11ea9](https://github.com/rabesss/impartus-cli/commit/2b11ea96c037e3e42441b078337d53b075099625))

## [0.1.25](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.24...impartus-cli-v0.1.25) (2026-08-17)


### CI/CD

* **deps:** bump docker/login-action from 4.4.0 to 4.6.0 ([#160](https://github.com/rabesss/impartus-cli/issues/160)) ([2dde818](https://github.com/rabesss/impartus-cli/commit/2dde8189e6dcd53d9329aa36a8ca1d3d2c1fe56a))
* **deps:** bump Factory-AI/droid-action ([#159](https://github.com/rabesss/impartus-cli/issues/159)) ([27509ab](https://github.com/rabesss/impartus-cli/commit/27509ab6d1445c28257e9c052b5bb30acf9424cf))
* **deps:** bump github/codeql-action/upload-sarif from 4.37.6 to 4.37.7 ([#161](https://github.com/rabesss/impartus-cli/issues/161)) ([a9dac05](https://github.com/rabesss/impartus-cli/commit/a9dac05c85177fa619d6309badeeacf376a596d7))
* inherit Pullfrog organization config ([8e71309](https://github.com/rabesss/impartus-cli/commit/8e713092ac698450b4bd5860e100d850a2969e4a))

## [0.1.24](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.23...impartus-cli-v0.1.24) (2026-08-16)


### Bug Fixes

* report unsupported media qualities safely ([#156](https://github.com/rabesss/impartus-cli/issues/156)) ([52955fe](https://github.com/rabesss/impartus-cli/commit/52955fea10642ef69e7682548e17b02cf5bc8207))

## [0.1.23](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.22...impartus-cli-v0.1.23) (2026-08-15)


### Bug Fixes

* **client:** explain unavailable media quality ([f5dbd8a](https://github.com/rabesss/impartus-cli/commit/f5dbd8a934c76a9bcd74c52c44cb7f2e07640728))
* **client:** preserve quality diagnostics across APIs ([8c5e5c2](https://github.com/rabesss/impartus-cli/commit/8c5e5c243b4006f9d02bf57c8de2da21dc2a9c45))
* **container:** build with Go 1.26.6 ([16f945d](https://github.com/rabesss/impartus-cli/commit/16f945d0e8eaa4c5493bea2dc2985534704b5385))
* improve media QA and container security ([f9d8d9c](https://github.com/rabesss/impartus-cli/commit/f9d8d9c0d62b6196fa5d53d11805e99bdb327a3a))


### Testing

* **player:** support fully headless mpv smoke ([e410c55](https://github.com/rabesss/impartus-cli/commit/e410c55f2136a81c7eef8c59d22111fc6bb2ba1d))

## [0.1.22](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.21...impartus-cli-v0.1.22) (2026-08-15)


### Features

* replace Bubble Tea with OpenTUI workspace ([068feef](https://github.com/rabesss/impartus-cli/commit/068feef6bdbd16c6540e48d6b377e2ac6635be24))
* **tui:** add live catalog and library workspace ([0675af1](https://github.com/rabesss/impartus-cli/commit/0675af1e490d2fc52f22ad8a151f3a43186ac4af))
* **tui:** add OpenTUI session foundation ([6e6ace6](https://github.com/rabesss/impartus-cli/commit/6e6ace6bf7f69d58e9f29ca2013e3ef3921fb4d1))
* **tui:** add playback and packaged launcher ([7f3b431](https://github.com/rabesss/impartus-cli/commit/7f3b431f2419d8878a12acfada27ae20657ef50c))


### Bug Fixes

* **cli:** cancel JSON downloads on interrupt ([9287b3b](https://github.com/rabesss/impartus-cli/commit/9287b3b75d647a9b7abc97b31e09985cecdca5ff))
* **cli:** clean download workspace on interrupt ([0645ad2](https://github.com/rabesss/impartus-cli/commit/0645ad2281223bc847e60ee97ad462a8df4e68be))
* **cli:** clean download workspace on interrupt ([cbf7cac](https://github.com/rabesss/impartus-cli/commit/cbf7cacbac4ecaa81dc57f2777fc1898962965a6))
* **tui:** create Windows bootstrap with ACL rights ([3dd454f](https://github.com/rabesss/impartus-cli/commit/3dd454fe5c7367cd9ae3061cd945ba2d9fa7ecd6))
* **tui:** distinguish shared course prefixes ([9f6f5a1](https://github.com/rabesss/impartus-cli/commit/9f6f5a162de122c33d9acd11c9554a86caf99bf7))


### Performance

* **tui:** cache course rail labels ([47d255c](https://github.com/rabesss/impartus-cli/commit/47d255cd2c3fa62885c4def42e00efc01530dd7d))


### Refactoring

* **tui:** remove legacy terminal frontend ([a9d5df2](https://github.com/rabesss/impartus-cli/commit/a9d5df242bc714c71deeb796f6596f366eb6cbdf))


### Documentation

* add download performance tuning guide ([#153](https://github.com/rabesss/impartus-cli/issues/153)) ([c5d5c98](https://github.com/rabesss/impartus-cli/commit/c5d5c98c71f2b17149550c227951df48ae4b62cb))


### CI/CD

* track Pullfrog v0 ([291cae1](https://github.com/rabesss/impartus-cli/commit/291cae18759cada9c0c71b9e2cf93421363fb51c))
* use Pullfrog custom API connection ([e8bdfc4](https://github.com/rabesss/impartus-cli/commit/e8bdfc4df3289c99219d3ae6e7c8d8a8258a2981))

## [0.1.21](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.20...impartus-cli-v0.1.21) (2026-08-12)


### Features

* **cli:** add Bubble Tea lecture workspace ([e29e6d8](https://github.com/rabesss/impartus-cli/commit/e29e6d8b1095feb0a4bab14dda76cb2776301fcd))
* **cli:** add Bubble Tea lecture workspace ([3b61e9e](https://github.com/rabesss/impartus-cli/commit/3b61e9ed7476e755c866a22f786ba5434a710847))
* **cli:** add generic durable lecture auto-download ([1c8cd29](https://github.com/rabesss/impartus-cli/commit/1c8cd290108ae0cfa39fb29e17648c8768768731))
* **cli:** add generic durable lecture auto-download ([20edc73](https://github.com/rabesss/impartus-cli/commit/20edc731828ec6bd1d2765b78dbd4baab49ec8e2))
* **cli:** add stable download artifact manifest v1 ([ca7d0f9](https://github.com/rabesss/impartus-cli/commit/ca7d0f9d1f7796ba7f12b3b8b7e88e724b267bb2))
* **cli:** add stable download artifact manifest v1 ([fd1706a](https://github.com/rabesss/impartus-cli/commit/fd1706af517f1c126be37000be3f0a3e5a04c93a))
* **cli:** persist artifacts, playback, and download jobs ([27741b5](https://github.com/rabesss/impartus-cli/commit/27741b5e8ad15afbd9ba9349a7d3595dbadcfded))
* **cli:** persist artifacts, playback, and download jobs ([7247710](https://github.com/rabesss/impartus-cli/commit/724771043cd7d2b73c0064066db02b9cfae36e40))
* **cli:** supervise mpv over private JSON IPC ([496c8ac](https://github.com/rabesss/impartus-cli/commit/496c8acf8c508012e92bd97c8812dedf04e2e16e))
* **cli:** supervise mpv over private JSON IPC ([4b08667](https://github.com/rabesss/impartus-cli/commit/4b0866733385c6e30ac467b9f24129c6440855e0))


### Bug Fixes

* **app:** await player result on cancellation ([99d1449](https://github.com/rabesss/impartus-cli/commit/99d1449f49612a23988ad4ecf7162f02aa8230d6))
* **app:** distinguish transport timeouts from cancellation ([6acfecc](https://github.com/rabesss/impartus-cli/commit/6acfeccf1a7805249f51bc3d81d7db207f943ba8))
* **app:** terminalize failed library commits ([119acd0](https://github.com/rabesss/impartus-cli/commit/119acd04fcf83f18d18985b837c2bb545ea7cf15))
* **artifact:** enforce manifest trust boundaries ([892152b](https://github.com/rabesss/impartus-cli/commit/892152b73fc793798fd4756b627cea96259acd0d))
* **artifact:** harden output descriptor opening ([a22895a](https://github.com/rabesss/impartus-cli/commit/a22895aaf87bde17b2f294f4c65dfd2a8e4b7b02))
* **artifact:** reject conflicting lecture scope ([535e45b](https://github.com/rabesss/impartus-cli/commit/535e45bae2557b39d7d2b912caf0eb3b4a5a808e))
* **artifact:** reject symlink materializations ([ade77dd](https://github.com/rabesss/impartus-cli/commit/ade77dd30f82e412965d6e3da0f5beb159d00dd1))
* **artifact:** resolve omitted institute scope ([282941f](https://github.com/rabesss/impartus-cli/commit/282941ff0da47dd6ac0ac05d6248194cfb6cf82b))
* **artifact:** revalidate manifest materializations ([6962a2f](https://github.com/rabesss/impartus-cli/commit/6962a2fc3b0c570bc6e9f33cd2c6babd34326e5b))
* **cli:** accept verify flags after artifact id ([ea10c31](https://github.com/rabesss/impartus-cli/commit/ea10c3172544f7a4108dca4d804f91299d1e777a))
* **cli:** preserve terminal outcome consistency ([4138d49](https://github.com/rabesss/impartus-cli/commit/4138d498d404c649e9b38bb509c516b8d4ebb0cb))
* **cli:** redact returned automation errors ([5f26c30](https://github.com/rabesss/impartus-cli/commit/5f26c30ef6341bffa86e2718b5c1d4b701bd416c))
* **cli:** remove stale download import ([2971f07](https://github.com/rabesss/impartus-cli/commit/2971f0746c9e18b97152fc6fa46999fb4dcbc075))
* **cli:** remove stale event imports ([c8e6bfc](https://github.com/rabesss/impartus-cli/commit/c8e6bfc2fb6d0f18f7ac4b45d9b9cad940954594))
* **cli:** share interactive lecture scope resolution ([0a26717](https://github.com/rabesss/impartus-cli/commit/0a2671797d5358ca6c3dccbbbec2fab3c3a14091))
* **cli:** skip empty library commits ([5d69039](https://github.com/rabesss/impartus-cli/commit/5d690397c12ca45a4320d7816e10daa9afe30d16))
* **cli:** skip playlists without selected media ([8561009](https://github.com/rabesss/impartus-cli/commit/85610092455dd7e74970fca0054ea5ade5f469e5))
* **config:** allow pipeline env opt-out ([15d2014](https://github.com/rabesss/impartus-cli/commit/15d2014da1c9e77a227c977705e8dd1788acfd8d))
* **config:** canonicalize validated views ([66a4529](https://github.com/rabesss/impartus-cli/commit/66a4529f0f1253d5e5744b7b823d72a9866b02a5))
* **config:** reject noncanonical media values ([75bef0b](https://github.com/rabesss/impartus-cli/commit/75bef0b81c4ac24a37916c0c91ce6499766a64aa))
* **doctor:** honor Windows permission semantics ([499a9a8](https://github.com/rabesss/impartus-cli/commit/499a9a8982489d1ea8ddc6c84b6eb7623462b501))
* **doctor:** reject ACL ownership controls ([06ef296](https://github.com/rabesss/impartus-cli/commit/06ef29668bc394ba9d1fdc29ad26cacaac479ca4))
* **doctor:** verify private state ownership ([a6febee](https://github.com/rabesss/impartus-cli/commit/a6febee8a52a94761f5beb5774b78cbd69b74bd3))
* **downloader:** attempt downloads with unset retry limit ([9688ad4](https://github.com/rabesss/impartus-cli/commit/9688ad446e50e08c2dcb01f52dec49a602b17b75))
* **downloader:** plan single-camera downloads ([0c06aba](https://github.com/rabesss/impartus-cli/commit/0c06abab72982ece8ba63282fd4947f76df5e37f))
* **downloader:** preserve cancellation and redaction safety ([d799df1](https://github.com/rabesss/impartus-cli/commit/d799df1e61c3f930467f102c4f3794d81beb9466))
* **downloader:** preserve cancellation diagnostics ([ce771bd](https://github.com/rabesss/impartus-cli/commit/ce771bd8131581519c7fd84515bc359874c8db9f))
* **downloader:** preserve completed pipeline results ([636f254](https://github.com/rabesss/impartus-cli/commit/636f25440007c689bb73151a17d62e7e4ba059f8))
* **downloader:** reject empty media batches ([fbbfb20](https://github.com/rabesss/impartus-cli/commit/fbbfb208cbb4729b8a2b629b3cb65dd93300cb5e))
* **downloader:** reject incomplete pipeline results ([e700cb4](https://github.com/rabesss/impartus-cli/commit/e700cb42d34a743998e0933e166c65f9f3b15200))
* **downloader:** reject unavailable selected media ([6319ff9](https://github.com/rabesss/impartus-cli/commit/6319ff9dad7ad1935411917f03f9b8faafd15e47))
* **downloader:** retain pipeline failure details ([6cc1422](https://github.com/rabesss/impartus-cli/commit/6cc1422c312a13b44849a3b8b89ce84777d1470a))
* **downloader:** scrub response bodies at source ([780794e](https://github.com/rabesss/impartus-cli/commit/780794e68a39d3a7da3a3fb3cb3a705f1ea69bd3))
* **downloader:** scrub upstream response secrets ([db6a1e5](https://github.com/rabesss/impartus-cli/commit/db6a1e53aaec8addd253fec8419350ed206c956b))
* **downloader:** stabilize pipeline failure handling ([4f547bf](https://github.com/rabesss/impartus-cli/commit/4f547bfa22863203060e0f29a5e4b403a613eb33))
* **download:** preserve partial and playback compatibility ([3042ebd](https://github.com/rabesss/impartus-cli/commit/3042ebdf24d4c1f94dd19ea16e679b08b8dd2c46))
* **download:** reject empty playlist batches ([996a491](https://github.com/rabesss/impartus-cli/commit/996a491aad4116248d43b4f171807b3125f4de21))
* **download:** surface incomplete lecture batches ([c3095a8](https://github.com/rabesss/impartus-cli/commit/c3095a86e25a974bb545a59cc28f48ad81a3c2f2))
* **events:** avoid nonterminal lecture failures ([d7bd999](https://github.com/rabesss/impartus-cli/commit/d7bd999f0bf56697025bab6527609d41c3f9d22b))
* **events:** close unavailable lecture lifecycle ([14cb551](https://github.com/rabesss/impartus-cli/commit/14cb551ec78091a14586d483490bf698eb9eca2c))
* **events:** distinguish transport timeouts from cancellation ([5e1f1bd](https://github.com/rabesss/impartus-cli/commit/5e1f1bd25dff9ae4b9185cf6b8cd5afcb2b97eee))
* **events:** fail closed on library commit errors ([8ce1eb5](https://github.com/rabesss/impartus-cli/commit/8ce1eb566bf45837465efab54f6c97d7df055942))
* **events:** keep lifecycle v1 to approved names ([667ad9c](https://github.com/rabesss/impartus-cli/commit/667ad9cdbbab973c9926b13111cb9be4f312b8de))
* **events:** preserve unavailable media lifecycle ([1a1dc30](https://github.com/rabesss/impartus-cli/commit/1a1dc308176f8fab36595ae0ff8d0624953efb76))
* **events:** restore original lifecycle contract ([061a27e](https://github.com/rabesss/impartus-cli/commit/061a27e95faf437dd00b9587b39a65021d5a9fee))
* **events:** sever secret-bearing error chains ([4a10302](https://github.com/rabesss/impartus-cli/commit/4a10302d837d3bdb5427173565e95048e544ebd6))
* **library:** canonicalize durable video selections ([d56e9c3](https://github.com/rabesss/impartus-cli/commit/d56e9c3e7080fab62ad50ff0023ff06728ff15b1))
* **library:** canonicalize expected file views ([3212334](https://github.com/rabesss/impartus-cli/commit/3212334832434862e2cd4db8252a62bf9eb124cb))
* **library:** clear stale hashes on re-record ([1a0469b](https://github.com/rabesss/impartus-cli/commit/1a0469bbcb7e5500196d40c7492e6cc5c1a23fe4))
* **library:** encode Windows SQLite file URIs ([8a135a2](https://github.com/rabesss/impartus-cli/commit/8a135a2ce560e2788c5735c5cc08838682ed769e))
* **library:** harden ACL and file verification ([e6114b2](https://github.com/rabesss/impartus-cli/commit/e6114b2968d213c7224f347cc51baa5276acad8f))
* **library:** harden final artifact commits ([9e617db](https://github.com/rabesss/impartus-cli/commit/9e617db171e53f654a78bc42f9028bd6b20f4ce6))
* **library:** harden private error persistence ([a624f05](https://github.com/rabesss/impartus-cli/commit/a624f052f5384d81293c1941d39473d76cfead13))
* **library:** scope recovery and hash publication ([b7309c3](https://github.com/rabesss/impartus-cli/commit/b7309c3f6ebf0862126cc525a822b83130528076))
* **library:** tolerate concurrent ACL setup ([65ab367](https://github.com/rabesss/impartus-cli/commit/65ab367ad899487a839427994da8913fa00260c3))
* **library:** validate recovered media containers ([c4942f4](https://github.com/rabesss/impartus-cli/commit/c4942f4387e2fbbd403f55a514db6b1445074b75))
* **library:** validate recovered media on one descriptor ([ea6d4b7](https://github.com/rabesss/impartus-cli/commit/ea6d4b7925e30910fdd06c748af997844a45de6d))
* **playback:** require verified terminal events ([6781c81](https://github.com/rabesss/impartus-cli/commit/6781c81f3fe436965947c1cc1d99c524cf8f5ace))
* **playback:** surface proxy authorization failures ([fbb35b9](https://github.com/rabesss/impartus-cli/commit/fbb35b95571dfa045f7b5357e80ad85ea16f2e35))
* **playback:** verify Windows privacy and auth failures ([1eae540](https://github.com/rabesss/impartus-cli/commit/1eae54062bfd277988450063ce1be2ffdfa47011))
* **player:** close IPC before classifying cancellation ([b7974ae](https://github.com/rabesss/impartus-cli/commit/b7974aefc108d46c878528e1f62d686bd0c2e961))
* **player:** drain in-flight terminal handlers ([f5639f0](https://github.com/rabesss/impartus-cli/commit/f5639f0fa8161f65f9f66ad239bd77a2816da3f3))
* **player:** drain terminal failures before cancellation ([8d199b3](https://github.com/rabesss/impartus-cli/commit/8d199b3e3dc236b39b275cc090339157696435ac))
* **player:** harden terminal event handling ([0e60977](https://github.com/rabesss/impartus-cli/commit/0e60977ac0a40f82aebb56007a75bfc09cce421f))
* **player:** hide unverified eof events ([0d05634](https://github.com/rabesss/impartus-cli/commit/0d056343e985cd6e445159e2ac537e052ccc3368))
* **player:** ignore idle eof notifications ([7e650fc](https://github.com/rabesss/impartus-cli/commit/7e650fca351b2a6ccdae98123c6d7f94434e4865))
* **player:** keep platform defaults portable ([e340df0](https://github.com/rabesss/impartus-cli/commit/e340df0c1e27fcd6f6d2b0056baab8da69308b86))
* **player:** preserve cancellation over clean quit ([580a548](https://github.com/rabesss/impartus-cli/commit/580a54821314a961b44c59ba9023efc8b0e1de77))
* **player:** preserve ready failures on cancellation ([fd2541a](https://github.com/rabesss/impartus-cli/commit/fd2541a632cda197d20eacf5a83bb1319b4b2b8b))
* **player:** retain terminal state after disconnect ([cdbce3c](https://github.com/rabesss/impartus-cli/commit/cdbce3c74028cd3acf975141adb5ec599871bcb3))
* **player:** surface safe playback failures ([3417fdd](https://github.com/rabesss/impartus-cli/commit/3417fdd6b935a5962e1e5ac6c04c16ca1834f36d))
* **secrets:** cover signed response credentials ([eb27aca](https://github.com/rabesss/impartus-cli/commit/eb27acace7f0f6e54ab1d543b37c355fc25a23b3))
* **secrets:** preserve redacted assignment context ([4e6ad07](https://github.com/rabesss/impartus-cli/commit/4e6ad0717130d14d1084a72a5cc3df812b148729))
* **secrets:** redact arbitrary authorization schemes ([0742e86](https://github.com/rabesss/impartus-cli/commit/0742e86181636cbb19d096d12054ab2205cbd31c))
* **secrets:** redact arbitrary authorization schemes ([e65b41c](https://github.com/rabesss/impartus-cli/commit/e65b41c148927c4afb76a46ba136e42d5b753e62))
* **secrets:** redact bare inline tokens ([5511b4e](https://github.com/rabesss/impartus-cli/commit/5511b4ea34e54709cc7790f33f6642ba91cae5cf))
* **secrets:** redact inline password values ([86723bf](https://github.com/rabesss/impartus-cli/commit/86723bf027e99b9a7f58040a82e4c9e998dea89f))
* **secrets:** redact short inline credentials ([8f9cbd4](https://github.com/rabesss/impartus-cli/commit/8f9cbd44c5f56f388ae919edd633229cfac25801))
* **secrets:** redact standalone key assignments ([8cece66](https://github.com/rabesss/impartus-cli/commit/8cece66c4ecc5ca932cbf2ea32a6fa8048a12a88))
* **secrets:** redact strong credential headers ([8595b97](https://github.com/rabesss/impartus-cli/commit/8595b97d01e6e78445b2d4fb754e46d9d9e34b3a))
* **security:** refresh vulnerable runtime base ([ff194bf](https://github.com/rabesss/impartus-cli/commit/ff194bfc2467fce20dfad55611ce3bc75f3d9e3d))
* **security:** refresh vulnerable runtime base ([57594f1](https://github.com/rabesss/impartus-cli/commit/57594f1ea306365428050094d9ea265b00ed10ea))
* **server:** skip unavailable selected media ([757c3f0](https://github.com/rabesss/impartus-cli/commit/757c3f09303c52d596961828e0f3ad852c11d3c6))
* **tui:** align planned outputs and retry help ([7cb9977](https://github.com/rabesss/impartus-cli/commit/7cb9977ded47df43fc6e0116b3c465156196acf2))
* **tui:** bound resume readiness wait ([02da028](https://github.com/rabesss/impartus-cli/commit/02da028ad2837a81065ff2766a8e35d9065b738c))
* **tui:** format playback clocks with hours ([2d190f2](https://github.com/rabesss/impartus-cli/commit/2d190f26092b8701284c8735b143797a77770135))
* **tui:** hand off bounded resume telemetry ([c94fc8d](https://github.com/rabesss/impartus-cli/commit/c94fc8d92060085c242399c621bc88815533be64))
* **tui:** harden playback lifecycle ([638e321](https://github.com/rabesss/impartus-cli/commit/638e321b4aba336e6cf8668681f48e4c1ea17d75))
* **tui:** harden playback state transitions ([280c347](https://github.com/rabesss/impartus-cli/commit/280c34743a88f094efc178b446d255c02cf0cb4c))
* **tui:** persist durable download jobs ([511332a](https://github.com/rabesss/impartus-cli/commit/511332a39e908e1ba91bb10e727a416974bfad47))
* **tui:** preserve terminal readiness failures ([e6380a6](https://github.com/rabesss/impartus-cli/commit/e6380a6296d0c06d0ba24f4959f06337a3004423))
* **tui:** redact backend errors at render boundary ([040a332](https://github.com/rabesss/impartus-cli/commit/040a33259db101ea5a10b7b6c52e14be1d32e4bf))
* **tui:** replay resume readiness events ([9440eca](https://github.com/rabesss/impartus-cli/commit/9440ecac6ce4e18a984809fe2a775102b7914d9f))
* **tui:** sanitize terminal-bound metadata ([88e071a](https://github.com/rabesss/impartus-cli/commit/88e071a9f516aebe4cc947f8222824acccdfccc7))
* **watch:** align filtering and durable progress ([9daa29a](https://github.com/rabesss/impartus-cli/commit/9daa29af0dbd4c31e2767969fcdca929c30c1f70))
* **watch:** align redaction and budget docs ([00216db](https://github.com/rabesss/impartus-cli/commit/00216dba2a5e595c4c4bc39a6565c6cf6597a276))
* **watch:** classify empty filtered selections ([3fbf6bf](https://github.com/rabesss/impartus-cli/commit/3fbf6bfa64498b23f09dbdcb1f937676d0993862))
* **watch:** close final review gaps ([4d2dcf3](https://github.com/rabesss/impartus-cli/commit/4d2dcf3452730542d6452ec4f35fa7d24c8efeb5))
* **watch:** consolidate scope and recovery paths ([712009e](https://github.com/rabesss/impartus-cli/commit/712009e2346d79c722d748a5c60de913267a9887))
* **watch:** enforce budget before playlist fetch ([4107ea0](https://github.com/rabesss/impartus-cli/commit/4107ea0b16bc5656898fe052440f4d9221750252))
* **watch:** enforce forced one-cycle runs ([23aa68d](https://github.com/rabesss/impartus-cli/commit/23aa68d82de5518f2ba9df22a793305284e48954))
* **watch:** harden review edge cases ([1523332](https://github.com/rabesss/impartus-cli/commit/1523332564546330bfec3ae7c88ad33978968f63))
* **watch:** preserve cancellation lifecycle semantics ([9297435](https://github.com/rabesss/impartus-cli/commit/92974355b1050dfcf3cff06ac4e73db29ddbf329))
* **watch:** preserve recovered artifact events ([d704da8](https://github.com/rabesss/impartus-cli/commit/d704da8c37adcf80c05e4ee8a080e9933e0cec09))
* **watch:** print forced cycle summary ([6b56d58](https://github.com/rabesss/impartus-cli/commit/6b56d5873a7f41dabffa28e5b99f61757694a287))
* **watch:** recover only watcher-owned jobs ([40d303e](https://github.com/rabesss/impartus-cli/commit/40d303efc1fc9301c356212f56a8b657e10491da))
* **watch:** retain partial terminal results ([06b52c9](https://github.com/rabesss/impartus-cli/commit/06b52c9055ed91752be3f3d8766b72abe46cd7e5))


### Performance

* **downloader:** enable bounded pipeline by default ([8899e56](https://github.com/rabesss/impartus-cli/commit/8899e56b24d868a5c03edd22eba934cbfc8fbdc8))
* **downloader:** enable bounded pipeline by default ([e2e7db7](https://github.com/rabesss/impartus-cli/commit/e2e7db78e562f73e513f51b08fa282f0888e8e4d))


### Refactoring

* **app:** narrow durable library boundary ([31ffdb5](https://github.com/rabesss/impartus-cli/commit/31ffdb5ff9b78976581802c9354b78aa30d64b5c))
* **artifact:** keep manifest boundary independent ([225602f](https://github.com/rabesss/impartus-cli/commit/225602feedf11cdbd5e7ec951fea880d19b80ef1))
* **artifact:** split stable file validation ([5352383](https://github.com/rabesss/impartus-cli/commit/53523838ea4596d44d0c128560776112fcfb791a))
* **cli:** split download presentation concerns ([476e339](https://github.com/rabesss/impartus-cli/commit/476e339fb227240061fd24f8aed56625c2e97692))
* **config:** centralize artifact view selection ([b8c703b](https://github.com/rabesss/impartus-cli/commit/b8c703bd4c5978157ff557f37a99d1b5b0e268ee))
* **downloader:** type planned output views ([ad0b325](https://github.com/rabesss/impartus-cli/commit/ad0b3255982dc9b578a0ecb7e47184f3e0b85ff1))
* **library:** reuse selection normalization ([5810b82](https://github.com/rabesss/impartus-cli/commit/5810b8223e7630456ed159b370b364f4903600c3))
* **library:** share artifact view validation ([ae333ce](https://github.com/rabesss/impartus-cli/commit/ae333ceb111833dd62aaa25b831a7ecbe18e1520))
* **library:** split container signature validation ([5e07820](https://github.com/rabesss/impartus-cli/commit/5e0782014957f78bc63cea16a9e47387a7fd1bd6))
* **player:** share playback error boundary ([725ca13](https://github.com/rabesss/impartus-cli/commit/725ca1363b57457d3a440bbc7b9f8292f2476cdf))
* **selection:** centralize artifact vocabulary ([13937a9](https://github.com/rabesss/impartus-cli/commit/13937a920d10bd25dbb23dfaea01560021303559))
* **selection:** unify view membership policy ([8f1c29c](https://github.com/rabesss/impartus-cli/commit/8f1c29cb0f51f773f8dd1fc8452056471fe725f1))


### Documentation

* close architecture review gaps ([0ee8ebf](https://github.com/rabesss/impartus-cli/commit/0ee8ebf8c2fdc0806b6bb8ef3a478d1ecf04b9cc))
* **downloader:** clarify cancellation precedence ([f493908](https://github.com/rabesss/impartus-cli/commit/f493908a211ca307f508ed65f33d90c77264da2d))
* pin remaining execution contracts ([0f50ab8](https://github.com/rabesss/impartus-cli/commit/0f50ab85239ad978997150804b8aeaf530cd37ba))
* plan consolidated lecture product ([489d330](https://github.com/rabesss/impartus-cli/commit/489d3309b4cd657523aad5c2803f5d362b810b12))
* remove unsupported OpenClaw manifest ([#133](https://github.com/rabesss/impartus-cli/issues/133)) ([70401a1](https://github.com/rabesss/impartus-cli/commit/70401a18765dcadb1cdba9201863564abd13014c))


### Testing

* **cli:** avoid verify result shadowing ([c5f2326](https://github.com/rabesss/impartus-cli/commit/c5f2326c37b4ada753b363c689ef4e51bc131325))
* **cli:** satisfy interactive scope lint checks ([8fca189](https://github.com/rabesss/impartus-cli/commit/8fca189143c967cff391e1faa73ac4fb47d03e7a))
* **config:** isolate env-sensitive defaults ([a498611](https://github.com/rabesss/impartus-cli/commit/a49861196372366a6ec460047fff99e45c282216))
* **doctor:** provide stacked library check ([770da54](https://github.com/rabesss/impartus-cli/commit/770da54bee225b8aac721fa7a12006dc1b5e5f28))
* **events:** cover strong credential headers ([f52c0fa](https://github.com/rabesss/impartus-cli/commit/f52c0faaa7e4e4a2b3575ee5e7e9ba4baba71913))
* **library:** cover strong credential persistence ([d8e9391](https://github.com/rabesss/impartus-cli/commit/d8e9391f59d51a3c6bf8bf4e2e913c08167522b9))
* **player:** cover unexpected terminal reasons ([051126a](https://github.com/rabesss/impartus-cli/commit/051126af3ed804c38dde524086c935e6fed7a346))
* **player:** synchronize cancellation after launch ([29e281c](https://github.com/rabesss/impartus-cli/commit/29e281ce6013b93c02f95f767a75e69fdce78dc8))
* **tui:** cover digest authorization redaction ([707091b](https://github.com/rabesss/impartus-cli/commit/707091b4b842867415984ee3283d7129fa861781))
* **tui:** cover strong credential headers ([20a7275](https://github.com/rabesss/impartus-cli/commit/20a7275a0dcdcb7b708ea37ed7d026f315a844c2))
* **watch:** avoid retryable job shadowing ([60179c7](https://github.com/rabesss/impartus-cli/commit/60179c7f88537193d584892e794e3ab806c21538))
* **watch:** cover arbitrary authorization schemes ([b96461a](https://github.com/rabesss/impartus-cli/commit/b96461ab79733fa7753949d579641dd27bb8b8ea))
* **watch:** use valid recovered media fixtures ([6062b08](https://github.com/rabesss/impartus-cli/commit/6062b08263ee2979936e2a91c8002aff33adde85))

## [0.1.20](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.19...impartus-cli-v0.1.20) (2026-07-13)


### Bug Fixes

* apply http and progress configuration ([#95](https://github.com/rabesss/impartus-cli/issues/95)) ([5e40278](https://github.com/rabesss/impartus-cli/commit/5e402789b7d0d611c07dd152a00a3a9c7a421581))
* apply http and progress configuration ([#95](https://github.com/rabesss/impartus-cli/issues/95)) ([2ee3dac](https://github.com/rabesss/impartus-cli/commit/2ee3dac61151bea10a919a0342e0c4f04bcd3e29))
* bound and normalize upstream media ingestion ([e34d79a](https://github.com/rabesss/impartus-cli/commit/e34d79acf098fcf92228379929aa6ece72f9a66b))
* bound readiness panic recovery ([1287ad7](https://github.com/rabesss/impartus-cli/commit/1287ad7495f4d6c2541e851d7749843801bca8ac))
* bound websocket clients and sanitize job panics ([8e42d6f](https://github.com/rabesss/impartus-cli/commit/8e42d6fb6a776b6e0e85eb471155e1b6ba30e922))
* bound WebSocket delivery and preserve terminal events ([7c77c13](https://github.com/rabesss/impartus-cli/commit/7c77c1371edac683d5e835d3763e9d1babeb6e58))
* **ci:** enforce quality and coverage thresholds ([#99](https://github.com/rabesss/impartus-cli/issues/99)) ([#131](https://github.com/rabesss/impartus-cli/issues/131)) ([ead5d5d](https://github.com/rabesss/impartus-cli/commit/ead5d5d0b31a0300d6a005ada570c4821554dbf5))
* close progress configuration review gaps ([25ea373](https://github.com/rabesss/impartus-cli/commit/25ea373c4d5410b0dd01f6d25a4f9137dcf75479))
* detach shared readiness refresh ([caab23e](https://github.com/rabesss/impartus-cli/commit/caab23efb2fe5f032fb128fb0fcaf6dfe22e478c))
* distinguish HLS discontinuity tags ([17ee5d7](https://github.com/rabesss/impartus-cli/commit/17ee5d7f480cc833b238f1d5ba03bae73004cbd2))
* harden readiness cleanup ([8b4efee](https://github.com/rabesss/impartus-cli/commit/8b4efee79e877c407312ea10c5ef54e9acd0350f))
* ignore HLS comment lines during parsing ([c84c7b8](https://github.com/rabesss/impartus-cli/commit/c84c7b87e811f73be5b4caf6b1415f8fd329bce4))
* isolate downloader diagnostics per instance ([0d1f13d](https://github.com/rabesss/impartus-cli/commit/0d1f13d7ec16425c2dbaf4deb56a788ab25fa40d))
* isolate json download diagnostics ([351dc28](https://github.com/rabesss/impartus-cli/commit/351dc281ece79d574262b2865d901e170732b717))
* isolate temporary workspaces ([#92](https://github.com/rabesss/impartus-cli/issues/92)) ([#105](https://github.com/rabesss/impartus-cli/issues/105)) ([e9f8e60](https://github.com/rabesss/impartus-cli/commit/e9f8e60e9b7808944ffa7ce6bb27c382b1916c96))
* keep json downloads machine readable ([06951e0](https://github.com/rabesss/impartus-cli/commit/06951e0bc6b981d2082976dc8349fad7f7b3ee0c))
* keep json downloads machine readable ([47382f4](https://github.com/rabesss/impartus-cli/commit/47382f44b7d2c1f7983af03d53a42c0021c2e851)), closes [#94](https://github.com/rabesss/impartus-cli/issues/94)
* keep panic cache on health clock ([6ba91dc](https://github.com/rabesss/impartus-cli/commit/6ba91dce5a7dbdbda321bff2ee7b364ba244dab2))
* make container workdir writable ([#93](https://github.com/rabesss/impartus-cli/issues/93)) ([#104](https://github.com/rabesss/impartus-cli/issues/104)) ([c962492](https://github.com/rabesss/impartus-cli/commit/c962492772790aa94507ec610ab079ef70e36e03))
* make sample config valid and sync docs ([#96](https://github.com/rabesss/impartus-cli/issues/96)) ([ba46311](https://github.com/rabesss/impartus-cli/commit/ba463116e3ea961c81a3bc2080620351b1ab0763))
* make sample config valid and sync docs ([#96](https://github.com/rabesss/impartus-cli/issues/96)) ([24642f0](https://github.com/rabesss/impartus-cli/commit/24642f0934febceb71211e1d85b0aa3aca00e96f))
* normalize and bound media ingestion ([1648164](https://github.com/rabesss/impartus-cli/commit/16481644c0ae9c7781b76d40895f6fb817109185))
* preserve final slide permissions ([18c842e](https://github.com/rabesss/impartus-cli/commit/18c842e1c532fcefb54eab15d907b30bfed6739d))
* preserve terminal and websocket event ordering ([bbbebde](https://github.com/rabesss/impartus-cli/commit/bbbebde0b688cfe01fbdf51063ad2478b98950ae))
* propagate config setup failures ([848e4e4](https://github.com/rabesss/impartus-cli/commit/848e4e49ee6f71b5b9701ef4823825e1ec13d2e5))
* scan container before publishing ([#98](https://github.com/rabesss/impartus-cli/issues/98)) ([#107](https://github.com/rabesss/impartus-cli/issues/107)) ([cb00fb0](https://github.com/rabesss/impartus-cli/commit/cb00fb0be56c6fc4374ef82b870b0c54843cbbc3))
* **server:** own cleanup lifecycle in Start ([#122](https://github.com/rabesss/impartus-cli/issues/122)) ([#129](https://github.com/rabesss/impartus-cli/issues/129)) ([649462d](https://github.com/rabesss/impartus-cli/commit/649462da99fb13dc0ce5dcce74a480887e37d652))


### Performance

* cache dependency readiness probes ([#100](https://github.com/rabesss/impartus-cli/issues/100)) ([7859778](https://github.com/rabesss/impartus-cli/commit/78597788dfd75bc63b2b958a5dd4f9a6aed39fd8))
* cache dependency readiness probes ([#100](https://github.com/rabesss/impartus-cli/issues/100)) ([1d008e1](https://github.com/rabesss/impartus-cli/commit/1d008e145021d4070c82432e968933651745486c))
* coalesce job persistence writes ([#111](https://github.com/rabesss/impartus-cli/issues/111)) ([fd4167c](https://github.com/rabesss/impartus-cli/commit/fd4167c4931076c095eca78b48b34c98bb46d47f))


### Refactoring

* split package ownership seams ([#124](https://github.com/rabesss/impartus-cli/issues/124)) ([#130](https://github.com/rabesss/impartus-cli/issues/130)) ([7dc4a79](https://github.com/rabesss/impartus-cli/commit/7dc4a792cabc218b2276b26c43bf28af18ef8dd2))


### Documentation

* document opaque UUID-backed job IDs ([#125](https://github.com/rabesss/impartus-cli/issues/125)) ([#127](https://github.com/rabesss/impartus-cli/issues/127)) ([6ab2eac](https://github.com/rabesss/impartus-cli/commit/6ab2eacffbc47514428b4f1c11a567cf03456c40))
* simplify README development guidance ([#110](https://github.com/rabesss/impartus-cli/issues/110))
* remove retired Go Report Card badge ([b67af49](https://github.com/rabesss/impartus-cli/commit/b67af4981431865000ca7654063ada376f8b6cb4))
* remove retired Go Report Card badge ([7470ebc](https://github.com/rabesss/impartus-cli/commit/7470ebc7e19c0241255ada80bd1acd1fbd059cbf))


### Testing

* cover focused startup and selection gaps ([#123](https://github.com/rabesss/impartus-cli/issues/123)) ([#128](https://github.com/rabesss/impartus-cli/issues/128)) ([48df2e4](https://github.com/rabesss/impartus-cli/commit/48df2e4a898cae5f6deb68eb24972c858cc4799d))
* drain stdout capture concurrently ([d716c18](https://github.com/rabesss/impartus-cli/commit/d716c180a577ff343cc76cd62b73255c8ed09d9e))
* load sample config from package directory ([373a516](https://github.com/rabesss/impartus-cli/commit/373a5169b82f569953784de937034ff3fc089ae6))


### Build System

* update Go builder to 1.26.5 ([#108](https://github.com/rabesss/impartus-cli/issues/108)) ([#109](https://github.com/rabesss/impartus-cli/issues/109)) ([6cc925d](https://github.com/rabesss/impartus-cli/commit/6cc925defc92eed24cac8acd981a80dc9183ceb0))


### CI/CD

* **deps:** bump pullfrog/pullfrog to 0.1.35 ([#101](https://github.com/rabesss/impartus-cli/issues/101))
* **deps:** bump github/codeql-action/upload-sarif to 4.37.0 ([#102](https://github.com/rabesss/impartus-cli/issues/102))
* **deps:** bump actions/setup-python to 6.3.0 ([#103](https://github.com/rabesss/impartus-cli/issues/103))

## [0.1.19](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.18...impartus-cli-v0.1.19) (2026-07-12)


### Bug Fixes

* remediate all v0.1.18 code review findings (25 fixes) ([#91](https://github.com/rabesss/impartus-cli/issues/91)) ([e465c3d](https://github.com/rabesss/impartus-cli/commit/e465c3d7d088fc58e3a439e78ce4b99a229bac71))


### Documentation

* add missing [#88](https://github.com/rabesss/impartus-cli/issues/88) codeql-action bump to 0.1.18 changelog ([bef5adf](https://github.com/rabesss/impartus-cli/commit/bef5adf0bc1b21ab3a2bf6f94306e7abb8e4ced4))

## [0.1.18](https://github.com/rabesss/impartus-cli/compare/impartus-cli-v0.1.17...impartus-cli-v0.1.18) (2026-07-12)


### Features

* Add droid-review.yml workflow ([ebbb6c3](https://github.com/rabesss/impartus-cli/commit/ebbb6c3db2e78d6f5534aa950d487070ef61f290))
* Add droid.yml workflow ([6556f69](https://github.com/rabesss/impartus-cli/commit/6556f695ea831a9892761fe3fa80d8f4a5cee6e2))


### Bug Fixes

* remediate security and quality review findings (P0 token leak, path traversal, network exposure) ([#83](https://github.com/rabesss/impartus-cli/issues/83)) ([b4ebcde](https://github.com/rabesss/impartus-cli/commit/b4ebcde24ccff870adbcae2dcef0070a8dddb2a1))


### CI/CD

* add ZAI Coding Plan OpenCode config ([#81](https://github.com/rabesss/impartus-cli/issues/81)) ([0c9f408](https://github.com/rabesss/impartus-cli/commit/0c9f4082a61155dd34bed4964d130a6247709b1d))
* **deps:** bump actions/checkout from 6 to 7 ([#87](https://github.com/rabesss/impartus-cli/issues/87)) ([dd754e5](https://github.com/rabesss/impartus-cli/commit/dd754e55c6ac6bdd44bda73c11b3c18aebbbf451))
* **deps:** bump github/codeql-action/upload-sarif from 4.36.2 to 4.36.3 ([#88](https://github.com/rabesss/impartus-cli/issues/88)) ([d860e62](https://github.com/rabesss/impartus-cli/commit/d860e627302f040d77ef62f9549e3b314532faeb))
* **deps:** bump golangci/golangci-lint-action from 9.2.1 to 9.3.0 ([#86](https://github.com/rabesss/impartus-cli/issues/86)) ([8abf4c4](https://github.com/rabesss/impartus-cli/commit/8abf4c4171243f589a918e09052e5ece925a6698))
* finalize AI review configuration ([83a9e71](https://github.com/rabesss/impartus-cli/commit/83a9e71cf50c21517a94b6fc12191470fa56dff2))
* update AI review configuration ([c1c9f9e](https://github.com/rabesss/impartus-cli/commit/c1c9f9e0576c5af38e7e8fba401b1472eb365e73))

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
