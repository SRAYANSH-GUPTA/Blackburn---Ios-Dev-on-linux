import 'package:darwin_sdk_kit/target/shared/ios_build_platform.dart';

final class SimulatorBuildPlatform implements IosBuildPlatformInterface {
  const SimulatorBuildPlatform({this.architecture = 'arm64'})
    : assert(
        architecture == 'arm64' || architecture == 'x86_64',
        'Use arm64 or x86_64',
      );
  final String architecture;
  @override
  String get sdkName => 'iphonesimulator';
  @override
  String get platformName => 'iPhoneSimulator';
  @override
  String get swiftSdkTriple => '$architecture-apple-ios-simulator';
  @override
  String get linkerPlatform => 'ios-simulator';
  @override
  String buildTriple(String minimumVersion) =>
      '$architecture-apple-ios$minimumVersion-simulator';
  @override
  String minimumVersionFlag(String minimumVersion) =>
      '-mios-simulator-version-min=$minimumVersion';
}
