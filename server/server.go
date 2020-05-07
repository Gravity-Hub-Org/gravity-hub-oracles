package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"eth-nebula-oracles/storage"
	"strconv"

	"github.com/syndtr/goleveldb/leveldb"
)

var db *leveldb.DB

func StartServer(host string, ctx context.Context, newDb *leveldb.DB) {
	for {
		db = newDb
		http.HandleFunc("/api/price/", handleGetPrice)
		http.ListenAndServe(host, nil)
		println("Restart server")

		select {
		case <-ctx.Done():
			return
		default:

		}
	}
}

func handleGetPrice(w http.ResponseWriter, r *http.Request) {
	keys, ok := r.URL.Query()["height"]

	if !ok || len(keys[0]) < 1 {
		http.Error(w, "Url Param 'height' is missing", 404)
		return
	}

	height, err := strconv.Atoi(keys[0])
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	text, err := storage.GetKeystore(db, uint64(height))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	result, err := json.Marshal(text)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	fmt.Fprintf(w, string(result))
}
