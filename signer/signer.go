package signer

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/json"
	"eth-nebula-oracles/config"
	"eth-nebula-oracles/contracts"
	"eth-nebula-oracles/signer/provider"
	"eth-nebula-oracles/storage"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum/crypto/secp256k1"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"

	"eth-nebula-oracles/models"
	"strconv"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
)

const (
	BftCoefficient = 3
)

func StartSigner(cfg config.Config, priveKeyString string, ctx context.Context, db *leveldb.DB) error {
	ethClient, err := ethclient.DialContext(ctx, cfg.NodeURL)
	if err != nil {
		return err
	}

	ethContractAddress := common.Address{}
	hexAddress, err := hexutil.Decode(cfg.ContractAddress)
	if err != nil {
		return err
	}
	ethContractAddress.SetBytes(hexAddress)

	ethContract, err := contracts.NewNebula(ethContractAddress, ethClient)
	if err != nil {
		return err
	}

	privBytes, err := hexutil.Decode(priveKeyString)
	if err != nil {
		return err
	}
	ethPrivateKey := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: secp256k1.S256(),
		},
		D: new(big.Int),
	}
	ethPrivateKey.D.SetBytes(privBytes)
	ethPrivateKey.PublicKey.X, ethPrivateKey.PublicKey.Y = ethPrivateKey.PublicKey.Curve.ScalarBaseMult(privBytes)

	isTimeout := false
	for true {
		var err error
		isTimeout, err = HandleHeight(cfg, db, ethContract, ethClient, ethPrivateKey, ctx, isTimeout)
		if err != nil {
			fmt.Printf("Error: %s \n", err.Error())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

func HandleHeight(cfg config.Config, db *leveldb.DB, contract *contracts.Nebula, client *ethclient.Client,
	privKey *ecdsa.PrivateKey, ctx context.Context, isTimeout bool) (bool, error) {
	transactOpt := bind.NewKeyedTransactor(privKey)
	height, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return false, err
	}

	pulse, err := contract.Pulses(nil, height.Number)
	if err != err {
		return false, err
	}

	values, signs, err := getSigns(cfg.Ips, height.Number.Uint64(), contract)
	if err != err {
		return false, err
	}
	if len(signs) >= BftCoefficient {
		oracleCount, err := contract.OraclesCount(nil)
		if err != err {
			return false, err
		}
		for i := 0; i < int(oracleCount.Uint64()); i++ {

		}
		//contract.ConfirmData(transactOpt)
		//fmt.Printf("Tx finilize: %s \n", tx.ID)
	}

	if pulse.Cmp(big.NewInt(0)) != 0 {
		return false, nil
	}

	err = addNewData(height.Number.Uint64(), transactOpt, db, contract, privKey)
	if err != err {
		return false, err
	}

	if !isTimeout {
		time.Sleep(time.Duration(cfg.Timeout) * time.Second)
		return true, nil
	}

	time.Sleep(time.Duration(cfg.Timeout) * time.Second)
	return false, nil
}

func addNewData(height uint64, transactOpt *bind.TransactOpts, db *leveldb.DB, contract *contracts.Nebula, privKey *ecdsa.PrivateKey) error {
	priceProvider := provider.BinanceProvider{}
	signedPrice, err := storage.GetKeystore(db, height)
	if err != nil && err != leveldb.ErrNotFound {
		fmt.Printf("Error: %s \n", err.Error())
	} else {
		newNotConvertedPrice, err := priceProvider.PriceNow()
		if err != nil {
			return err
		}

		newPrice := uint64(newNotConvertedPrice * 100)
		prefix, err := contract.Name(nil)
		if err != nil {
			return err
		}
		byteMsg := make([]byte, 0)
		byteMsg = append(byteMsg, []byte(prefix)...)
		binary.BigEndian.PutUint64(byteMsg, newPrice)
		sig, err := signEthMsg(byteMsg, privKey)
		if err != nil {
			return err
		}

		signedText := models.SignedText{
			Message:   hexutil.Encode(byteMsg),
			PublicKey: transactOpt.From.String(),
			Signature: hexutil.Encode(sig),
		}
		err = storage.PutKeystore(db, height, signedText)
		if err != nil {
			return err
		}
	}
	fmt.Printf("Price by {%d}: {%s} \n", height, signedPrice.Message)
	return nil
}

func getSigns(ips []string, height uint64, contract *contracts.Nebula) (map[string]*big.Int, map[string]string, error) {
	values := make(map[string]*big.Int)
	signs := make(map[string]string)
	prefix, err := contract.Name(nil)
	if err != nil {
		return nil, nil, err
	}
	for _, ip := range ips {
		var client = &http.Client{Timeout: 10 * time.Second}
		res, err := client.Get(ip + "/api/price?height=" + strconv.Itoa(int(height)))
		if err != nil {
			fmt.Printf("Http error %s: %s \n", ip, err.Error())
			continue
		}

		if res.StatusCode == 200 {
			var result models.SignedText
			err = json.NewDecoder(res.Body).Decode(&result)
			if err != nil {
				fmt.Printf("Parse error %s: %s \n", ip, err.Error())
				continue
			}
			if result.Message == "" {
				fmt.Printf("Oracle (%s) %s: %s \n", ip, result.PublicKey, "empty msg")
				continue
			}
			msg, err := hexutil.Decode(result.Message)
			if err != nil {
				return nil, nil, err
			}

			bytesPrefix, err := hexutil.Decode(prefix)
			value := binary.BigEndian.Uint32(msg[len(bytesPrefix):])
			values[result.PublicKey] = big.NewInt(int64(value))
			signs[result.PublicKey] = result.Signature
			fmt.Printf("Oracle (%s) %s: %s \n", ip, result.PublicKey, values[result.PublicKey])
		}
		if res.Body != nil {
			if err := res.Body.Close(); err != nil {
				fmt.Printf("Http close error %s: %s \n", ip, err.Error())
				continue
			}
		}
	}
	return values, signs, nil
}

func signEthMsg(message []byte, privKey *ecdsa.PrivateKey) ([]byte, error) {
	validationMsg := "\x19Ethereum Signed Message:\n" + strconv.Itoa(len(message))
	validationHash := crypto.Keccak256(append([]byte(validationMsg), message[:]...))
	return crypto.Sign(validationHash, privKey)
}
