{
  description = "go";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    nixpkgs-darwin-x86_64.url = "github:NixOS/nixpkgs/nixpkgs-26.05-darwin";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    nixpkgs,
    nixpkgs-darwin-x86_64,
    flake-utils,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs =
          import
          (
            if system == "x86_64-darwin"
            then nixpkgs-darwin-x86_64
            else nixpkgs
          )
          {inherit system;};
      in {
        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.delve
            pkgs.go
            pkgs.go-mockery
            pkgs.gofumpt
            pkgs.goimports-reviser
            pkgs.golangci-lint
            pkgs.gopls
            pkgs.gotestsum
            pkgs.gotools
            pkgs.just
          ];
        };
      }
    );
}
