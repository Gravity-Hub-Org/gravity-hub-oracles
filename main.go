package main

import (
	"context"
	"eth-nebula-oracles/config"
	"eth-nebula-oracles/server"
	"eth-nebula-oracles/signer"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/syndtr/goleveldb/leveldb"
)

const (
	defaultConfigFileName = "config.json"
	defaultDbPath         = "db"
	defaultHost           = "127.0.0.1:8080"
)

func main() {
	var host, confFileName, seed, dbPath string
	flag.StringVar(&host, "host", defaultHost, "set host")
	flag.StringVar(&seed, "seed", "", "set seed")
	flag.StringVar(&confFileName, "config", defaultConfigFileName, "set config path")
	flag.StringVar(&dbPath, "db", defaultDbPath, "set db path")
	flag.Parse()

	cfg, err := config.Load(confFileName)
	if err != nil {
		panic(err)
	}

	db, err := leveldb.OpenFile(dbPath+"/"+"prices", nil)
	defer db.Close()

	ctxWithCancel, cancelCtxFunc := context.WithCancel(context.Background())
	go server.StartServer(host, ctxWithCancel, db)

	go signer.StartSigner(cfg, seed, cfg.ChainId[0], ctxWithCancel, pr, db)

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		fmt.Println(sig)
		done <- true
	}()

	fmt.Println("Started...")
	<-done

	cancelCtxFunc()

	fmt.Println("Stopped...")
}
