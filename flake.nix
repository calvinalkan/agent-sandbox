{
  description = "agent-sandbox - CLI tool that runs commands inside a filesystem sandbox using bwrap";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "agent-sandbox";
        version = "0.1.0";
        src = ./.;
        vendorHash = "sha256-EJebrUlmzwSYD6EjddHiFDd2aJ2+ikrEr6qXFrQRV1U=";

        nativeBuildInputs = [ pkgs.bash pkgs.gnumake ];

        buildPhase = ''
          make build
        '';

        installPhase = ''
          mkdir -p $out/bin
          cp agent-sandbox $out/bin/
        '';

        doCheck = false;

        meta = {
          description = "CLI tool that runs commands inside a filesystem sandbox using bwrap";
          platforms = [ "x86_64-linux" ];
        };
      };

      checks.${system}.default = pkgs.buildGoModule {
        pname = "agent-sandbox";
        version = "0.1.0";
        src = ./.;
        vendorHash = "sha256-EJebrUlmzwSYD6EjddHiFDd2aJ2+ikrEr6qXFrQRV1U=";

        nativeBuildInputs = [ pkgs.bash pkgs.gnumake pkgs.bubblewrap ];

        buildPhase = ''
          make build
        '';

        checkPhase = ''
          make test
        '';

        installPhase = ''
          mkdir -p $out/bin
          cp agent-sandbox $out/bin/
        '';

        doCheck = true;
      };
    };
}
