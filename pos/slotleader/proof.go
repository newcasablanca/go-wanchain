package slotleader

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"

	"github.com/wanchain/go-wanchain/crypto"
	"github.com/wanchain/go-wanchain/pos/posconfig"

	"github.com/wanchain/go-wanchain/log"
	"github.com/wanchain/go-wanchain/pos/util/convert"
	"github.com/wanchain/go-wanchain/rlp"
	"github.com/wanchain/pos/uleaderselection"
)

//ProofMes = [PK, Gt, skGt] []*PublicKey
//Proof = [e,z] []*big.Int
func (s *SlotLeaderSelection) VerifySlotProof(epochID uint64, slotID uint64, Proof []*big.Int, ProofMeg []*ecdsa.PublicKey) bool {
	_, errGenesis := s.getPreEpochLeadersPK(epochID)
	if epochID == 0 || errGenesis != nil {
		return s.verifySlotProofByGenesis(epochID, slotID, Proof, ProofMeg)
	}
	var epochLeadersPtrPre []*ecdsa.PublicKey
	epochLeadersPtrPre, err := s.getPreEpochLeadersPK(epochID)
	if err != nil {
		log.Warn(err.Error())
	}

	rbPtr, err := s.getRandom(epochID)
	if err != nil {
		log.Error(err.Error())
		return false
	}

	rbBytes := rbPtr.Bytes()

	// build stage two and proof info
	var validEpochLeadersIndex [posconfig.EpochLeaderCount]bool // true: can be used to slot leader false: can not be used to slot leader
	var stageTwoAlphaPKi [posconfig.EpochLeaderCount][posconfig.EpochLeaderCount]*ecdsa.PublicKey
	var stageTwoProof [posconfig.EpochLeaderCount][StageTwoProofCount]*big.Int //[0]: e; [1]:Z

	for i := 0; i < posconfig.EpochLeaderCount; i++ {
		validEpochLeadersIndex[i] = true
	}

	indexesSentTran, err := s.GetSlotLeaderStage2TxIndexes(epochID - 1)
	log.Debug("VerifySlotProof", "indexesSentTran", indexesSentTran)
	if err != nil {
		log.Error("VerifySlotProof", "indexesSentTran error", err.Error())
		return false
	}

	for i := 0; i < posconfig.EpochLeaderCount; i++ {
		if !indexesSentTran[i] {
			validEpochLeadersIndex[i] = false
			continue
		}
		alphaPki, proof, err := s.GetStage2TxAlphaPki(epochID-1, uint64(i))
		if err != nil {
			log.Debug("VerifySlotProof:GetStage2TxAlphaPki", "index", i, "error", err.Error())
			validEpochLeadersIndex[i] = false
			continue
		} else {
			for j := 0; j < posconfig.EpochLeaderCount; j++ {
				stageTwoAlphaPKi[i][j] = alphaPki[j]
			}
			for j := 0; j < StageTwoProofCount; j++ {
				stageTwoProof[i][j] = proof[j]
			}
		}
	}

	var hasValidTx bool
	hasValidTx = false
	log.Debug("VerifySlotProof:VerifyDleqProof", "validEpochLeadersIndex", validEpochLeadersIndex)
	for _, valid := range validEpochLeadersIndex {

		if valid {
			hasValidTx = true
			break
		}
	}
	if !hasValidTx {
		return s.verifySlotProofByGenesis(epochID, slotID, Proof, ProofMeg)
	}
	var publicKey *ecdsa.PublicKey
	publicKey = ProofMeg[0]

	publicKeyIndexes := make([]int, 0)
	for index, value := range epochLeadersPtrPre {
		if uleaderselection.PublicKeyEqual(publicKey, value) {
			publicKeyIndexes = append(publicKeyIndexes, index)
		}
	}

	// Verify skGt
	var skGtValid bool
	skGtValid = false
	for _, index := range publicKeyIndexes {

		smaPieces := make([]*ecdsa.PublicKey, 0)
		for i := 0; i < posconfig.EpochLeaderCount; i++ {
			if validEpochLeadersIndex[i] {
				smaPieces = append(smaPieces, stageTwoAlphaPKi[i][index])
			}
		}

		if len(smaPieces) == 0 {
			log.Error("len(smaPieces) == 0 in proof.go")
			return false
		}

		smaLen := new(big.Int).SetInt64(int64(len(smaPieces)))

		log.Debug("VerifySlotLeaderProofskGT aphaiPki", "index", index, "epochID", epochID, "slotID", slotID)
		log.Debug("VerifySlotLeaderProofskGT", "epochID", epochID, "slotID", slotID, "slotLeaderRb", rbBytes[:])

		smaPiecesHexStr := make([]string, 0)
		for _, value := range smaPieces {
			smaPiecesHexStr = append(smaPiecesHexStr, hex.EncodeToString(crypto.FromECDSAPub(value)))
		}
		log.Debug("VerifySlotLeaderProof", "epochID", epochID, "slotID", slotID, "smaPiecesHexStr", smaPiecesHexStr)

		var buffer bytes.Buffer
		buffer.Write(rbBytes[:])
		buffer.Write(convert.Uint64ToBytes(epochID))
		buffer.Write(convert.Uint64ToBytes(slotID))
		temp := buffer.Bytes()

		skGt := new(ecdsa.PublicKey)
		skGt.Curve = crypto.S256()

		for i := 0; i < len(epochLeadersPtrPre); i++ {
			tempHash := crypto.Keccak256(temp)
			tempBig := new(big.Int).SetBytes(tempHash)
			cstemp := new(big.Int).Mod(tempBig, smaLen)

			if i == 0 {
				skGt.X = new(big.Int).Set(smaPieces[cstemp.Int64()].X)
				skGt.Y = new(big.Int).Set(smaPieces[cstemp.Int64()].Y)
			} else {
				skGt.X, skGt.Y = uleaderselection.Wadd(skGt.X, skGt.Y, smaPieces[cstemp.Int64()].X, smaPieces[cstemp.Int64()].Y)
			}
			temp = tempHash
		}

		if uleaderselection.PublicKeyEqual(skGt, ProofMeg[2]) {
			skGtValid = true
			break
		}
	}
	if !skGtValid {
		log.Warn("VerifySlotLeaderProof Fail skGt is not valid", "epochID", epochID, "slotID", slotID)
		return false
	}
	log.Debug("VerifySlotLeaderProof skGt is verified successfully.", "epochID", epochID, "slotID", slotID)

	// verify slot leader proof
	return uleaderselection.VerifySlotLeaderProof(Proof[:], ProofMeg[:], epochLeadersPtrPre[:], rbBytes[:])
}

