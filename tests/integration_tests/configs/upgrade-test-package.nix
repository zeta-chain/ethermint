let
  pkgs = import ../../../nix { };
  fetchEthermint = rev: builtins.fetchTarball "https://github.com/zeta-chain/ethermint/archive/${rev}.tar.gz";
  released = pkgs.buildGo122Module rec {
    name = "ethermintd";
    src = fetchEthermint "1ebf85a354a08c856a8c2b97fc82f66f6e44f761";
    subPackages = [ "cmd/ethermintd" ];
    vendorHash = "sha256-6EHCw0/Lo1JfDOEfsn/NufRco0zgebCo0hwwm5wJoFU=";
    doCheck = false;
  };
  current = pkgs.callPackage ../../../. { };
in
pkgs.linkFarm "upgrade-test-package" [
  { name = "genesis"; path = released; }
  { name = "skd50"; path = current; }
]
