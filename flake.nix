{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            gopls
            golangci-lint
            air
          ];

          nativeBuildInputs = with pkgs; [ go ];
        };

        packages = rec {
          agent-up = pkgs.callPackage ./package.nix {
            pname = "agent-up";
            subPackage = "cmd/agent-up";
          };
          agent-up-server = pkgs.callPackage ./package.nix {
            pname = "agent-up-server";
            subPackage = "cmd/agent-up-server";
          };
          client = agent-up;
          server = agent-up-server;
          default = agent-up;
        };
      }
    );
}
