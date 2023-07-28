package stomper

import (
	"bytes"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"strconv"
	"sync"
)

var _subMux sync.Mutex

var endOfHeaders []byte
var heartBeatPayload = []byte("\n")

func (server *StompServer) WssHandler(writer http.ResponseWriter, request *http.Request) {
	if !server.setup {
		server.Sugar.Errorf("server not setup")
		return
	}

	conn, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		server.Sugar.Warnf("failed to upgrade: %v", err)
		writer.Write([]byte(fmt.Sprintf("%v", err)))
		return
	}

	defer func() {
		defer conn.Close()
		for _, handler := range server.disconnectHandlers {
			handler(conn)
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			server.Sugar.Warnf("failed to read: %v", err)
			break
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
			err = connect(conn)
			if err != nil {
				server.Sugar.Warnf("unable to connect: %v", err)
				break
			}

			for _, handler := range server.connectHandlers {
				handler(conn, request, &stompMsg)
			}
		} else if command == Send || command == Subscribe || command == Unsubscribe {
			destination, ok := headers["destination"]
			if !ok {
				destination = ""
			}

			if command == Send {
				for _, handler := range server.messageHandlers {
					handler(conn, destination, &stompMsg)
				}
			} else if command == Subscribe {
				for _, handler := range server.subscribeHandlers {
					handler(conn, destination)
				}
			} else if command == Unsubscribe {
				for _, handler := range server.unsubscribeHandlers {
					handler(conn, destination)
				}
			}
		} else if command == Disconnect {
			return
		}
	}
}

func (server *StompServer) parseMessage(message []byte) (*StompMessage, error) {
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