// PackSlotProof can make a pack info for header seal
func (s *SlotLeaderSelection) PackSlotProof(epochID uint64, slotID uint64, privKey *ecdsa.PrivateKey) ([]byte, error) {
	proofMeg, proof, err := s.getSlotLeaderProof(privKey, epochID, slotID)
	if err != nil {
		return nil, err
	}

	objToPack := &Pack{Proof: convert.BigIntArrayToByteArray(proof), ProofMeg: convert.PkArrayToByteArray(proofMeg)}

	buf, err := rlp.EncodeToBytes(objToPack)

	return buf, err
}

func (s *SlotLeaderSelection) GetInfoFromHeadExtra(epochID uint64, input []byte) ([]*big.Int, []*ecdsa.PublicKey, error) {
	var info Pack
	err := rlp.DecodeBytes(input, &info)
	if err != nil {
		log.Error("GetInfoFromHeadExtra rlp.DecodeBytes failed", "epochID", epochID, "input", hex.EncodeToString(input))
		return nil, nil, err
	}

	return convert.ByteArrayToBigIntArray(info.Proof), convert.ByteArrayToPkArray(info.ProofMeg), nil
}

func (s *SlotLeaderSelection) getSlotLeaderProofByGenesis(PrivateKey *ecdsa.PrivateKey, epochID uint64, slotID uint64) ([]*ecdsa.PublicKey, []*big.Int, error) {
	//1. SMA PRE
	smaPiecesPtr := s.smaGenesis
	epochLeadersPtrPre := s.epochLeadersPtrArrayGenesis
	rbBytes := s.randomGenesis.Bytes()

	log.Debug("getSlotLeaderProofByGenesis", "epochID", epochID, "slotID", slotID)
	log.Debug("getSlotLeaderProofByGenesis", "epochID", epochID, "slotID", slotID, "slotLeaderRb", hex.EncodeToString(rbBytes[:]))
	profMeg, proof, err := uleaderselection.GenerateSlotLeaderProof2(PrivateKey, smaPiecesPtr[:], epochLeadersPtrPre[:], rbBytes[:], slotID, epochID)
	return profMeg, proof, err
}

