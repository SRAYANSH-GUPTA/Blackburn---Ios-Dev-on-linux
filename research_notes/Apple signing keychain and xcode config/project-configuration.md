# Xcode project configuration without the GUI

Research date: 2026-11-05. Status: primary documentation/source review; examples below are proposed recipes, not commands executed against Blackburn's macOS guest. No claim that a particular Xcode/iOS SDK release works on this Intel guest is implied.

## Which existing tools replace everyday configuration work?

### Takeaway

The existing toolchain is sufficient for build settings, project structure, Info.plist and entitlements. A custom GUI is optional product work, not a dependency for this workflow. Keep editable configuration on Linux; use the guest's selected Xcode command-line tools to evaluate and validate the actual Apple build.

### Cited Findings

| Tool | Useful scope | Boundary |
|---|---|---|
| SweetPad VS Code extension | Schemes, build/run, destinations, debugger, tests, SourceKit-LSP integration | Its installation guide requires a Mac with Xcode. It is not a general graphical replacement for the Signing & Capabilities or Build Settings inspectors. [Official setup](https://sweetpad.hyzyla.dev/docs/vscode/getting-started/), [overview](https://sweetpad.hyzyla.dev/) |
| Official Swift extension (`swiftlang.swift-vscode`) | Completion, navigation, diagnostics, LLDB DAP and SwiftPM tasks | Most features target SwiftPM; its README explicitly calls `.xcodeproj` support limited. Cross-platform extension availability does not supply Apple's SDKs or evaluate iOS build settings on Linux. [Repository](https://github.com/swiftlang/vscode-swift), [official documentation](https://docs.swift.org/main/documentation/userdocs/) |
| Property List Editor (`ivhernandez.vscode-plist`) | A tree-style custom editor; its listing explicitly includes `.entitlements` | It edits property-list data. It does not grant portal capabilities or generate provisioning profiles. [Publisher listing](https://marketplace.visualstudio.com/items?itemName=ivhernandez.vscode-plist) |
| `.xcconfig` files | Human-readable build settings, Debug/Release separation, conditional settings for SDK/architecture | Project/target settings can override these files. They must be wired to configurations; editing an unreferenced file has no effect. [Apple guide](https://developer.apple.com/documentation/xcode/adding-a-build-configuration-file-to-your-project) |
| Ruby `xcodeproj` | Read/update existing `.xcodeproj`, workspaces, schemes, configurations; CLI can print configurations, YAML and diffs | Use a version that understands the project's object types. Reading stored settings is distinct from resolving Xcode's inheritance and SDK defaults. [Maintainer README](https://raw.githubusercontent.com/CocoaPods/Xcodeproj/master/README.md) |

Apple supports `xcodebuild` for querying, building, testing and archiving projects/workspaces, with `-list` and `-xcconfig` examples. The separately installed Command Line Tools package includes a macOS SDK; it is not the full iOS Xcode toolchain. Keep Xcode installed on disk while quitting its IDE process. [Apple TN2339](https://developer.apple.com/library/archive/technotes/tn2339/_index.html)

`xcodebuild -showBuildSettings -json` is an Apple-documented interface (including an output fix in release notes). Use it to inspect effective settings for the chosen scheme/configuration/SDK, rather than assuming a raw project-file value wins. [Apple release notes](https://developer.apple.com/documentation/Xcode-Release-Notes/xcode-12_5-release-notes)

Proposed inspection command, run **inside macOS**, substituting the real container/scheme:

```sh
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
xcodebuild -list -workspace Runner.xcworkspace
xcodebuild -workspace Runner.xcworkspace -scheme Runner \
  -configuration Debug -sdk iphonesimulator -showBuildSettings -json \
  > /tmp/blackburn-debug-settings.json
```

Proposed `Config/Signing.xcconfig` (wire through the generator or `xcodeproj` once). Setting meanings are documented in Apple's [build settings reference](https://developer.apple.com/documentation/Xcode/build-settings-reference):

```xcconfig
DEVELOPMENT_TEAM = ABCDE12345
PRODUCT_BUNDLE_IDENTIFIER = com.example.blackburn
CODE_SIGN_STYLE = Automatic
CODE_SIGN_ENTITLEMENTS = App/App.entitlements
GENERATE_INFOPLIST_FILE = NO
INFOPLIST_FILE = App/Info.plist
```

### Inferences

- Recommended Blackburn baseline: Linux editor + XML plist/entitlements + committed `.xcconfig`; SSH commands in the macOS guest for Xcode queries/builds. SweetPad is optional if its extension host runs on macOS; a Linux-only installation does not meet its requirements. A VS Code Remote-SSH setup targeting macOS is an integration option, not a verified SweetPad configuration in this research.
- Do not migrate an existing Flutter project's project-generation ownership without necessity. Targeted Ruby edits plus existing Flutter/CocoaPods regeneration are a lower-risk starting point. For a new native project, XcodeGen gives a readable source of truth.
- A custom Blackburn interface could show resolved settings and signing status by wrapping existing CLIs, but would add maintenance without unlocking missing signing or configuration primitives.

### Gaps

- No extension was installed or smoke-tested on this laptop or guest. No primary evidence found here establishes SweetPad's Linux-to-macOS remote-extension compatibility.
- Linux execution of the Ruby gem is a reasonable candidate for plain file manipulation, but was not validated here. Run the known-compatible gem in macOS if a new project format or plist dependency causes problems.

## XcodeGen or Tuist: which configuration source should Blackburn use?

### Takeaway

Choose XcodeGen for a compact YAML/JSON app definition; choose Tuist when reusable Swift manifests and a larger project graph justify its additional system. Both remove the routine need to click through Xcode; neither replaces Apple's build tools or signing authority.

### Cited Findings

- XcodeGen generates `.xcodeproj` from directory structure and YAML/JSON specs; targets, configurations, schemes and settings are declared in text. `xcodegen generate` reads `project.yml`. Its README installation instructions request Xcode. [Maintainer README](https://github.com/yonaskolb/XcodeGen)
- Its changelog separately documents Linux generation support introduced in 2.19 and Linux Swift 5.4 build fixes in 2.23. Therefore “XcodeGen cannot run on Linux” would be too strong. These historical entries do not certify every current release on Blackburn. [Changelog](https://github.com/yonaskolb/XcodeGen/blob/master/CHANGELOG.md?plain=1)
- Its spec supports configuration files, target settings, Info.plist generation, entitlements and target attributes. [ProjectSpec](https://github.com/yonaskolb/XcodeGen/blob/master/Docs/ProjectSpec.md)
- Tuist uses `Project.swift` and `Workspace.swift`, compiles/caches manifests, and supports shared Swift helpers. [Manifests](https://tuist.dev/en/docs/guides/features/projects/manifests)
- Current Tuist installation documentation permits Linux via Mise but explicitly excludes Xcode-dependent commands such as `tuist generate`. Platform-independent commands are available. [Install guide](https://tuist.dev/en/docs/guides/install-tuist)
- Tuist's own migration runbook recommends baselining the build, inventorying target settings, moving them to `.xcconfig`, generating with `tuist generate --no-open`, then validating with `xcodebuild`. [Maintainer migration instructions](https://github.com/tuist/tuist/blob/main/skills/skills/migrate/SKILL.md)

Illustrative XcodeGen spec; files referenced below must exist. This wires existing plist/entitlements files instead of generating them; omit signing settings for a project that only needs unsigned simulator experiments. [Spec schema](https://github.com/yonaskolb/XcodeGen/blob/master/Docs/ProjectSpec.md)

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
        PRODUCT_BUNDLE_IDENTIFIER: com.example.blackburn
        INFOPLIST_FILE: App/Info.plist
        CODE_SIGN_ENTITLEMENTS: App/App.entitlements
```

### Inferences

- Recommended default: edit `project.yml` on Linux, run pinned XcodeGen in macOS via SSH, then `xcodebuild` there. Linux generation is an optional optimization to verify using the exact pinned Swift/XcodeGen versions and clean-checkout generation/build comparison.
- Treat generator files as canonical. Manual edits to generated `.pbxproj` are otherwise liable to disappear on the next generation. Prefer incremental adoption; generators are not required simply to change a bundle ID or add an entitlement.

### Gaps

- The current XcodeGen Linux build was not compiled here. No current-version Linux support claim beyond the cited historical changelog was validated.
- No timing or RAM savings were measured. A declarative generator does not establish that WindowServer can be terminated during Simulator rendering.

## How should plist, entitlements and capabilities be changed safely without Xcode?

### Takeaway

Plists are configuration data; an entitlement file is one input to signing. Enabling a capability can also require frameworks, Info.plist values, App ID/portal changes and a matching profile. Editing XML alone cannot grant an Apple service entitlement.

### Cited Findings

- Apple's capability workflow can modify entitlements, Info.plist, linked frameworks and signing assets; supported capabilities depend on platform and program membership. [Apple capability guide](https://developer.apple.com/documentation/xcode/adding-capabilities-to-your-app)
- `CODE_SIGN_ENTITLEMENTS` points to an entitlement file; `CODE_SIGN_STYLE` chooses automatic/manual asset acquisition. Info.plist content can also come from build settings when `GENERATE_INFOPLIST_FILE` is enabled. [Apple build settings](https://developer.apple.com/documentation/Xcode/build-settings-reference)
- Apple documents creating entitlements yourself and converting them to standard XML using `plutil`; Apple also demonstrates `PlistBuddy -c` for adding a Boolean entitlement. These utilities run independently of Keychain Access and the Xcode GUI. [Apple signing article](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/), [Apple helper-tool example](https://developer.apple.com/documentation/Xcode/embedding-a-helper-tool-in-a-sandboxed-app)
- Python's standard-library `plistlib` reads/writes XML and binary plists, providing a host-side option without Apple's command executables. It does not support the OpenStep project format, so use `xcodeproj` for `.pbxproj`. [Python documentation](https://docs.python.org/3/library/plistlib.html)

Proposed macOS recipes; run `man plutil` and `/usr/libexec/PlistBuddy -h` in the installed guest to check its exact options. Keep a source-control diff after changes:

```sh
plutil -lint App/Info.plist App/App.entitlements
plutil -p App/Info.plist
plutil -convert xml1 App/App.entitlements
plutil -replace CFBundleDisplayName -string Blackburn App/Info.plist
/usr/libexec/PlistBuddy -c 'Print :CFBundleDisplayName' App/Info.plist
```

`-replace` assumes the key exists; use `-insert` to add an absent key. `plutil -p` is human-oriented output; use a typed plist parser for automation. These command semantics should be checked with the installed system's help/man pages before production scripting.

### Inferences

- Capability-change checklist: consult that capability's Apple documentation; update source entitlements/Info.plist and framework dependencies; enable the matching App ID capability and regenerate/acquire profiles if required; build for the actual destination; inspect the resulting signed app. A `SystemCapabilities` project marker alone is not an entitlement authorization.
- Inspect the **built** Info.plist and code signature when debugging; generated values, substitutions and platform/signing rules can differ from committed source files. A plist syntax check establishes structure, not permission or provisioning correctness.
- Use a plist tree editor when visual inspection is useful. This already meets the narrow request for a graphical Info.plist/entitlements editor without maintaining a bespoke project inspector.

### Gaps

- No physical Apple device is available; device-only capability behavior cannot be validated by successful simulator builds.
- The illustrative edits were not applied to a live project. No claim is made that every entitlement is available through automated provisioning, or that GUI setup can authorize restricted capabilities.
