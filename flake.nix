{
  description = "glass: a reflection-first framework and go-in-go interpreter";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        glass = (pkgs.buildGoModule.override { go = pkgs.go_1_26; }) {
          pname = "glass";
          version = "0.1.0";
          src = self;
          vendorHash = null;
          subPackages = [ "cmd/gsgen" "examples/repl" "examples/weave" ];
          postInstall = ''
            mv $out/bin/repl $out/bin/glass
            mv $out/bin/weave $out/bin/glass-weave-demo
          '';
          meta = {
            description = "reflection-first framework and go-in-go interpreter";
            homepage = "https://github.com/dappermint/glass";
            mainProgram = "glass";
          };
        };
        default = glass;
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.system}.glass}/bin/glass";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [ go_1_26 gopls gotools ];
        };
      });
    };
}
