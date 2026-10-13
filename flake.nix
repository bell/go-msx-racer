{
  description = "Build and play go-msx-racer with locally extracted ROM assets";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          linuxLibraries = with pkgs; [
            alsa-lib
            alsa-plugins
            libGL
            libX11
            libXcursor
            libXi
            libXinerama
            libXrandr
            libXxf86vm
            libXext
            libXrender
          ];
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [ go_1_27 pkg-config ];
            buildInputs = pkgs.lib.optionals pkgs.stdenv.hostPlatform.isLinux linuxLibraries;

            # Ebitengine also loads graphics libraries dynamically at runtime.
            shellHook = pkgs.lib.optionalString pkgs.stdenv.hostPlatform.isLinux ''
              export LD_LIBRARY_PATH="${pkgs.lib.makeLibraryPath linuxLibraries}''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
              if [ -d /run/opengl-driver/lib ]; then
                export LD_LIBRARY_PATH="/run/opengl-driver/lib:$LD_LIBRARY_PATH"
              fi
              export ALSA_PLUGIN_DIR="''${ALSA_PLUGIN_DIR:-${pkgs.alsa-plugins}/lib/alsa-lib}"
            '';
          };
        });
    };
}
