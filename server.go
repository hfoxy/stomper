package stomper

import (
	"fmt"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
)

var _subscriptionMux sync.Mutex
var _clientMux sync.Mutex

type SubscribeHandler func(*websocket.Conn, string) bool
type UnsubscribeHandler func(*websocket.Conn, string)
type ConnectHandler func(*websocket.Conn, *http.Request, *StompMessage) bool
type DisconnectHandler func(*websocket.Conn)
type MessageHandler func(*websocket.Conn, string, *StompMessage)

type Server struct {
	Sugar               *zap.SugaredLogger
	Compression         bool
	setup               bool
	upgrader            websocket.Upgrader
	messageHandlers     []MessageHandler
	subscribeHandlers   []SubscribeHandler
	unsubscribeHandlers []UnsubscribeHandler
	connectHandlers     []ConnectHandler
	disconnectHandlers  []DisconnectHandler
	clients             map[net.Addr]*websocket.Conn
	subscriptions       map[string]map[net.Addr]map[string]*websocket.Conn
}

func (server *Server) AddMessageHandler(handler MessageHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add message handler after server is setup")
	}

	server.messageHandlers = append(server.messageHandlers, handler)
	return nil
}

func (server *Server) AddSubscribeHandler(handler SubscribeHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add subscribe handler after server is setup")
	}

	server.subscribeHandlers = append(server.subscribeHandlers, handler)
	return nil
}

func (server *Server) AddUnsubscribeHandler(handler UnsubscribeHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add unsubscribe handler after server is setup")
	}

	server.unsubscribeHandlers = append(server.unsubscribeHandlers, handler)
	return nil
}

func (server *Server) AddConnectHandler(handler ConnectHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add connect handler after server is setup")
	}

	server.connectHandlers = append(server.connectHandlers, handler)
	return nil
}

func (server *Server) AddDisconnectHandler(handler DisconnectHandler) error {
	if server.setup {
		return fmt.Errorf("unable to add disconnect handler after server is setup")
	}

	server.disconnectHandlers = append(server.disconnectHandlers, handler)
	return nil
}

func (server *Server) Setup() {
	sugar := server.Sugar
	if sugar == nil {
		sugar = logInit(false)
	}

	server.Sugar = sugar
	server.clients = make(map[net.Addr]*websocket.Conn)
	server.subscriptions = make(map[string]map[net.Addr]map[string]*websocket.Conn)

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

func (server *Server) addClient(client *websocket.Conn) {
	_clientMux.Lock()
	defer _clientMux.Unlock()
	server.clients[client.RemoteAddr()] = client
}

func (server *Server) removeClient(client *websocket.Conn) {
	_clientMux.Lock()
	defer _clientMux.Unlock()
	delete(server.clients, client.RemoteAddr())

	_subscriptionMux.Lock()
	defer _subscriptionMux.Unlock()
	for _, subs := range server.subscriptions {
		delete(subs, client.RemoteAddr())
	}
}

func (server *Server) addSubscription(client *websocket.Conn, message StompMessage) bool {
	var topic string
	var subId string
	var ok bool
	if topic, ok = message.Headers["destination"]; !ok {
		return false
	}

	if subId, ok = message.Headers["id"]; !ok {
		return false
	}

	_clientMux.Lock()
	_subscriptionMux.Lock()
	defer _clientMux.Unlock()
	defer _subscriptionMux.Unlock()

	subs, sok := server.subscriptions[topic]
	if !sok {
		subs = make(map[net.Addr]map[string]*websocket.Conn)
		server.subscriptions[topic] = subs
	}

	clientSubs, csok := subs[client.RemoteAddr()]
	if !csok {
		clientSubs = make(map[string]*websocket.Conn)
		subs[client.RemoteAddr()] = clientSubs
	}

	clientSubs[subId] = client
	server.subscriptions[topic] = subs
	server.Sugar.Infof("[%s] subscribed to '%s' (%s)", client.RemoteAddr(), topic, subId)
	return true
}

func (server *Server) removeSubscription(client *websocket.Conn, message StompMessage) bool {
	var topic string
	var subId string
	var ok bool
	if topic, ok = message.Headers["destination"]; !ok {
		return false
	}

	if subId, ok = message.Headers["id"]; !ok {
		return false
	}

	_clientMux.Lock()
	_subscriptionMux.Lock()
	defer _clientMux.Unlock()
	defer _subscriptionMux.Unlock()

	subs, sok := server.subscriptions[topic]
	if !sok {
		return true
	}

	clientSubs, csok := subs[client.RemoteAddr()]
	if !csok {
		return true
	}

	delete(clientSubs, subId)
	if len(clientSubs) == 0 {
		delete(subs, client.RemoteAddr())
	}

	server.subscriptions[topic] = subs
	return true
}

func (server *Server) SendMessage(topic string, contentType string, body string) {
	_clientMux.Lock()
	_subscriptionMux.Lock()
	defer _clientMux.Unlock()
	defer _subscriptionMux.Unlock()

	byteBody := []byte(body)
	length := len(byteBody)

	subs, ok := server.subscriptions[topic]
	if ok {
		for _, clientSubs := range subs {
			for subId, client := range clientSubs {
				message := StompMessage{
					Command: Message,
					Headers: map[string]string{
						"content-type":   contentType,
						"subscription":   subId,
						"destination":    topic,
						"content-length": strconv.Itoa(length),
					},
					Body: &byteBody,
				}

				err := client.WriteMessage(websocket.TextMessage, message.ToPayload())
				if err != nil {
					server.Sugar.Errorf("unable to write message: %v", err)
				}
			}
		}
	}
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
