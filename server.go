package stomper

import (
	"fmt"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"os"
)

type StompSubscribeHandler func(*websocket.Conn, string)
type StompUnsubscribeHandler func(*websocket.Conn, string)
type StompConnectHandler func(*websocket.Conn, *http.Request, *StompMessage)
type StompDisconnectHandler func(*websocket.Conn)
type StompMessageHandler func(*websocket.Conn, string, *StompMessage)

type StompServer struct {
	Sugar               *zap.SugaredLogger
	Compression         bool
	setup               bool
	upgrader            websocket.Upgrader
	messageHandlers     []StompMessageHandler
	subscribeHandlers   []StompSubscribeHandler
	unsubscribeHandlers []StompUnsubscribeHandler
	connectHandlers     []StompConnectHandler
	disconnectHandlers  []StompDisconnectHandler
	clients             []*websocket.Conn
	subscriptions       map[string][]*websocket.Conn
}

func (server *StompServer) AddMessageHandler(handler StompMessageHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add message handler after server is setup")
	}

	server.messageHandlers = append(server.messageHandlers, handler)
	return nil
}

func (server *StompServer) AddSubscribeHandler(handler StompSubscribeHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add subscribe handler after server is setup")
	}

	server.subscribeHandlers = append(server.subscribeHandlers, handler)
	return nil
}

func (server *StompServer) AddUnsubscribeHandler(handler StompUnsubscribeHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add unsubscribe handler after server is setup")
	}

	server.unsubscribeHandlers = append(server.unsubscribeHandlers, handler)
	return nil
}

func (server *StompServer) AddConnectHandler(handler StompConnectHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add connect handler after server is setup")
	}

	server.connectHandlers = append(server.connectHandlers, handler)
	return nil
}

func (server *StompServer) AddDisconnectHandler(handler StompDisconnectHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add disconnect handler after server is setup")
	}

	server.disconnectHandlers = append(server.disconnectHandlers, handler)
	return nil
}

func (server *StompServer) Setup() {
	sugar := server.Sugar
	if sugar == nil {
		sugar = logInit(false)
	}

	server.Sugar = sugar

	upgrader := websocket.Upgrader{
		ReadBufferSize:    1024 * 1024 * 1024,
		WriteBufferSize:   1024 * 1024 * 1024,
		EnableCompression: server.Compression,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
			server.Sugar.Errorf("error: %v", reason)
		},
		Subprotocols: []string{"v10.stomp", "v11.stomp", "v12.stomp"},
	}

	server.upgrader = upgrader
	server.setup = true
}

func logInit(debugEnabled bool) *zap.SugaredLogger {
	pe := zap.NewProductionEncoderConfig()

	pe.EncodeTime = zapcore.ISO8601TimeEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(pe)

	level := zap.InfoLevel
	if debugEnabled {
		level = zap.DebugLevel
	}

	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level),
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stderr), zap.WarnLevel),
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stderr), zap.ErrorLevel),
	)

	l := zap.New(core)

	return l.Sugar()
}