func (s *SlotLeaderSelection) getSlotLeaderProof(PrivateKey *ecdsa.PrivateKey, epochID uint64, slotID uint64) ([]*ecdsa.PublicKey, []*big.Int, error) {

	epochLeadersPtrPre, err := s.getPreEpochLeadersPK(epochID)
	if epochID == uint64(0) || err != nil {
		if err != nil {
			log.Warn("getSlotLeaderProof", "getPreEpochLeadersPK error", err.Error())
		}
		return s.getSlotLeaderProofByGenesis(PrivateKey, epochID, slotID)
	}

	//SMA PRE
	smaPiecesPtr, isGenesis, _ := s.getSMAPieces(epochID)
	if isGenesis {
		return s.getSlotLeaderProofByGenesis(PrivateKey, epochID, slotID)
	}

	//RB PRE
	var rbPtr *big.Int
	rbPtr, err = s.getRandom(epochID)
	if err != nil {
		log.Error("getSlotLeaderProof", "getRandom error", err.Error())
		return nil, nil, err
	}

	rbBytes := rbPtr.Bytes()

	log.Debug("getSlotLeaderProof", "epochID", epochID, "slotID", slotID)
	log.Debug("getSlotLeaderProof", "epochID", epochID, "slotID", slotID, "slotLeaderRb", hex.EncodeToString(rbBytes))

	epochLeadersHexStr := make([]string, 0)
	for _, value := range epochLeadersPtrPre {
		epochLeadersHexStr = append(epochLeadersHexStr, hex.EncodeToString(crypto.FromECDSAPub(value)))
	}
	log.Debug("getSlotLeaderProof", "epochID", epochID, "slotID", slotID, "epochLeadersHexStr", epochLeadersHexStr)

	smaPiecesHexStr := make([]string, 0)
	for _, value := range smaPiecesPtr {
		smaPiecesHexStr = append(smaPiecesHexStr, hex.EncodeToString(crypto.FromECDSAPub(value)))
	}
	log.Debug("getSlotLeaderProof", "epochID", epochID, "slotID", slotID, "smaPiecesHexStr", smaPiecesHexStr)

	profMeg, proof, err := uleaderselection.GenerateSlotLeaderProof2(PrivateKey, smaPiecesPtr, epochLeadersPtrPre, rbBytes[:], slotID, epochID)

	return profMeg, proof, err
}

func (s *SlotLeaderSelection) verifySlotProofByGenesis(epochID uint64, slotID uint64, Proof []*big.Int, ProofMeg []*ecdsa.PublicKey) bool {

	var publicKey *ecdsa.PublicKey
	publicKey = ProofMeg[0]

	publicKeyIndexes := make([]int, 0)
	for index, value := range s.epochLeadersPtrArrayGenesis {
		if uleaderselection.PublicKeyEqual(publicKey, value) {
			publicKeyIndexes = append(publicKeyIndexes, index)
		}
	}

	// Verify skGt
	var skGtValid bool
	skGtValid = false
	for _, index := range publicKeyIndexes {

		smaPieces := make([]*ecdsa.PublicKey, 0)
		for i := 0; i < posconfig.EpochLeaderCount; i++ {
			smaPieces = append(smaPieces, s.stageTwoAlphaPKiGenesis[i][index])
		}

		if len(smaPieces) == 0 {
			return false
		}
		smaLen := new(big.Int).SetInt64(int64(len(smaPieces)))
		log.Debug("verifySlotProofByGenesis", "epochID", epochID, "slotID", slotID, "slotLeaderRb", hex.EncodeToString(s.randomGenesis.Bytes()))
		log.Debug("verifySlotProofByGenesis aphaiPki", "index", index, "epochID", epochID, "slotID", slotID)

		var buffer bytes.Buffer
		buffer.Write(s.randomGenesis.Bytes())
		buffer.Write(convert.Uint64ToBytes(epochID))
		buffer.Write(convert.Uint64ToBytes(slotID))
		temp := buffer.Bytes()

		skGt := new(ecdsa.PublicKey)
		skGt.Curve = crypto.S256()

		for i := 0; i < posconfig.EpochLeaderCount; i++ {
			tempHash := crypto.Keccak256(temp)
			tempBig := new(big.Int).SetBytes(tempHash)
			cstemp := new(big.Int).Mod(tempBig, smaLen)

			//log.Debug("verifySlotProofByGenesis", "skGtPiece index", i, "alphaiPki index", cstemp.Int64())

			if i == 0 {
				skGt.X = new(big.Int).Set(smaPieces[cstemp.Int64()].X)
				skGt.Y = new(big.Int).Set(smaPieces[cstemp.Int64()].Y)
			} else {
				skGt.X, skGt.Y = uleaderselection.Wadd(skGt.X, skGt.Y, smaPieces[cstemp.Int64()].X, smaPieces[cstemp.Int64()].Y)
			}
			temp = tempHash
		}

		if uleaderselection.PublicKeyEqual(skGt, ProofMeg[2]) {
			skGtValid = true
			break
		}
	}
	if !skGtValid {
		log.Warn("verifySlotProofByGenesis Fail skGt is not valid", "epochID", epochID, "slotID", slotID)
		return false
	}
	log.Debug("verifySlotProofByGenesis skGt is verified successfully.", "epochID", epochID, "slotID", slotID)
	return uleaderselection.VerifySlotLeaderProof(Proof[:], ProofMeg[:], s.epochLeadersPtrArrayGenesis[:], s.randomGenesis.Bytes()[:])
}
