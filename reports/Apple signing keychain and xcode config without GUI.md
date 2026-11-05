# Run Apple development without persistent viewers

**Blackburn can edit Xcode configuration, manage signing identities and build over SSH without keeping Xcode or Keychain Access open.** Existing editors, project generators, `xcodebuild`, `security` and fastlane cover the required operations; a custom settings GUI is unnecessary. Full Intel-compatible Xcode must remain installed, and the established Simulator workflow should retain the macOS user's graphical login session. Routine simulator development does not require production signing assets; a team App Store Connect API key can also remove the need for initial Xcode Apple ID sign-in when provisioning is required. The important local exception is lifecycle management: **closing Blackburn's native Reims window powers off its VM**, and the current `macbox view` commands restart the guest. Use a dismissible browser viewer for the simplest operational model, or validate the controlled no-window transition below before relying on it unattended. This report records documentation and local-source research on 2026-11-05; none of its provisioning, signing or VM-transition commands was executed during this research.

## Existing tools cover configuration without a custom GUI

### Choose text configuration and resolve it with Xcode

Keep project configuration in version-controlled files on Linux and use the selected Xcode inside macOS to evaluate it. This separates three questions: what the project stores, what its effective build settings become, and what Apple authorizes through a provisioning profile. An editor answers the first; `xcodebuild` answers the second; signing and developer-portal operations answer the third. Apple's command-line toolchain supports project/workspace queries, builds, tests and archives independently of the IDE. The standalone Command Line Tools installation does **not** replace full Xcode for iOS SDKs and tools such as `simctl`. ([Apple command-line reference](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference), [Apple TN2339](https://developer.apple.com/library/archive/technotes/tn2339/_index.html))

| Tool | Useful role on Blackburn | Boundary and recommendation |
|---|---|---|
| SweetPad | VS Code scheme/destination selection, build/run, tests and debugging | Its documented setup requires a Mac with Xcode. A Linux extension installation does not supply a remote Apple toolchain. A macOS extension host through Remote-SSH is an integration candidate, untested here. It is not a complete Signing & Capabilities inspector. |
| Official Swift extension | Completion, navigation, diagnostics, LLDB and SwiftPM tasks | Its repository describes `.xcodeproj` support as limited. Use it for Swift editing, alongside guest-side Xcode commands. |
| Property List Editor | Tree-style viewing/editing of plist and `.entitlements` files | Sufficient for visual data editing; it cannot authorize portal capabilities or acquire profiles. |
| `.xcconfig` | Reviewable Debug/Release settings, SDK/architecture conditions | Wire it to project configurations; higher-precedence project/target settings can override it. |
| XcodeGen | Generate `.xcodeproj` from `project.yml`, target structure and settings | Good default for a new native app. Run a pinned version in macOS first; historical Linux support does not certify the current release on Blackburn. |
| Tuist | Swift manifests and shared helpers for larger project graphs | Current installation documentation excludes Xcode-dependent commands such as `tuist generate` on Linux. Edit manifests on Linux and generate in macOS. |
| Ruby `xcodeproj` | Targeted changes to existing projects, schemes and configurations | Prefer a compatible pinned gem over direct `.pbxproj` text replacement. Stored values do not resolve Xcode inheritance. |
| `plutil`, PlistBuddy, Python `plistlib` | Validate, inspect and modify typed plist data | Apple utilities run in the guest; Python also provides a Linux option. `plistlib` does not parse OpenStep `.pbxproj`. |
| `xcodebuild -showBuildSettings -json` | Inspect the effective settings for an actual scheme/configuration/SDK | Run in the selected full Xcode environment; use this as the configuration diagnostic authority. |

