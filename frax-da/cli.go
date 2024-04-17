package fraxda

import (
	"fmt"
	"net/url"

	"github.com/urfave/cli/v2"

	opservice "github.com/ethereum-optimism/optimism/op-service"
)

const (
	DaRpcFlagName = "da.rpc"
)

var (
	defaultDaRpc = "https://da-rpc.mainnet.frax.com"
)

func CLIFlags(envPrefix string) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    DaRpcFlagName,
			Usage:   "DA endpoint",
			Value:   defaultDaRpc,
			EnvVars: opservice.PrefixEnvVar(envPrefix, "DA_RPC"),
		},
	}
}

type Config struct {
	DaRpc string
}

func (c Config) Check() error {
	if c.DaRpc == "" {
		c.DaRpc = defaultDaRpc
	}

	if _, err := url.Parse(c.DaRpc); err != nil {
		return fmt.Errorf("invalid da rpc url: %w", err)
	}

	return nil
}

type CLIConfig struct {
	DaRpc string
}

func (c CLIConfig) Check() error {
	if c.DaRpc == "" {
		c.DaRpc = defaultDaRpc
	}

	if _, err := url.Parse(c.DaRpc); err != nil {
		return fmt.Errorf("invalid da rpc url: %w", err)
	}

	return nil
}

func NewCLIConfig() CLIConfig {
	return CLIConfig{
		DaRpc: defaultDaRpc,
	}
}

func ReadCLIConfig(ctx *cli.Context) CLIConfig {
	return CLIConfig{
		DaRpc: ctx.String(DaRpcFlagName),
	}
}
