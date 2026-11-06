{
  lib,
  buildGoModule,
  pname,
  subPackage,
}:
buildGoModule {
  inherit pname;
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.fileFilter (file: file.hasExt "go" || file.name == "go.mod") ./.;
  };
  subPackages = [ subPackage ];

  vendorHash = null;

  ldflags = [
    "-s"
    "-w"
  ];

  # subPackages selects the executable, but both binaries also rely on the
  # internal packages. Exercise those tests rather than only the command tests.
  checkPhase = # sh
    ''
      runHook preCheck
      go test -p "$NIX_BUILD_CORES" ./...
      runHook postCheck
    '';

  meta = {
    description = "Upload and serve files for agents";
    homepage = "https://github.com/billy4479/agent-up";
    license = lib.licenses.mit;
    mainProgram = pname;
  };
}
