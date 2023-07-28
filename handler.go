package stomper

import (
	"bytes"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"reflect"
	"strconv"
	"sync"
)

var endOfHeaders []byte
var heartBeatPayload = []byte("\n")

// Client is a wrapper over ws connection.
type Client struct {
	conn *websocket.Conn
	uid  uint64
}

var _mutex sync.Mutex
var clientUid uint64 = 0

func newClient(conn *websocket.Conn) *Client {
	_mutex.Lock()
	defer _mutex.Unlock()

	clientUid++
	return &Client{conn, clientUid}
}

func (server *Server) WssHandler(writer http.ResponseWriter, request *http.Request) {
	if !server.setup {
		server.Sugar.Errorf("server not setup")
		return
	}

	_conn, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		server.Sugar.Warnf("failed to upgrade: %v", err)
		writer.Write([]byte(fmt.Sprintf("%v", err)))
		return
	}

	client := newClient(_conn)
	go server.clientHandler(client, request.Header)
}

func (server *Server) clientHandler(client *Client, header http.Header) {
	defer func() {
		defer client.conn.Close()
		for _, handler := range server.disconnectHandlers {
			handler(client)
		}

		server.removeClient(client)
	}()

	for {
		mt, message, err := client.conn.ReadMessage()
		if err != nil {
			if _, ok := err.(*websocket.CloseError); ok {
				break
			}

			server.Sugar.Warnf("failed to read: (%d) (%s) %v", mt, reflect.TypeOf(err), err)
			break
		}

		if mt != websocket.TextMessage {
			continue
		}

		if bytes.Equal(message, heartBeatPayload) {
			continue
		}

		result, err := server.parseMessage(message)
		if err != nil {
			server.Sugar.Warnf("error parsing message: %v", err)
			break
		}

		stompMsg := *result
		command := stompMsg.Command
		headers := stompMsg.Headers

		if command == Connect {
			err = connect(client.conn)
			if err != nil {
				server.Sugar.Warnf("unable to connect: %v", err)
				break
			}

			for _, handler := range server.connectHandlers {
				if !handler(client, header, &stompMsg) {
					return
				}
			}

			server.addClient(client)
		} else if command == Send || command == Subscribe || command == Unsubscribe {
			destination, ok := headers["destination"]
			if !ok {
				destination = ""
			}

			if command == Send {
				for _, handler := range server.messageHandlers {
					handler(client, destination, &stompMsg)
				}
			} else if command == Subscribe {
				subscribe := true
				for _, handler := range server.subscribeHandlers {
					if !handler(client, destination) {
						subscribe = false
						break
					}
				}

				if subscribe {
					server.addSubscription(client, stompMsg)
				}
			} else if command == Unsubscribe {
				for _, handler := range server.unsubscribeHandlers {
					handler(client, destination)
				}

				server.removeSubscription(client, stompMsg)
			}
		} else if command == Disconnect {
			return
		}
	}
}

func (server *Server) parseMessage(message []byte) (*StompMessage, error) {
	split := bytes.Split(message, []byte("\n"))
	if len(split) < 2 {
		server.Sugar.Warnf("invalid command: %s", message)
		return nil, nil
	}

	command := StompCommand(split[0])
	headers := make(map[string]string)

	lastHeader := 0
	for index, line := range split {
		if index == 0 {
			continue
		}

		if bytes.Equal(line, endOfHeaders) {
			lastHeader = index
			break
		}

		header := bytes.SplitN(line, []byte(":"), 2)
		if len(header) != 2 {
			server.Sugar.Warnf("invalid header (%s)", line)
			break
		}

		headers[string(header[0])] = string(header[1])
	}

	var body []byte
	bodyWithNull := bytes.Join(split[lastHeader+1:], []byte("\n"))
	if val, ok := headers["content-length"]; ok {
		l, err := strconv.ParseInt(val, 10, 32)
		length := int(l)

		if err != nil {
			server.Sugar.Warnf("invalid content-length (%s)", val)
			return nil, nil
		}

		receivedLength := len(bodyWithNull) - 1
		if length < receivedLength {
			server.Sugar.Warnf(
				"invalid content-length exceeds body size. expected %d got %d (%s)",
				length, receivedLength, val,
			)

			return nil, nil
		}

		body = bodyWithNull[:length]
	} else {
		nullIndex := bytes.IndexByte(bodyWithNull, 0x00)
		body = bodyWithNull[:nullIndex]
	}

	return &StompMessage{
		Command: command,
		Headers: headers,
		Body:    &body,
	}, nil
}

func connect(conn *websocket.Conn) error {
	stompMessage := StompMessage{
		Command: Connected,
		Headers: map[string]string{
			"version":    "1.2",
			"heart-beat": "10000,10000",
		},
		Body: nil,
	}

	return conn.WriteMessage(websocket.TextMessage, stompMessage.ToPayload())
}
