package main

import (
	"flag"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"stomper"
)

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
	stompServer := stomper.Server{
		Compression: comp == "true",
	}

	stompServer.AddConnectHandler(func(conn *websocket.Conn, request *http.Request, message *stomper.StompMessage) bool {
		stompServer.Sugar.Infof("[connect] %s", conn.RemoteAddr())
		return true
	})

	stompServer.AddDisconnectHandler(func(conn *websocket.Conn) {
		stompServer.Sugar.Infof("[disconnect] %s", conn.RemoteAddr())
	})

	stompServer.AddSubscribeHandler(func(conn *websocket.Conn, s string) bool {
		stompServer.Sugar.Infof("[%s] [%s] subscribe", conn.RemoteAddr(), s)
		return true
	})

	stompServer.AddUnsubscribeHandler(func(conn *websocket.Conn, s string) {
		stompServer.Sugar.Infof("[%s] [%s] unsubscribe", conn.RemoteAddr(), s)
	})

	stompServer.AddMessageHandler(func(conn *websocket.Conn, s string, message *stomper.StompMessage) {
		stompServer.Sugar.Infof("[%s] [%s] recv: %s", conn.RemoteAddr(), s, string(*message.Body))
	})

	stompServer.Setup()
	http.HandleFunc("/wss/websocket", stompServer.WssHandler)
	http.HandleFunc("/health", healthHandler)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
