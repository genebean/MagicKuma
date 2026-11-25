{
  description = "Distributed, declarative monitoring for Uptime Kuma";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      each = nixpkgs.lib.genAttrs systems;
    in {
      nixosModules = {
        client = import ./nix/client.nix { inherit self; };
        server = import ./nix/server.nix { inherit self; };
        autokuma = import ./nix/autokuma.nix;
        default.imports = [ self.nixosModules.client self.nixosModules.server self.nixosModules.autokuma ];
      };
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
      checks = each (system: { package = self.packages.${system}.default; } //
        nixpkgs.lib.optionalAttrs (nixpkgs.lib.hasSuffix "-linux" system) {
          publication = import ./tests/nixos.nix { pkgs = nixpkgs.legacyPackages.${system}; inherit self; };
        });
      devShells = each (system: { default = nixpkgs.legacyPackages.${system}.mkShell {
        packages = [ nixpkgs.legacyPackages.${system}.go ];
      }; });
    };
}
