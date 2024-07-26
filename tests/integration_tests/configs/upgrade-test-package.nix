let
  pkgs = import ../../../nix { };
  fetchEthermint = rev: builtins.fetchTarball "https://github.com/zeta-chain/ethermint/archive/${rev}.tar.gz";
  released = pkgs.buildGo119Module rec {
    name = "ethermintd";
    src = fetchEthermint "5db67f17e6a0a87ea580841be0266f898e3d63d9";
    subPackages = [ "cmd/ethermintd" ];
    vendorSha256 = "sha256-6EHCw0/Lo1JfDOEfsn/NufRco0zgebCo0hwwm5wJoFU=";
    doCheck = false;
  };
  current = pkgs.callPackage ../../../. { };
in
pkgs.linkFarm "upgrade-test-package" [
  { name = "genesis"; path = released; }
  { name = "integration-test-upgrade"; path = current; }
]
