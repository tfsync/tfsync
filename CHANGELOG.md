# Changelog

## [0.5.0](https://github.com/tfsync/tfsync/compare/v0.4.0...v0.5.0) (2026-04-24)


### Features

* **controller:** stream runner pod logs into status.lastPlanOutput ([77b767c](https://github.com/tfsync/tfsync/commit/77b767c5ce91ae0c87c0007cd92f061c4957676a))


### Bug Fixes

* **provider:** scope config map keys to path subdir, not clone root ([fa6a505](https://github.com/tfsync/tfsync/commit/fa6a505d307b2e146f1cdb3b77cdbac7c1e7f582))

## [0.4.0](https://github.com/tfsync/tfsync/compare/v0.3.0...v0.4.0) (2026-04-20)


### Features

* **provider:** implement S3 state backend; stub GCS, azurerm, HTTP ([a997e32](https://github.com/tfsync/tfsync/commit/a997e327fdbe12cf5b75918353a6ea62c49b3ab0))

## [0.3.0](https://github.com/tfsync/tfsync/compare/v0.2.0...v0.3.0) (2026-04-20)


### Features

* **api:** make credentials optional ([40f61ba](https://github.com/tfsync/tfsync/commit/40f61ba887b386a734ae7d30049143f0a930b889))
* **provider:** pluggable GitProvider/SecretProvider/StateBackend interfaces ([4596280](https://github.com/tfsync/tfsync/commit/459628028a3a9a18c7e75d9b972a211e620124b0))


### Bug Fixes

* **controller:** eliminate reconcile storm from self-triggered status writes ([2d0c70b](https://github.com/tfsync/tfsync/commit/2d0c70b6ac85dcd43c5f66166db07279c3aedd72))
* **controller:** include all status mutations in setPhase patch ([a5a1b50](https://github.com/tfsync/tfsync/commit/a5a1b50fd5845415d4b4d2dd0642c30a6713b3ae))
* **runner:** mount emptyDir for writable workspace ([f5c9a4a](https://github.com/tfsync/tfsync/commit/f5c9a4a91f4580c3c6c031d3a916e66e629d9b1c))

## [0.2.0](https://github.com/tfsync/tfsync/compare/v0.1.0...v0.2.0) (2026-04-14)


### Features

* **api:** add Workspace v1alpha1 CRD types ([98fbdd8](https://github.com/tfsync/tfsync/commit/98fbdd86435cb114bfbcf0531252620b79b29477))
* **cli:** add tfsync CLI for list, sync, plan ([54f832b](https://github.com/tfsync/tfsync/commit/54f832b609f8b244dadfee6418b2632502aa2c74))
* **controller:** add WorkspaceReconciler with ephemeral runner Jobs ([dc8bc4c](https://github.com/tfsync/tfsync/commit/dc8bc4c1ed5ff043db01320075a4b583abc5f33b))
