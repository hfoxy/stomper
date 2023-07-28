package main

import (
	"flag"
	"log"
	"net/http"
	"stomper"
)

var version = "v0.0.1"

var addr = flag.String("addr", "localhost:8448", "http service address")
var compression = flag.String("compression", "true", "enable compression")

func healthHandler(writer http.ResponseWriter, _ *http.Request) {
	_, err := writer.Write([]byte("ok"))
	if err != nil {
		return
	}
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	comp := *compression
	stompServer := stomper.StompServer{
		Compression: comp == "true",
	}

	stompServer.Setup()
	stompServer.Sugar.Infof("staring stomper %s...", version)

	http.HandleFunc("/wss/websocket", stompServer.WssHandler)
	http.HandleFunc("/health", healthHandler)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
