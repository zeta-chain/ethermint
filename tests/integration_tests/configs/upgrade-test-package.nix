let
  pkgs = import ../../../nix { };
  fetchEthermint = rev: builtins.fetchTarball "https://github.com/zeta-chain/ethermint/archive/${rev}.tar.gz";
  released = pkgs.buildGo122Module rec {
    name = "ethermintd";
    src = fetchEthermint "1ebf85a354a08c856a8c2b97fc82f66f6e44f761";
    subPackages = [ "cmd/ethermintd" ];
    vendorHash = "sha256-vbhXi0SxPMc93j0kV6QNTpAxX+9w51iKYxMkZkixblo=";
    doCheck = false;
  };
  current = pkgs.callPackage ../../../. { };
in
pkgs.linkFarm "upgrade-test-package" [
  { name = "genesis"; path = released; }
  { name = "skd50"; path = current; }
]
