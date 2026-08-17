{
  buildGoModule,
  pname,
  subPackage,
}:
buildGoModule {
  inherit pname;
  version = "0.1.0";

  src = ./.;
  subPackages = [ subPackage ];

  vendorHash = null;

  ldflags = [
    "-s"
    "-w"
  ];
}
