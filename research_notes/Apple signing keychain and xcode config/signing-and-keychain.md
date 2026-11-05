# Headless Apple signing and Keychain on Blackburn

Research date: 2026-11-05. Documentation and source review only; no VM, Apple account, or signing credentials were changed. Commands below run **inside the macOS guest**, as its dedicated build user.

## 1. Can the signing Keychain operate without Keychain Access?

### Takeaway

Yes. A file-based, dedicated build keychain supports unattended certificate import, unlocking, identity discovery and signing. Unlocking alone does not grant a signing process access to an imported private key; configure the access partition list as well. [Apple security command source](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.1), [fastlane importer source](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb).

### Cited Findings

- `security find-identity` searches for a certificate **and its corresponding private key**; a downloaded `.cer` alone is insufficient. Apple distinguishes file keychains from the data protection keychain, whose identities this command does not enumerate. [Apple TN3161](https://developer.apple.com/documentation/technotes/tn3161-inside-code-signing-certificates).
- The CLI covers `create-keychain`, `unlock-keychain`, `set-keychain-settings`, `import`, `list-keychains`, `find-identity`, and `lock-keychain`/`delete-keychain`. The partition list is an ACL restriction based on calling application signatures; `apple:` is required for `/usr/bin/codesign`. The `-s` selector matches **keys capable of signing**, not a single identity. Use a dedicated keychain, rather than applying this operation broadly to a personal login keychain. [Apple CLI source](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.1).
- GitHub documents temporary keychain creation, import and partition-list setup for unattended macOS CI. Its example replaces the search list and uses broad import access (`-A`); a persistent Blackburn guest should preserve its search list and can authorize the specific signing tool instead. Self-hosted runners require explicit cleanup. [GitHub signing workflow](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications).
- fastlane supplies `keychain_password` to configure partitions; omission can trigger a password prompt. Its importer currently uses partitions `apple-tool:,apple:,codesign:`. [fastlane importer implementation](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb).

**Illustrative macOS Bash template — inspect placeholders before use:**

```bash
#!/bin/bash
set -euo pipefail
set +x
umask 077
: "${KEYCHAIN_PASSWORD:?Inject from secret storage}"
: "${P12_PASSWORD:?Inject from secret storage}"
: "${P12_PATH:?Absolute path to encrypted certificate plus private key}"
KEYCHAIN_PATH="$HOME/Library/Keychains/blackburn-build.keychain-db"

# One-time provisioning only; do not recreate an existing persistent keychain.
security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security import "$P12_PATH" -k "$KEYCHAIN_PATH" -P "$P12_PASSWORD" \
  -f pkcs12 -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: \
  -s -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH" >/dev/null
security find-identity -v -p codesigning "$KEYCHAIN_PATH"
```

The timeout and lock-on-sleep flags intentionally bound the unlock interval. Keep passwords out of committed scripts and shell tracing; password arguments can still be exposed to processes/users with sufficient privilege. This is a local build-user trust boundary, not a secret-isolation mechanism. The template combines documented `security` operations and fastlane's partition configuration. [Apple CLI source](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.1), [fastlane importer](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb).

**Per-build search-list preservation, unlock, and restoration:**

```bash
# Continue in Bash with the variables above; no sudo.
old_keychains=()
while IFS= read -r line; do
  line="${line#*\"}"
  line="${line%\"*}"
  old_keychains+=("$line")
done < <(security list-keychains -d user)

cleanup() {
  security list-keychains -d user -s "${old_keychains[@]}"
  security lock-keychain "$KEYCHAIN_PATH"
}
trap cleanup EXIT
security list-keychains -d user -s "$KEYCHAIN_PATH" "${old_keychains[@]}"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security find-identity -v -p codesigning "$KEYCHAIN_PATH"
# Run the project's xcodebuild/fastlane build here, before this shell exits.
```

This is an engineering adaptation of CLI search-list semantics. Serialize signing jobs for this user: search-list changes are user-wide and parallel jobs can race. Do not change the default keychain merely to add a signing identity. An ephemeral variant uses a unique path per job and calls `security delete-keychain` after restoring the search list, plus removes only profiles/temporary secret files created by that job. [Apple CLI source](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.1), [GitHub self-hosted cleanup](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications).

### Inferences

- For this long-lived personal VM, a persistent dedicated keychain, explicitly unlocked at build start and locked afterward, offers less churn. For disposable CI use an ephemeral keychain. fastlane supports a custom keychain path, search-list insertion, timeout, and optional default-keychain change; its `setup_ci` creates a temporary keychain and sets match to read-only. [create_keychain](https://docs.fastlane.tools/actions/create_keychain/), [setup_ci](https://docs.fastlane.tools/actions/setup_ci/).

### Gaps

- These templates have not been executed against Blackburn's installed Tahoe/Xcode versions. Validate `security help import`, `security help set-key-partition-list` and one actual signing operation over SSH before declaring setup complete. GUI inspection is a recovery option, not a required dependency.

## 2. How do fastlane and App Store Connect API replace interactive setup?

### Takeaway

Use either Apple automatic provisioning authenticated with a **team** API key, or fastlane-managed local certificates/profiles. An `AuthKey_*.p8` authenticates API requests; it is not an Apple Development/Distribution signing identity. [Apple API keys](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api), [Apple signing certificate internals](https://developer.apple.com/documentation/technotes/tn3161-inside-code-signing-certificates).

### Cited Findings

- `cert` checks for installed signing identities; when required it generates a private key and CSR, obtains a certificate and imports it. It cannot recover an existing certificate's private key from Apple and does not revoke existing certificates automatically. Missing private keys require a trusted `.p12` backup/import or a newly issued identity. [cert](https://docs.fastlane.tools/actions/cert/).
- `sigh` creates, renews, repairs, downloads and installs provisioning profiles. Run `cert` first when using the pair. It manages its own profiles rather than Xcode-managed ones; its `offline_profile` feature still requires Apple ID authentication. [sigh](https://docs.fastlane.tools/actions/sigh/).
- `match` synchronizes keys/certificates/profiles via Git, Google Cloud or S3; Git storage is encrypted. `readonly: true` fetches existing assets without creating replacements. Current docs identify the Xcode 16+ profile directory as `~/Library/Developer/Xcode/UserData/Provisioning Profiles`, and the earlier directory as `~/Library/MobileDevice/Provisioning Profiles`. Select Xcode before running match. Existing signing assets can be imported; there is no need to run destructive `match nuke` as a routine setup step. [match](https://docs.fastlane.tools/actions/match/).
- An API key signs JWTs. **Individual keys cannot use provisioning endpoints**; use a team key for certificate/profile automation. Team keys cover all apps and have role-based access, so an app restriction is not a security boundary for that key. [Apple API key documentation](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api), [fastlane API key support](https://docs.fastlane.tools/app-store-connect-api/).
- The Account Holder requests API access; Account Holder/Admin generates team keys. Download is one-time. Initial website setup can be performed from a Linux browser: it does not imply signing into the macOS desktop or Xcode. [Apple API setup](https://developer.apple.com/help/app-store-connect/get-started/app-store-connect-api).
- From Xcode 13, `xcodebuild` supports API-key authentication with `-authenticationKeyPath`, `-authenticationKeyID`, `-authenticationKeyIssuerID`, together with `-allowProvisioningUpdates`, without signing into Xcode. This permits CLI distribution and cloud signing. [Apple WWDC21 demonstration](https://developer.apple.com/videos/play/wwdc2021/10204/).
- Cloud-managed distribution signing is a distinct workflow, with access permissions and server-held identities; it is not a universal substitute for all local build signing requirements. [Apple cloud-managed certificates](https://developer.apple.com/help/account/certificates/cloud-managed-certificates/), [Apple UserRole permissions](https://developer.apple.com/documentation/appstoreconnectapi/userrole).
- Legal agreements, membership status and Certificates, Identifiers & Profiles access still apply. Users added to an individually enrolled developer's App Store Connect account do not thereby become members of that Apple Developer Program team. Enterprise enrollment has a separate API. [Apple roles](https://developer.apple.com/help/account/access/roles), [Enterprise API](https://developer.apple.com/help/account/keys/enterprise-program-api/).

**Headless automatic provisioning template:**

```bash
# Run inside guest; Xcode.app installed and selected; key outside source tree.
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

The export options must match the intended distribution method and selected Xcode; consult its `xcodebuild -help`. `-allowProvisioningUpdates` authorizes portal communication, not a promise to overcome missing permissions, missing local keys, unsupported entitlements, or device registration constraints. The API flags should accompany each operation requiring portal access. [Apple CLI distribution example](https://developer.apple.com/videos/play/wwdc2021/10204/).

**Fastfile template for existing match assets:**

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

Supply match storage configuration and its storage credentials/passphrase separately. A controlled maintenance lane can use `readonly: false` to create/renew assets; normal builds consume them read-only. `cert(api_key: api, ...)` followed by `sigh(api_key: api, app_identifier: ...)` is the alternative when shared match storage is unnecessary. [match](https://docs.fastlane.tools/actions/match/), [API key action](https://docs.fastlane.tools/actions/app_store_connect_api_key/), [cert](https://docs.fastlane.tools/actions/cert/), [sigh](https://docs.fastlane.tools/actions/sigh/).

### Inferences

- Blackburn should skip this entire production-signing path for routine simulator development. Device identity/provisioning is separate from building simulator binaries; simulator signing may be ad hoc and entitlement-dependent rather than requiring an Apple-issued identity. Do not indiscriminately strip signatures or entitlements to resolve a project error. Apple's development workflow explicitly separates simulator testing and device provisioning. [Apple device provisioning guide](https://developer.apple.com/library/archive/documentation/ToolsLanguages/Conceptual/YourFirstAppStoreSubmission/ProvisionYourDevicesforDevelopment/ProvisionYourDevicesforDevelopment.html).
- API-key onboarding can eliminate the proposed initial Xcode Apple ID sign-in entirely for eligible paid-team workflows. This does not eliminate interactive website enrollment, agreement acceptance or account recovery. [API setup](https://developer.apple.com/help/app-store-connect/get-started/app-store-connect-api), [Apple roles](https://developer.apple.com/help/account/access/roles).

### Gaps

- Apple role tables use icons omitted by text extraction; do not infer precise least-privilege role support for every certificate endpoint from an empty extracted table. Validate the selected **team key** against the exact certificate/profile operations needed; Admin is a broad bootstrap option, not a least-privilege recommendation.
- The installed Xcode's `-help` remains the authoritative source for current provisioning/export options; no macOS command was run during this research.

## 3. What is the headless failure/recovery path?

### Takeaway

First classify identity, unlock/access, profile and API errors independently. Keychain Access and Xcode can help inspect difficult cases on demand, but most recovery steps have CLI or web equivalents. [Apple missing-key guidance](https://help.apple.com/xcode/mac/current/en.lproj/devd1432da9a.html), [fastlane importer error handling](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb).

### Cited Findings

| Symptom | Diagnostic / action | Basis |
|---|---|---|
| Certificate exists but no valid identity | Run `security find-identity -v -p codesigning "$KEYCHAIN_PATH"`; check key pairing, validity and certificate chain. Recover trusted `.p12` or issue a new identity if the key is lost. | [Apple TN3161](https://developer.apple.com/documentation/technotes/tn3161-inside-code-signing-certificates), [cert](https://docs.fastlane.tools/actions/cert/) |
| Keychain prompt / signing access failure over SSH | Unlock the intended keychain as the build user; verify search list and reapply partitions with the correct keychain password. `errSecInternalComponent` is not proof of one specific cause. | [fastlane importer](https://github.com/fastlane/fastlane/blob/master/fastlane_core/lib/fastlane_core/keychain_importer.rb) |
| No profile / entitlement mismatch | Match target bundle ID, signing certificate and enabled capabilities to the profile, then regenerate only the affected profile. Inspect via `security cms -D -i profile.mobileprovision` and `codesign -d --entitlements :- App.app`. | [Apple TN2415](https://developer.apple.com/library/archive/technotes/tn2415/_index.html) |
| API 401/403 | Verify key ID, issuer, matching `.p8`, team-key type, revocation and role; check membership/agreements. Do not replace a working signing identity merely because API authentication fails. | [Apple API keys](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api), [Apple access recovery](https://developer.apple.com/help/account/access/resolving-access-issues) |
| Profile remains undiscovered on current Xcode | Check selected Xcode and Xcode 16+ profile directory; prefer match installation over copying into an old hard-coded directory. | [match](https://docs.fastlane.tools/actions/match/) |

### Inferences

- A successful acceptance test is an unattended signing/build invocation from the **same SSH/build-user context used daily**, with Xcode and Keychain Access closed. Identity listing alone does not exercise private-key signing access. Keep the guest's normal security services running; removing the viewer is a different operation from disabling Keychain/security daemons.

### Gaps

- This research does not establish that a macOS guest with no Aqua login session can run Blackburn's simulator or every plugin. Signing CLI feasibility and simulator session requirements are separate questions. Do not equate closing a VM viewer with terminating WindowServer or logging out.
