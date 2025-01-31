local config = import 'default.jsonnet';

config {
  'ethermint_9000-1'+: {
    'app-config'+: {
      'minimum-gas-prices': '100000000000aphoton',
    },
    genesis+: {
      consensus_params: {
        block: {
          max_bytes: '1048576',
          max_gas: '81500000',
        },
      },
      app_state+: {
        feemarket+: {
          params+: {
            base_fee:: super.base_fee,
          },
        },
        gov+: {
          params+: {
            expedited_voting_period:: super.expedited_voting_period,
          },
        },
      },
    },
  },
}