These boundaries follow the publishers' documentation: [SweetPad setup](https://sweetpad.hyzyla.dev/docs/vscode/getting-started/), [Swift extension](https://github.com/swiftlang/vscode-swift), [Property List Editor](https://marketplace.visualstudio.com/items?itemName=ivhernandez.vscode-plist), [Apple configuration files](https://developer.apple.com/documentation/xcode/adding-a-build-configuration-file-to-your-project), [XcodeGen](https://github.com/yonaskolb/XcodeGen), [XcodeGen Linux changelog](https://github.com/yonaskolb/XcodeGen/blob/master/CHANGELOG.md?plain=1), [Tuist installation](https://tuist.dev/en/docs/guides/install-tuist), [xcodeproj](https://raw.githubusercontent.com/CocoaPods/Xcodeproj/master/README.md), [Python plistlib](https://docs.python.org/3/library/plistlib.html) and [Apple JSON build-settings interface](https://developer.apple.com/documentation/Xcode-Release-Notes/xcode-12_5-release-notes).

For an existing Flutter project, retain Flutter/CocoaPods ownership and make focused `.xcconfig`, plist and `xcodeproj` changes. A generator migration introduces another source of truth and is unnecessary merely to change the bundle ID. For a new native project, XcodeGen's YAML is a compact default; Tuist is justified when shared Swift manifests and a larger graph become useful. Treat generator manifests as canonical because subsequent generation can discard manual project edits. Tuist's own migration guidance starts with a working baseline, extracts settings into `.xcconfig`, generates without opening Xcode, and validates with `xcodebuild`. ([Tuist manifests](https://tuist.dev/en/docs/guides/features/projects/manifests), [Tuist migration guidance](https://github.com/tuist/tuist/blob/main/skills/skills/migrate/SKILL.md))

A minimal XcodeGen configuration can reference existing files rather than generate them. The named files must exist; pin the generator version in project tooling. ([XcodeGen spec](https://github.com/yonaskolb/XcodeGen/blob/master/Docs/ProjectSpec.md))

```yaml
name: BlackburnApp
targets:
  BlackburnApp:
    type: application
    platform: iOS
    sources: [App]
    configFiles:
      Debug: Config/Signing.xcconfig
      Release: Config/Signing.xcconfig
    settings:
      base:
        INFOPLIST_FILE: App/Info.plist
        CODE_SIGN_ENTITLEMENTS: App/App.entitlements
```

```xcconfig
// Config/Signing.xcconfig; illustrative identifiers only.
DEVELOPMENT_TEAM = ABCDE12345
PRODUCT_BUNDLE_IDENTIFIER = com.example.blackburn
CODE_SIGN_STYLE = Automatic
GENERATE_INFOPLIST_FILE = NO
```

### Validate capabilities beyond the source entitlement file

An entitlement file is a signing input, not permission granted by Apple. A capability can also require linked frameworks, Info.plist entries, App ID changes and a newly acquired provisioning profile. Update all inputs required by that capability's documentation, then inspect the **built** plist and signature. A project `SystemCapabilities` marker or valid XML alone does not establish authorization. ([Apple capability guide](https://developer.apple.com/documentation/xcode/adding-capabilities-to-your-app), [Apple build settings reference](https://developer.apple.com/documentation/Xcode/build-settings-reference), [Apple entitlement troubleshooting](https://developer.apple.com/library/archive/technotes/tn2415/_index.html))

Run this inspection recipe inside macOS, using the actual workspace and scheme. Substitute `-project` if no workspace exists. `plutil -replace` assumes the key exists; use `-insert` for an absent key. Inspect the installed tools' help before turning examples into production automation.

```sh
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
xcodebuild -list -workspace Runner.xcworkspace
xcodebuild -workspace Runner.xcworkspace -scheme Runner \
  -configuration Debug -sdk iphonesimulator -showBuildSettings -json \
  > /tmp/blackburn-debug-settings.json
plutil -lint App/Info.plist App/App.entitlements
plutil -p App/Info.plist
plutil -replace CFBundleDisplayName -string Blackburn App/Info.plist
/usr/libexec/PlistBuddy -c 'Print :CFBundleDisplayName' App/Info.plist
```

Apple documents CLI entitlement-file manipulation with `plutil` and PlistBuddy. Use a typed parser rather than human-oriented `plutil -p` output for automated comparisons. A custom Blackburn GUI could wrap these interfaces, but it would improve convenience rather than unlock a missing capability. Detailed alternatives and limitations are in the [project configuration notes](../research_notes/Apple%20signing%20keychain%20and%20xcode%20config/project-configuration.md). ([Apple signing article](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/), [Apple PlistBuddy example](https://developer.apple.com/documentation/Xcode/embedding-a-helper-tool-in-a-sandboxed-app))

## Headless signing separates API access from private keys

### Use a dedicated keychain and exercise real signing

`security` supports creating, importing into, unlocking, searching and locking a file-based build keychain without Keychain Access. **A signing identity requires a certificate and its corresponding private key**; downloading a `.cer` cannot recreate a missing key. Unlocking the keychain is also separate from authorizing `codesign` to use an imported private key. Configure its partition list to avoid an interactive access prompt, using a dedicated build keychain because the signing-key selector can affect multiple keys. ([Apple TN3161](https://developer.apple.com/documentation/technotes/tn3161-inside-code-signing-certificates), [Apple security command source](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.1), [fastlane importer](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb))

This **one-time macOS Bash template** imports an existing trusted encrypted `.p12`. Inject its variables from secret storage; do not recreate an existing persistent keychain. The six-hour timeout is a policy choice, not an Apple requirement. Command arguments remain visible to sufficiently privileged processes even with tracing disabled.

```bash
set -euo pipefail
set +x
umask 077
: "${KEYCHAIN_PASSWORD:?Inject from secret storage}"
: "${P12_PASSWORD:?Inject from secret storage}"
: "${P12_PATH:?Absolute path to encrypted identity backup}"
KEYCHAIN_PATH="$HOME/Library/Keychains/blackburn-build.keychain-db"
security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security import "$P12_PATH" -k "$KEYCHAIN_PATH" -P "$P12_PASSWORD" \
  -f pkcs12 -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: \
  -s -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH" >/dev/null
security find-identity -v -p codesigning "$KEYCHAIN_PATH"
```

For each signed build, preserve the user's existing keychain search list, temporarily include this keychain, unlock it, run the build, restore the search list and lock it in an exit trap. The [signing sub-note](../research_notes/Apple%20signing%20keychain%20and%20xcode%20config/signing-and-keychain.md) contains the complete Bash preservation recipe. Serialize these jobs because the search list is user-wide. A persistent personal VM suits a dedicated persistent keychain; disposable jobs suit temporary keychains with explicit cleanup. Avoid replacing the entire search list or broad import access merely because a hosted-CI example does so. The acceptance test is an actual unattended signing/build operation from the daily SSH user, with Xcode and Keychain Access closed; identity listing alone does not exercise private-key access. ([Apple security CLI](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.1), [GitHub signing workflow](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications), [fastlane create_keychain](https://docs.fastlane.tools/actions/create_keychain/))

### Pick automatic provisioning or managed signing assets

**`AuthKey_*.p8` authenticates API requests; it is not the app's code-signing private key.** Use a **team API key** for provisioning endpoints; individual keys cannot use them. Initial API access and key creation require eligible account roles and website interaction, but that website can be used from Linux. Xcode Apple ID sign-in is therefore optional for eligible API-key/manual-signing workflows. Membership, agreements, roles and service-specific capability approval still apply, and keys/profiles/certificates need periodic maintenance. ([Apple API keys](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api), [Apple API onboarding](https://developer.apple.com/help/app-store-connect/get-started/app-store-connect-api), [Apple roles](https://developer.apple.com/help/account/access/roles))

| Route | What it automates | Blackburn usage |
|---|---|---|
| `xcodebuild` automatic provisioning | Portal updates using `-allowProvisioningUpdates` and API-key authentication | Use for an automatically signed project; supply API flags to each operation that needs portal access. |
| fastlane `match` | Synchronizes signing keys, certificates and profiles through Git/GCS/S3 | Preferred when repeatable shared assets matter. Routine builds use `readonly: true`; a maintenance lane creates/renews assets. |
| fastlane `cert` then `sigh` | Creates/imports an identity, then creates/repairs/downloads profiles | Suitable without shared match storage. `cert` cannot recover an existing lost private key. |

Apple demonstrated the API-key `xcodebuild` flags from Xcode 13 onward. Cloud-managed distribution signing is a distinct server-held-identity workflow with its own permissions; it does not mean a `.p8` universally replaces local signing identities. fastlane's API support also has action-specific limitations, so validate the exact intended operations. ([Apple CLI distribution demonstration](https://developer.apple.com/videos/play/wwdc2021/10204/), [cloud-managed certificates](https://developer.apple.com/help/account/certificates/cloud-managed-certificates/), [fastlane API support](https://docs.fastlane.tools/app-store-connect-api/), [match](https://docs.fastlane.tools/actions/match/), [cert](https://docs.fastlane.tools/actions/cert/), [sigh](https://docs.fastlane.tools/actions/sigh/))

A proposed automatic-provisioning archive/export sequence follows. Run in the guest with full Xcode selected. The project must have the correct team/capabilities; `ExportOptions.plist` must match the selected Xcode's `xcodebuild -help` and the intended distribution method. The flags authorize communication rather than overcoming missing keys, permissions or unsupported capabilities.

```bash
xcodebuild -project Example.xcodeproj -scheme Example \
  -configuration Release -destination 'generic/platform=iOS' \
  -archivePath build/Example.xcarchive \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_KEY_PATH" \
  -authenticationKeyID "$ASC_KEY_ID" \
  -authenticationKeyIssuerID "$ASC_ISSUER_ID" \
  DEVELOPMENT_TEAM="$APPLE_TEAM_ID" CODE_SIGN_STYLE=Automatic archive
xcodebuild -exportArchive -archivePath build/Example.xcarchive \
  -exportOptionsPlist ExportOptions.plist -exportPath build/export \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_KEY_PATH" \
  -authenticationKeyID "$ASC_KEY_ID" \
  -authenticationKeyIssuerID "$ASC_ISSUER_ID"
```

For existing match assets, a lane can avoid Xcode account login entirely. Supply the storage backend, credentials and any storage passphrase separately; the snippet intentionally does not create or revoke assets. Current match documentation distinguishes the Xcode 16+ profile location from the older MobileDevice directory, another reason to let the tool install profiles rather than hard-code a legacy path. ([match](https://docs.fastlane.tools/actions/match/), [API-key action](https://docs.fastlane.tools/actions/app_store_connect_api_key/))

```ruby
lane :signing_assets do
  api = app_store_connect_api_key(
    key_id: ENV.fetch("ASC_KEY_ID"),
    issuer_id: ENV.fetch("ASC_ISSUER_ID"),
    key_filepath: ENV.fetch("ASC_KEY_PATH")
  )
  match(
    type: "appstore",
    app_identifier: ["com.example.blackburn"],
    api_key: api,
    readonly: true,
    keychain_name: ENV.fetch("BUILD_KEYCHAIN_PATH"),
    keychain_password: ENV.fetch("KEYCHAIN_PASSWORD")
  )
end
```

Blackburn's ordinary **Intel simulator work should bypass the production-signing onboarding path**. Simulator and device-distribution artifacts have different architecture/platform requirements; simulator builds can use ad hoc signing and do not ordinarily require an Apple-issued distribution identity or device profile. Do not indiscriminately strip entitlements/signatures to fix errors, and do not expect signing to make an arm64 device IPA run in an x86_64 simulator. No physical device is available here, so simulator success cannot validate device-only capabilities. ([Apple TN2415](https://developer.apple.com/library/archive/technotes/tn2415/_index.html), [Apple provisioning workflow](https://developer.apple.com/library/archive/documentation/ToolsLanguages/Conceptual/YourFirstAppStoreSubmission/ProvisionYourDevicesforDevelopment/ProvisionYourDevicesforDevelopment.html))

## On-demand viewing needs a controlled macbox transition

### Distinguish a closed viewer from a stopped desktop

The achievable default is **no persistent Xcode GUI and no visible Linux viewer**, while retaining the guest's normal logged-in user session. Removing the Linux presentation window does not establish that WindowServer, securityd, trustd or CoreSimulator can be stopped. Keep those services and the simulator's owning user intact. Current local source provides only `macbox view local` and `macbox view stream`; `headless`, `private` and `stream off` are proposals, not shipped commands. ([local parser](../macbox/macbox:605), [startup](../macbox/macbox:298), [upstream architecture](https://github.com/TUNA-NOPE/macbox/blob/main/docs/architecture.md))

Two local findings change the requested “configure, close window, continue headlessly” procedure. The native SDL close event reaches QEMU's power-off callback, so **do not close the local Reims window expecting SSH to remain available**. In contrast, closing the browser tab disconnects that viewer while the detached stream server stays running. Also, changing `view` restarts the VM even when requesting the same mode; it is not live viewer attachment. Stream mode is therefore the simplest stock setup for occasional GUI access, although its private Sway and streaming helpers continue consuming resources after the browser closes. ([installed SDL handler](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-vgpu-window/src/sdl/shell.rs:380), [installed QEMU close callback](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:438), [view dispatcher](../macbox/macbox:608), [stream launcher](../macbox/container/stream-up.sh:39))

### Stage 1: bootstrap once, using a viewer only when useful

From Linux, select the existing Blackburn instance explicitly; its helper uses `blackburn.env`, whereas upstream defaults to `macbox.env`. Save active work before selecting a view because the command can restart the guest. For an existing setup, skip account creation and other completed bootstrap work. ([Blackburn configuration](../scripts/blackburn-macbox.sh:4), [upstream default](../macbox/macbox:7))

```bash
# Linux; use the installed macbox executable for this checkout.
export MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env"
macbox status
macbox view stream
macbox up
# Open http://127.0.0.1:6081/ in a browser.
# Alternative from a Wayland desktop: macbox view local; macbox up
ssh macbox true
```

Use the desktop for initial local-account/Remote Login setup if necessary. macOS/iCloud sign-in is unnecessary for creating a build user. If choosing Xcode-account authentication, use Xcode Settings > Accounts and complete sign-in/2FA here. If choosing team API-key or manual signing, skip it. Keychain Access is optional for inspecting imported identities or diagnosing prompts; ordinary imports and authorization use the CLI. Quit Xcode and Keychain Access when finished, leaving the guest user logged in. Autologin is a convenience decision, not a code-signing prerequisite; local provisioning refuses it with FileVault enabled. ([macbox setup](https://github.com/TUNA-NOPE/macbox/blob/main/docs/setup.md), [SSH bootstrap](../macbox/macbox:402), [guest provisioning](../macbox/guest/macbox-guest:187))

Install a compatible Intel-capable **Xcode 26 or earlier**, with an appropriate universal simulator runtime; verify the actual installed version and architecture. Apple documents that Intel Macs use Xcode 26 or earlier, so an arbitrary newer download is not a safe replacement. No claim about an unreleased version is needed. Local `macbox xcode <xip>` already selects Xcode, performs first-launch setup and enables debugging access without opening its IDE; guest runtime provisioning requests the universal architecture variant. ([Apple Intel guidance](https://developer.apple.com/documentation/apple-silicon/porting-your-macos-apps-to-apple-silicon), [Apple requirements](https://developer.apple.com/xcode/system-requirements), [local installer](../macbox/guest/macbox-guest:369), [runtime provisioner](../macbox/guest/macbox-guest:396))

Run the following guest setup only where needed. License acceptance must follow the user's acceptance of Apple's terms.

```bash
ssh -t macbox
file /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -license accept
sudo xcodebuild -runFirstLaunch
xcodebuild -version
xcodebuild -showsdks
xcrun --find simctl
xcrun simctl list runtimes
# If an appropriate runtime is missing, with compatible Xcode selected:
xcodebuild -downloadPlatform iOS -architectureVariant universal
exit
```

Apple documents first-launch and platform-download operations from Terminal. Selecting `/Library/Developer/CommandLineTools` is not a fix for a missing or incompatible iOS Xcode installation. Configure the signing path above only if required, then close the **browser tab** for stock daily operation. With a native viewer, minimize it or plan a transition; its close button shuts the VM down. ([Apple additional components](https://developer.apple.com/documentation/xcode/downloading-and-installing-additional-xcode-components), [Apple tools](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference))

### Stage 2: optionally validate a true no-host-window mode

Reims can retain its GPU/display objects without a Linux presentation window. Persist `REIMS_VGPU_WINDOW=0` in the VM configuration: a shell export is insufficient because `reims-ctl` sanitizes inherited variables and applies explicit VM overrides. The flag takes effect when QEMU next starts. Stock `macbox up` still starts Sway/stream helpers when no mounted host Wayland socket exists, so the flag alone is not a complete compositor-free mode. ([Reims overrides](https://github.com/steelbrain/reims-vgpu#environment-overrides), [local environment handling](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:119), [local window gate](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:869), [macbox startup](../macbox/macbox:298))

The following is a **source-derived, unexecuted manual procedure**, intended for a planned validation window. Stop builds/simulator work first. It retains the existing container, gracefully shuts down the guest, waits for a stopped VM, removes display services, then starts QEMU without a presentation window. Do not stop Sway while QEMU still owns its visible window.

```bash
# Linux Bash; source only the existing trusted local instance configuration.
export MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env"
source "$MACBOX_CONFIG"
: "${MACBOX_HOME:=$HOME/.local/share/macbox}"
: "${MACBOX_CONTAINER:=macbox}" "${MACBOX_VM:=mac}"
: "${MACBOX_DOCKER:=docker}" "${MACBOX_SSH_HOST:=macbox}"
ctl_existing() {
  "$MACBOX_DOCKER" exec -e "HOME=$MACBOX_HOME/home" \
    "$MACBOX_CONTAINER" bash -c \
    '. "$HOME/.cargo/env"; exec reims-ctl "$@"' reims-ctl "$@"
}
macbox up
ctl_existing set "$MACBOX_VM" --env REIMS_VGPU_WINDOW=0 --env WAYLAND_DISPLAY=
ssh "$MACBOX_SSH_HOST" 'sudo -n shutdown -h now'
# SSH can disconnect during shutdown. Check repeatedly until VM is STOPPED:
ctl_existing list
# STOP HERE if it is still running or shutdown authorization failed.
```

Only after confirming stopped state, continue in that same Linux shell:

```bash
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -f '[s]elkies-venv/bin/selkies' || true
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -f '/[w]ebsockify' || true
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -x wayvnc || true
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -x sway || true
ctl_existing start "$MACBOX_VM"
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" ps -eo pid,args
```

Inspect full process arguments for remaining capture/encoder helpers. This transition does not patch the wrapper: a subsequent `macbox down` removes the container, and `macbox up` recreates its display stack. Wrapper status can therefore still say “stream” after manual service removal. To restore viewing while the container exists, run `ctl_existing set "$MACBOX_VM" --env REIMS_VGPU_WINDOW=1`, then `macbox view stream` or `macbox view local`, accepting its restart. If the container was removed, recreate it with `macbox up`, set the override back to 1 through ctl, then request the desired view. Changing view while the persisted override remains 0 will not restore the Reims window. ([shutdown implementation](../macbox/macbox:324), [stream processes](../macbox/container/stream-up.sh:18), [view dispatcher](../macbox/macbox:608))

The appropriate future engineering change is a small explicit wrapper mode that skips display startup, persists the correct flag and reports accurate status; visible modes must restore flag 1. A private-display mode with encoder toggling would instead allow restart-free browser reconnection while retaining Sway. Neither requires a bespoke signing/settings GUI. The [operational notes](../research_notes/Apple%20signing%20keychain%20and%20xcode%20config/operational-lifecycle.md) contain the underlying source references and recovery details.

## Validate daily work before claiming fully headless operation

For native Swift/ObjC projects, run the guest's full Apple build toolchain over SSH. The existing Flutter/xcross path can retain its Linux compilation, simulator deployment and VM-service tunnel; this research does not establish that arbitrary native Xcode projects work through xcross unchanged. Use an available x86_64 simulator runtime and compatible dependencies. The local simulator bridge already uses `simctl` boot/readiness/install/launch operations without requiring `open -a Simulator`. ([Apple tool reference](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference), [local simulator launcher](../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:136))

```bash
# Guest SSH session; replace project, scheme, UUID and bundle identifier.
launchctl print "gui/$(id -u)" >/dev/null
xcrun simctl list devices available
cd ~/work/MyApp
xcodebuild -project MyApp.xcodeproj -scheme MyApp -configuration Debug \
  -sdk iphonesimulator -destination 'platform=iOS Simulator,id=SIMULATOR_UUID' \
  -derivedDataPath "$PWD/DerivedData" ARCHS=x86_64 ONLY_ACTIVE_ARCH=YES build
# Use -workspace instead of -project where applicable.
xcrun simctl boot SIMULATOR_UUID  # only if presently shut down
xcrun simctl bootstatus SIMULATOR_UUID -b
xcrun simctl install SIMULATOR_UUID DerivedData/Build/Products/Debug-iphonesimulator/MyApp.app
xcrun simctl launch SIMULATOR_UUID com.example.MyApp
xcrun simctl io SIMULATOR_UUID screenshot /tmp/myapp.png
# From Linux afterward: scp macbox:/tmp/myapp.png /tmp/myapp.png
```

The acceptance record should demonstrate the same-user graphical login domain, successful build and simulator readiness, real app installation/launch, a nonblank screenshot, and—where relevant—debugger attachment and Flutter hot reload. For signed artifacts, additionally demonstrate a real signing/archive/export operation through SSH and inspect the built entitlements. For the no-window path, record container process arguments proving capture helpers stopped. An SSH prompt, valid identity list or absence of a visible window is individually insufficient. No rendering test, RAM/power benchmark or signing smoke test was performed here, so no resource-saving percentage or runtime success is claimed.

| Failure | First diagnostic and recovery | Source |
|---|---|---|
| Certificate visible but no signing identity | Check `security find-identity -v -p codesigning`; recover its paired private key from trusted `.p12` or issue a new identity. | [Apple TN3161](https://developer.apple.com/documentation/technotes/tn3161-inside-code-signing-certificates) |
| Keychain prompt or SSH signing failure | Verify same build user, unlock state, search list and key partitions. `errSecInternalComponent` alone does not identify one cause. | [fastlane importer](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb) |
| Profile/entitlement mismatch | Compare bundle ID, certificate and enabled capabilities. Decode profile with `security cms -D -i profile.mobileprovision`; inspect app with `codesign -d --entitlements :- App.app`. | [Apple TN2415](https://developer.apple.com/library/archive/technotes/tn2415/_index.html) |
| API 401/403 | Verify matching key/issuer IDs and `.p8`, team-key type, revocation, role, membership and agreements. | [Apple API keys](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api) |
| Profile not found after Xcode upgrade | Check selected Xcode and current profile installation directory; prefer match's installation logic. | [match](https://docs.fastlane.tools/actions/match/) |
| Local viewer closure ends SSH session | Current source deliberately powers off the VM; restart deliberately and use stream viewing or validate no-window mode. | [local QEMU callback](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:438) |
| No viewer after restoring stream mode | Restore persisted `REIMS_VGPU_WINDOW=1`, then restart through the selected view mode. | [local window gate](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:873) |
| SSH works but simulator output is blank | Recheck logged-in user domain, compatible runtime, bootstatus, destination architecture and screenshots; return to GUI for diagnosis. | [local readiness path](../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:136) |

The practical investment is in a reliable lifecycle wrapper and automated acceptance checks. Configuration and signing already expose usable interfaces; the remaining Blackburn-specific uncertainty concerns no-window simulator rendering and lifecycle recovery. Until those are runtime-validated, stream mode with a closed browser tab is the concrete low-friction baseline. When no guest work is needed, stop individual simulators or use `macbox down`; keeping an idle VM alive is not required to preserve its project configuration or signing assets.
