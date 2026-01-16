{
  description = "agent-sandbox - CLI tool that runs commands inside a filesystem sandbox using bwrap";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
      version = "0.1.0";
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "agent-sandbox";
        inherit version;
        src = ./.;
        vendorHash = "sha256-EJebrUlmzwSYD6EjddHiFDd2aJ2+ikrEr6qXFrQRV1U=";

        nativeBuildInputs = [ pkgs.makeWrapper ];

        # Tests need bwrap which can't run inside nix's sandbox
        doCheck = false;

        ldflags = [
          "-X main.version=${version}"
          "-X main.commit=${self.shortRev or "dirty"}"
          "-X main.date=1970-01-01_00:00:00"
        ];

        postInstall = ''
          wrapProgram $out/bin/agent-sandbox \
            --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.bubblewrap ]}
        '';

        meta = with pkgs.lib; {
          description = "CLI tool that runs commands inside a filesystem sandbox using bwrap";
          license = {
            fullName = "Proprietary";
            free = false;
          };
          platforms = [ "x86_64-linux" ];
        };
      };
    };
}
