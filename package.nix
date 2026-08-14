{
  lib,
  buildGoModule,
}:
buildGoModule {
  pname =
    if builtins.pathExists ./go.mod then
      let
        moduleLine = builtins.head (lib.splitString "\n" (builtins.readFile ./go.mod));
        modulePath = lib.removePrefix "module " moduleLine;
      in
      builtins.baseNameOf modulePath
    else
      "app";
  version = "0.1.0";

  src = ./.;
  subPackages = [ "." ];

  vendorHash = lib.fakeHash;

  ldflags = [
    "-s"
    "-w"
  ];
}
