package driver

import (
	fraxda "github.com/ethereum-optimism/optimism/frax-da"
	"github.com/ethereum-optimism/optimism/op-node/rollup/derive"
)

func SetDAClient(cfg fraxda.Config) error {
	client, err := fraxda.NewDAClient(cfg.DaRpc)
	if err != nil {
		return err
	}
	return derive.SetDAClient(client)
}
