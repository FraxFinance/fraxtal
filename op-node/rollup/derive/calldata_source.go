package derive

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	fraxda "github.com/ethereum-optimism/optimism/frax-da"
	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/ethereum-optimism/optimism/op-service/eth"
)

var daClient *fraxda.DAClient

func SetDAClient(c *fraxda.DAClient) error {
	if daClient != nil {
		return errors.New("da client already configured")
	}
	daClient = c
	return nil
}

type DataIter interface {
	Next(ctx context.Context) (eth.Data, error)
}

type L1TransactionFetcher interface {
	InfoAndTxsByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, types.Transactions, error)
}

// DataSourceFactory readers raw transactions from a given block & then filters for
// batch submitter transactions.
// This is not a stage in the pipeline, but a wrapper for another stage in the pipeline
type DataSourceFactory struct {
	log     log.Logger
	dsCfg   DataSourceConfig
	fetcher L1TransactionFetcher
}

func NewDataSourceFactory(log log.Logger, cfg *rollup.Config, fetcher L1TransactionFetcher) *DataSourceFactory {
	return &DataSourceFactory{log: log, dsCfg: DataSourceConfig{l1Signer: cfg.L1Signer(), batchInboxAddress: cfg.BatchInboxAddress}, fetcher: fetcher}
}

// OpenData returns a DataIter. This struct implements the `Next` function.
func (ds *DataSourceFactory) OpenData(ctx context.Context, id eth.BlockID, batcherAddr common.Address) (DataIter, error) {
	return NewDataSource(ctx, ds.log, ds.dsCfg, ds.fetcher, id, batcherAddr)
}

// DataSourceConfig regroups the mandatory rollup.Config fields needed for DataFromEVMTransactions.
type DataSourceConfig struct {
	l1Signer          types.Signer
	batchInboxAddress common.Address
}

// DataSource is a fault tolerant approach to fetching data.
// The constructor will never fail & it will instead re-attempt the fetcher
// at a later point.
type DataSource struct {
	// Internal state + data
	open bool
	data []eth.Data
	// Required to re-attempt fetching
	id      eth.BlockID
	dsCfg   DataSourceConfig
	fetcher L1TransactionFetcher
	log     log.Logger

	batcherAddr common.Address
}

// NewDataSource creates a new calldata source. It suppresses errors in fetching the L1 block if they occur.
// If there is an error, it will attempt to fetch the result on the next call to `Next`.
func NewDataSource(ctx context.Context, log log.Logger, dsCfg DataSourceConfig, fetcher L1TransactionFetcher, block eth.BlockID, batcherAddr common.Address) (DataIter, error) {
	_, txs, err := fetcher.InfoAndTxsByHash(ctx, block.Hash)

	if err != nil {
		return &DataSource{
			open:        false,
			id:          block,
			dsCfg:       dsCfg,
			fetcher:     fetcher,
			log:         log,
			batcherAddr: batcherAddr,
		}, nil
	} else {
		data, err := DataFromEVMTransactions(dsCfg, batcherAddr, txs, log.New("origin", block))
		if err != nil {
			return &DataSource{
				open:        false,
				id:          block,
				dsCfg:       dsCfg,
				fetcher:     fetcher,
				log:         log,
				batcherAddr: batcherAddr,
			}, err
		}
		return &DataSource{
			open: true,
			data: data,
		}, nil
	}
}

// Next returns the next piece of data if it has it. If the constructor failed, this
// will attempt to reinitialize itself. If it cannot find the block it returns a ResetError
// otherwise it returns a temporary error if fetching the block returns an error.
func (ds *DataSource) Next(ctx context.Context) (eth.Data, error) {
	if !ds.open {
		if _, txs, err := ds.fetcher.InfoAndTxsByHash(ctx, ds.id.Hash); err == nil {
			ds.open = true
			ds.data, err = DataFromEVMTransactions(ds.dsCfg, ds.batcherAddr, txs, log.New("origin", ds.id))
			if err != nil {
				// already wrapped
				return nil, err
			}
		} else if errors.Is(err, ethereum.NotFound) {
			return nil, NewResetError(fmt.Errorf("failed to open calldata source: %w", err))
		} else {
			return nil, NewTemporaryError(fmt.Errorf("failed to open calldata source: %w", err))
		}
	}
	if len(ds.data) == 0 {
		return nil, io.EOF
	} else {
		data := ds.data[0]
		ds.data = ds.data[1:]
		return data, nil
	}
}

// DataFromEVMTransactions filters all of the transactions and returns the calldata from transactions
// that are sent to the batch inbox address from the batch sender address.
// This will return an empty array if no valid transactions are found.
func DataFromEVMTransactions(dsCfg DataSourceConfig, batcherAddr common.Address, txs types.Transactions, log log.Logger) ([]eth.Data, error) {
	var out []eth.Data
	for j, tx := range txs {
		if to := tx.To(); to != nil && *to == dsCfg.batchInboxAddress {
			seqDataSubmitter, err := dsCfg.l1Signer.Sender(tx) // optimization: only derive sender if To is correct
			if err != nil {
				log.Warn("tx in inbox with invalid signature", "index", j, "txHash", tx.Hash(), "err", err)
				continue // bad signature, ignore
			}
			// some random L1 user might have sent a transaction to our batch inbox, ignore them
			if seqDataSubmitter != batcherAddr {
				log.Warn("tx in inbox with unauthorized submitter", "index", j, "txHash", tx.Hash(), "err", err)
				continue // not an authorized batch submitter, ignore
			}

			data := tx.Data()
			switch len(data) {
			case 0:
				out = append(out, data)
			default:
				switch data[0] {
				case fraxda.DerivationVersionFraxDa:
					log.Info("fraxda: requesting data", "id", hex.EncodeToString(data))
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					data, err := daClient.Read(ctx, data[1:])
					cancel()
					if err != nil {
						return nil, NewResetError(fmt.Errorf("fraxda: failed to fetch data for id %s: %w", hex.EncodeToString(data), err))
					}
					out = append(out, data)
				case fraxda.DerivationVersionCelestia:
					log.Info("fraxda: requesting old celestia data", "id", hex.EncodeToString(data))
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					data, err := daClient.ReadCelestia(ctx, hex.EncodeToString(data[1:]))
					cancel()
					if err != nil {
						return nil, NewResetError(fmt.Errorf("fraxda: failed to fetch celestia data for id %s: %w", hex.EncodeToString(data), err))
					}
					out = append(out, data)
				default:
					out = append(out, data)
					log.Info("fraxda: using eth fallback")
				}
			}
		}
	}
	return out, nil
}
