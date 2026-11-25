{
  description = "Distributed, declarative monitoring for Uptime Kuma";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      each = nixpkgs.lib.genAttrs systems;
    in {
      packages = each (system: { default = nixpkgs.legacyPackages.${system}.buildGoModule {
        pname = "magickuma";
        version = "0.0.0-dev";
        src = ./.;
        vendorHash = null;
        subPackages = [ "cmd/magickuma" ];
        meta.mainProgram = "magickuma";
      }; });
      apps = each (system: { default = {
        type = "app";
        program = "${self.packages.${system}.default}/bin/magickuma";
      }; });
      checks = each (system: { package = self.packages.${system}.default; });
      devShells = each (system: { default = nixpkgs.legacyPackages.${system}.mkShell {
        packages = [ nixpkgs.legacyPackages.${system}.go ];
      }; });
    };
}
