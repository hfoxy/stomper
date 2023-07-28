package stomper

import "fmt"

type StompMessage struct {
	Command StompCommand
	Headers map[string]string
	Body    *[]byte
}

func (m *StompMessage) ToString() string {
	var body string
	if m.Body != nil {
		body = string(*m.Body)
	}

	return fmt.Sprintf("%s: headers(%s): '%s'", m.Command, m.Headers, body)
}

func (m *StompMessage) ToPayload() []byte {
	var data []byte
	data = append(data, []byte(m.Command)...)
	data = append(data, []byte("\n")...)

	for name, value := range m.Headers {
		data = append(data, []byte(name)...)
		data = append(data, []byte(":")...)
		data = append(data, []byte(value)...)
		data = append(data, []byte("\n")...)
	}

	data = append(data, []byte("\n\n")...)
	if m.Body != nil {
		body := m.Body
		data = append(data, *body...)
	}

	data = append(data, 0x00)
	return data
}

type StompCommand string

const (
	Connect     StompCommand = "CONNECT"
	Stomp                    = "STOMP"
	Connected                = "CONNECTED"
	Send                     = "SEND"
	Subscribe                = "SUBSCRIBE"
	Unsubscribe              = "UNSUBSCRIBE"
	Ack                      = "ACK"
	Nack                     = "NACK"
	Begin                    = "BEGIN"
	Commit                   = "COMMIT"
	Abort                    = "ABORT"
	Disconnect               = "DISCONNECT"
	Message                  = "MESSAGE"
	Receipt                  = "RECEIPT"
	Error                    = "ERROR"
)
