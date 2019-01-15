package posapi

import (
	"encoding/hex"
	"fmt"

	"context"
	"errors"
	"math/big"

	"github.com/wanchain/go-wanchain/consensus"
	"github.com/wanchain/go-wanchain/core/vm"
	"github.com/wanchain/go-wanchain/crypto"
	"github.com/wanchain/go-wanchain/internal/ethapi"
	"github.com/wanchain/go-wanchain/pos"
	"github.com/wanchain/go-wanchain/pos/posdb"
	"github.com/wanchain/go-wanchain/pos/postools"
	"github.com/wanchain/go-wanchain/pos/slotleader"
	"github.com/wanchain/go-wanchain/rpc"
)

type PosApi struct {
	chain   consensus.ChainReader
	backend ethapi.Backend
}

func APIs(chain consensus.ChainReader, backend ethapi.Backend) []rpc.API {
	return []rpc.API{{
		Namespace: "pos",
		Version:   "1.0",
		Service:   &PosApi{chain, backend},
		Public:    false,
	}}
}

func (a PosApi) Version() string {
	return "1.0"
}

func (a PosApi) GetSlotErrorCount() string {
	return postools.Uint64ToString(slotleader.ErrorCount)
}

func (a PosApi) GetSlotWarnCount() string {
	return postools.Uint64ToString(slotleader.WarnCount)
}

func (a PosApi) GetSlotLeadersByEpochID(epochID uint64) string {
	info := ""
	for i := uint64(0); i < pos.SlotCount; i++ {
		buf, err := posdb.GetDb().GetWithIndex(epochID, i, slotleader.SlotLeader)
		if err != nil {
			info += fmt.Sprintf("epochID:%d, index:%d, error:%s \n", err.Error())
		} else {
			info += fmt.Sprintf("epochID:%d, index:%d, pk:%s \n", epochID, i, hex.EncodeToString(buf))
		}
	}

	return info
}

func (a PosApi) GetEpochLeadersByEpochID(epochID uint64) string {
	info := ""

	type epoch interface {
		GetEpochLeaders(epochID uint64) [][]byte
	}

	selector := posdb.GetEpocherInst()

	if selector == nil {
		return "GetEpocherInst error"
	}

	epochLeaders := selector.(epoch).GetEpochLeaders(epochID)
	info += fmt.Sprintf("epoch leader count:%d \n", len(epochLeaders))

	for i := 0; i < len(epochLeaders); i++ {
		info += fmt.Sprintf("epochID:%d, index:%d, pk:%s \n", epochID, i, hex.EncodeToString(epochLeaders[i]))
	}

	return info
}

func (a PosApi) GetSmaByEpochID(epochID uint64) string {
	pks, err := slotleader.GetSlotLeaderSelection().GetSma(epochID)
	info := ""
	if err != nil {
		info = "" + err.Error() + "\n"
	}

	info += fmt.Sprintf("sma count:%d \n", len(pks))

	for i := 0; i < len(pks); i++ {
		info += fmt.Sprintf("epochID:%d, index:%d, SMA:%s \n", epochID, i, hex.EncodeToString(crypto.FromECDSAPub(pks[i])))
	}

	return info
}

func (a PosApi) GetRandomProposersByEpochID(epochID uint64) string {
	info := ""

	leaders := posdb.GetRBProposerGroup(epochID)
	info += fmt.Sprintf("random proposer count:%d \n", len(leaders))

	for i := 0; i < len(leaders); i++ {
		info += fmt.Sprintf("epochID:%d, index:%d, random proposer:%s \n", epochID, i, hex.EncodeToString(leaders[i].Marshal()))
	}

	return info
}

func (a PosApi) Random(epochId uint64, blockNr int64) (*big.Int, error) {
	state, _, err := a.backend.StateAndHeaderByNumber(context.Background(), rpc.BlockNumber(blockNr))
	if err != nil {
		return nil, err
	}

	r := vm.GetStateR(state, epochId)
	if r == nil {
		return nil, errors.New("no random number exists")
	}

	return r, nil
}
