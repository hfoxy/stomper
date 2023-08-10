package stomper

import (
	"fmt"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"os"
	"strconv"
	"sync"
)

var _subscriptionMux sync.Mutex
var _clientMux sync.Mutex

type SubscribeHandler func(*Client, string) bool
type UnsubscribeHandler func(*Client, string)
type ConnectHandler func(*Client, http.Header, *StompMessage) bool
type DisconnectHandler func(*Client)
type MessageHandler func(*Client, string, *StompMessage)

type Server struct {
	Sugar               *zap.SugaredLogger
	Compression         bool
	ReadBufferSize      int
	WriteBufferSize     int
	setup               bool
	upgrader            websocket.Upgrader
	messageHandlers     []MessageHandler
	subscribeHandlers   []SubscribeHandler
	unsubscribeHandlers []UnsubscribeHandler
	connectHandlers     []ConnectHandler
	disconnectHandlers  []DisconnectHandler
	clients             map[uint64]*Client
	subscriptions       map[string]map[uint64]map[string]*Client
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
	server.clients = make(map[uint64]*Client)
	server.subscriptions = make(map[string]map[uint64]map[string]*Client)

	readBufferSize := server.ReadBufferSize
	if readBufferSize <= 0 {
		readBufferSize = 128
	}

	writeBufferSize := server.WriteBufferSize
	if writeBufferSize <= 0 {
		writeBufferSize = 512
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:    readBufferSize,
		WriteBufferSize:   writeBufferSize,
		WriteBufferPool:   &sync.Pool{},
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

func (server *Server) addClient(client *Client) {
	_clientMux.Lock()
	defer _clientMux.Unlock()
	server.clients[client.Uid] = client
}

func (server *Server) removeClient(client *Client) {
	_clientMux.Lock()
	defer _clientMux.Unlock()
	delete(server.clients, client.Uid)

	_subscriptionMux.Lock()
	defer _subscriptionMux.Unlock()
	for _, subs := range server.subscriptions {
		delete(subs, client.Uid)
	}
}

func (server *Server) addSubscription(client *Client, message StompMessage) bool {
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
		subs = make(map[uint64]map[string]*Client)
		server.subscriptions[topic] = subs
	}

	clientSubs, csok := subs[client.Uid]
	if !csok {
		clientSubs = make(map[string]*Client)
		subs[client.Uid] = clientSubs
	}

	clientSubs[subId] = client
	server.subscriptions[topic] = subs
	server.Sugar.Infof("[%d] subscribed to '%s' (%s)", client.Uid, topic, subId)
	return true
}

func (server *Server) removeSubscription(client *Client, message StompMessage) bool {
	var subId string
	var ok bool
	if subId, ok = message.Headers["id"]; !ok {
		return false
	}

	_clientMux.Lock()
	_subscriptionMux.Lock()
	defer _clientMux.Unlock()
	defer _subscriptionMux.Unlock()

	for _, subs := range server.subscriptions {
		clientSubs, csok := subs[client.Uid]
		if !csok {
			return true
		}

		delete(clientSubs, subId)
		if len(clientSubs) == 0 {
			delete(subs, client.Uid)
		}

		//server.subscriptions[topic] = subs
	}

	return true
}

func (server *Server) SendMessageWithCheck(topic string, contentType string, body string, check func(client *Client) bool) {
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

				if check != nil && !check(client) {
					continue
				}

				err := client.Conn.WriteMessage(websocket.TextMessage, message.ToPayload())
				if err != nil {
					server.Sugar.Errorf("unable to write message: %v", err)
				}
			}
		}
	}
}

func (server *Server) SendMessage(topic string, contentType string, body string) {
	server.SendMessageWithCheck(topic, contentType, body, nil)
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
