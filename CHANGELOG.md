# Changelog

## 0.1.0 (2026-11-18)


### ⚠ BREAKING CHANGES

* **chart:** helm install without -f examples/values.yaml no longer creates the rtl-sdr and rak4631 DeviceClasses.
* **chart:** kubectl apply -f deploy/daemonset.yaml -f deploy/deviceclasses.yaml no longer works; install the chart instead.
* **usb-dra:** rename driver to usb.jacaudi.dev

### Features

* **chart:** add usb-dra chart on bjw-s common with the DaemonSet and RBAC ([63b48d1](https://github.com/jacaudi/usb-dra/commit/63b48d17c007c1226616ac9108f6f0ead74ba5e2))
* **chart:** render DeviceClasses from values and drop the hand-written manifests ([5fe729b](https://github.com/jacaudi/usb-dra/commit/5fe729b1e1b2affdd224b9798962faecb7e3a283))
* **chart:** render the node-scoped slice policy from the chart's own names ([4bc0abc](https://github.com/jacaudi/usb-dra/commit/4bc0abcf98f6e7aea775ebdddf3a8f51da0cf6cf))
* **deploy:** add usb deviceclasses and guarded example claims ([2d7f047](https://github.com/jacaudi/usb-dra/commit/2d7f04739580c9770eac4b1e61db1eab0f9c3a59))
* **kubeletplugin:** add versioned atomic checkpoint ([de3a10d](https://github.com/jacaudi/usb-dra/commit/de3a10dbbe18ffaa21d19c69ff28f932903c92f9))
* **kubeletplugin:** inject ttys with aliases and persist prepared claims ([3f7836a](https://github.com/jacaudi/usb-dra/commit/3f7836a3472d13bdeea1256f74af7960933a817c))
* **rtlsdr-dra:** DRA driver exposing RTL-SDR dongles as schedulable devices ([b0f5b47](https://github.com/jacaudi/usb-dra/commit/b0f5b47f02dde89b667c61c4e8bf6e02dd5fe3a8))
* **usb-dra:** rename driver to usb.jacaudi.dev ([05b234d](https://github.com/jacaudi/usb-dra/commit/05b234d6902860a3c92233b8fbf6dfe0794dec43))
* **usb:** add device class filter and include overrides ([1f8e3b0](https://github.com/jacaudi/usb-dra/commit/1f8e3b0b812d787c6cc33e0d9c79dd85944b4e70))
* **usb:** publish every eligible usb device with stable identity ([01036b3](https://github.com/jacaudi/usb-dra/commit/01036b3ca96f07b8d3d5b82e4e2aab04b9536780))
* **usb:** scan sysfs for every usb device with interfaces, ttys and port ([b82de51](https://github.com/jacaudi/usb-dra/commit/b82de5131c421a7e6cff1083cda999eae2ce9326))


### Bug Fixes

* address security audit finding S1 ([0a7ca06](https://github.com/jacaudi/usb-dra/commit/0a7ca06198a7a3417a003edb8632da29c5beff6f))
* **chart:** publish bare chart version, reject zero rescanInterval, tidy README ([572b7b5](https://github.com/jacaudi/usb-dra/commit/572b7b543c75282d8799b434198925293c00c7bf))
* **chart:** quote interpolated values and tighten the cdiRoot pattern ([f722e74](https://github.com/jacaudi/usb-dra/commit/f722e74c79b8e1e2dabd7c7343b061c4b473941a))
* **deploy:** scope the driver's ResourceSlice writes to its own node ([f59ba64](https://github.com/jacaudi/usb-dra/commit/f59ba64d386edd592322e57d8644d5e90234725f))
* **deps:** unblock the cdi v1.1.2 and go 1.27.4 upgrades ([27d2007](https://github.com/jacaudi/usb-dra/commit/27d20077bb93ed1054adfbbafc547f5274d7f380))
* **kubeletplugin:** let swapped devices re-prepare after a port swap ([ba032b1](https://github.com/jacaudi/usb-dra/commit/ba032b1c2e22669f384838a6d92b1c7fd4fa874d))
* **kubeletplugin:** reject stray arguments and a non-positive rescan interval ([8023a06](https://github.com/jacaudi/usb-dra/commit/8023a06974f746d8d55999e7b8d070fc224144a2))
* **kubeletplugin:** scan sysfs once per claim in prepare ([d781ad8](https://github.com/jacaudi/usb-dra/commit/d781ad8b0a5a9ecd60d558b721ac03b0aad912e5))
* **kubeletplugin:** single-line holder doc comment ([1704f05](https://github.com/jacaudi/usb-dra/commit/1704f05cc7ccc736f4836336d249ce6a0ff52925))
* **rtlsdr-dra:** run as root and grant resourceclaims read ([8d0bf44](https://github.com/jacaudi/usb-dra/commit/8d0bf445f1c9fd075cb94fdec3b21c7db3815a3a))
* **usb:** drop vhci devices before reading their interfaces ([6c4b3be](https://github.com/jacaudi/usb-dra/commit/6c4b3be9fce9d28bc627ad5eef56adaebbdaee7a))
* **usb:** skip devices whose vendor or product id is not four hex digits ([af703e6](https://github.com/jacaudi/usb-dra/commit/af703e6b0da26a110e8934dffa765d50b33fb543))
* **usb:** treat control-character serials as unusable and drop dead code ([2875a1d](https://github.com/jacaudi/usb-dra/commit/2875a1da29f46e39b1ba98243fdb6ef318ebe25d))


### Refactors

* **chart:** build common values from one helper ([9a1af60](https://github.com/jacaudi/usb-dra/commit/9a1af60aa45c707c76585ae2b9b50d69c39736f4))
* **chart:** empty default values; example deployment in examples/ ([d899747](https://github.com/jacaudi/usb-dra/commit/d899747da1c59bc55682cc581f33c00f2270221c))
* **ci:** name the kubectl manifest install.yaml ([7f13f70](https://github.com/jacaudi/usb-dra/commit/7f13f70ba168857bccb2f9abb47b8fa2bc16084c))


### Documentation

* add project instructions for Claude Code ([1aa7357](https://github.com/jacaudi/usb-dra/commit/1aa7357b38c621871a52e26585675726d8f4f9d8))
* align readme with the code as built ([a9a878e](https://github.com/jacaudi/usb-dra/commit/a9a878eae9c3c72202a8097e8963881a138fccda))
* correct claim allocation wording ([2838d03](https://github.com/jacaudi/usb-dra/commit/2838d03543603bb8f8f6857e52477875562457a7))
* correct README claims and invariant 12 wording ([67c438d](https://github.com/jacaudi/usb-dra/commit/67c438d1d32e70928433c2be0f65f632f62d8f2c))
* describe usb-dra and its invariants ([57bd317](https://github.com/jacaudi/usb-dra/commit/57bd3175dd0ec24ce3144be9fbd5febdba95b6fa))
* **design:** note that serials with control characters are unusable ([9a1bd21](https://github.com/jacaudi/usb-dra/commit/9a1bd211b947cdc6be26604990f4fb5d37a1c45b))
* **design:** revise the helm chart design after review ([90d8dcb](https://github.com/jacaudi/usb-dra/commit/90d8dcb71ef808c5ed5d25fcefa29ff30ff4fa2e))
* **design:** usb-dra generic usb dra driver ([2d5bf57](https://github.com/jacaudi/usb-dra/commit/2d5bf57f6238a69f9796e1cc8c1dee62d17ca310))
* **design:** usb-dra helm chart on bjw-s common ([42e6b9f](https://github.com/jacaudi/usb-dra/commit/42e6b9f978cfbe2329d7080d74e7b25dc8d05b9f))
* **examples:** add Flux HelmReleases for RTL-SDRs and a USB printer ([3b73cce](https://github.com/jacaudi/usb-dra/commit/3b73ccea7631ecbf5d678abb9548618bac5db5ed))
* install usb-dra with the helm chart ([470aa4b](https://github.com/jacaudi/usb-dra/commit/470aa4b4f612636354c2ccaeec64933c21aee15e))
* **plan:** usb-dra helm chart implementation plan ([0839c0c](https://github.com/jacaudi/usb-dra/commit/0839c0c72939e056994e8c1e89b6289e611576d8))
* **readme:** add license section ([0f247f8](https://github.com/jacaudi/usb-dra/commit/0f247f8c7f4b3defe86f0f9c355f9d35c6813339))
* **readme:** restructure readme as a quick start ([15a034c](https://github.com/jacaudi/usb-dra/commit/15a034cc8ba556992defe04495dcffd8de021383))
* shorten code comments and use generic USB wording ([3c491b0](https://github.com/jacaudi/usb-dra/commit/3c491b01b4eeebf30871a231953c13d9e9a8a405))
* update the design to the code as built ([a13304d](https://github.com/jacaudi/usb-dra/commit/a13304da2776bd82e0af7f0eb90ec8b510e6a9b9))


### Miscellaneous Chores

* add renovate config ([959dd3e](https://github.com/jacaudi/usb-dra/commit/959dd3e6e7335ff78a523dfb7dbc34c184512e4a))
* **deps:** bump x/net, grpc and the go toolchain to clear govulncheck ([51e2101](https://github.com/jacaudi/usb-dra/commit/51e21019eca3e382991e03f1a955e86d8180f875))
* **deps:** Update actions/setup-go action to v7.1.0 ([f2ea339](https://github.com/jacaudi/usb-dra/commit/f2ea339deec4e44cafec443c0dd0aac8fce99aec))
* **deps:** Update astral-sh/setup-uv action to v10 ([d016ab0](https://github.com/jacaudi/usb-dra/commit/d016ab06fd74404cf216fd3bfcad668299ba7e96))
* **deps:** Update dependency go to v1.26.6 ([#6](https://github.com/jacaudi/usb-dra/issues/6)) ([29843f5](https://github.com/jacaudi/usb-dra/commit/29843f577442ea44c72ddfaba4e95781e2211134))
* **deps:** Update dependency go to v1.27.4 ([308f897](https://github.com/jacaudi/usb-dra/commit/308f8970d6c40b7666574fe56915628bc6821df3))
* **deps:** Update gcr.io/distroless/static-debian12:latest Docker digest to d75cdd7 ([66c7118](https://github.com/jacaudi/usb-dra/commit/66c711832bc971fb4c945537dbdfcb2bc480ea60))
* **deps:** Update GitHub Actions ([ce8171e](https://github.com/jacaudi/usb-dra/commit/ce8171ebcf777eccf94b838c2cb5fa73e1ce47ab))
* **deps:** Update kubernetes-client-libraries to v0.34.10 ([#1](https://github.com/jacaudi/usb-dra/issues/1)) ([fbb0e7e](https://github.com/jacaudi/usb-dra/commit/fbb0e7ecd81cc381c952d3a22d78d8846c2b3dcc))
* **deps:** Update kubernetes-client-libraries to v0.34.11 ([#10](https://github.com/jacaudi/usb-dra/issues/10)) ([85c197b](https://github.com/jacaudi/usb-dra/commit/85c197b726e2702248737e630aed487101b36a5e))
* **deps:** Update kubernetes-client-libraries to v0.34.13 ([24f6f02](https://github.com/jacaudi/usb-dra/commit/24f6f02e740f8d22ffeb9e74cb36a53299e0e0f7))
* **deps:** Update mirror.gcr.io/library/golang Docker tag to v1.27 ([1cffa29](https://github.com/jacaudi/usb-dra/commit/1cffa292c438c1b869b5b8e6e5dbdf9f05f82b4a))
* **deps:** Update module k8s.io/apimachinery to v0.34.11 ([#9](https://github.com/jacaudi/usb-dra/issues/9)) ([5489c1f](https://github.com/jacaudi/usb-dra/commit/5489c1f82b31332228cf6364e378b92a557c1a7a))
* **deps:** Update module tags.cncf.io/container-device-interface to v1.1.2 ([1f9ad44](https://github.com/jacaudi/usb-dra/commit/1f9ad4456f65f7280ac9530ee76cf94b3d257c4e))
* license the project under MIT ([be6545f](https://github.com/jacaudi/usb-dra/commit/be6545f4ef2d83c2126d20e08cf6da051495ff41))
