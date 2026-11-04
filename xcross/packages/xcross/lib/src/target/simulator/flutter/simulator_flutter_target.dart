import 'package:cli_kit/shared/platform/platform_host.dart';
import 'package:darwin_sdk_kit/target/shared/ios_target.dart';
import 'package:meta/meta.dart';
import 'package:xcross/src/target/shared/flutter/flutter_target_build_policy.dart';
import 'package:xcross/src/target/shared/flutter/ios_plist_metadata.dart';

@internal
final class SimulatorFlutterTarget<T extends PlatformHostInterface>
    implements FlutterTargetBuildPolicy<T> {
  const SimulatorFlutterTarget(this.target);
  @override
  final SimulatorTargetInterface<T> target;
  @override
  String outputDirectory(String projectRoot) => target.host.paths.context.join(
    projectRoot,
    'build',
    'xcross-ios-simulator',
    target.buildPlatform.swiftSdkTriple.startsWith('x86_64-') ? 'x86_64' : null,
  );
  @override
  String buildDirectory(String projectRoot, String name) =>
      target.host.paths.context.join(outputDirectory(projectRoot), name);
  @override
  String get engineArtifact => 'ios';
  @override
  bool get supportsPrecompiledModes => false;
  @override
  List<String> get engineSliceIdentifiers => [
    'ios-arm64_x86_64-simulator',
    if (target.buildPlatform.swiftSdkTriple.startsWith('x86_64-'))
      'ios-x86_64-simulator'
    else
      'ios-arm64-simulator',
  ];
  @override
  String selectEngineSlice(String xcframework) => selectFlutterEngineSlice(
    xcframework,
    engineSliceIdentifiers,
    paths: target.host.paths.context,
    fileSystem: target.host.fileSystem,
  );
  @override
  bool matchesLibraryVariant(String? variant) => variant == 'simulator';
  @override
  String transformPlist(String xml, {String? sdkName}) =>
      IosPlistMetadata.overwrite(
        xml.replaceAll('<string>arm64</string>', ''),
        platform: target.buildPlatform,
        sdkName: sdkName,
      );
  @override
  String get sanitizerRuntimeLibrary => 'libclang_rt.iossim.a';
  @override
  String get workspaceSuffix =>
      target.buildPlatform.swiftSdkTriple.startsWith('x86_64-')
      ? '-simulator-x86_64'
      : '-simulator';
  @override
  String get binaryArtifactDirectory =>
      target.buildPlatform.swiftSdkTriple.startsWith('x86_64-')
      ? 'binary-artifacts-simulator-x86_64-v1'
      : 'binary-artifacts-simulator-v1';
}
