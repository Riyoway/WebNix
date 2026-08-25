{
  description = "WebNix host and workspace NixOS configurations";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/56c02bc00adcf003215cc4bd996d6efaf4cff188";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
    in {
      nixosConfigurations.workspace = nixpkgs.lib.nixosSystem {
        inherit system;
        modules = [
          "${nixpkgs}/nixos/modules/virtualisation/incus-virtual-machine.nix"
          ./workspace.nix
        ];
      };

      nixosModules.host = import ./host.nix;

      devShells.${system}.default =
        let pkgs = import nixpkgs { inherit system; };
        in pkgs.mkShell {
          packages = with pkgs; [ go_1_26 nodejs_24 incus caddy oauth2-proxy ];
        };
    };
}
