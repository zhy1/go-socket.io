package socketio

import (
	"github.com/googollee/go-engine.io"
	"log"
	"net/http"
	"strings"
	"sync"
)

// Socket is the socket object of socket.io.
type Socket interface {
	// Id returns the session id of socket.
	Id() string

	// Rooms returns the rooms name joined now.
	Rooms() []string

	// Request returns the first http request when established connection.
	Request() *http.Request

	// On registers the function f to handle an event.
	On(event string, f interface{}) error

	// Emit emits an event with given args.
	Emit(event string, args ...interface{}) error

	// Join joins the room.
	Join(room string) error

	// Leave leaves the room.
	Leave(room string) error

	// Disconnect disconnect the socket.
	Disconnect()

	// BroadcastTo broadcasts an event to the room with given args.
	BroadcastTo(room, event string, args ...interface{}) error
}

type socket struct {
	*socketHandler
	conn      engineio.Conn
	namespace string
	id        int
	mu        sync.Mutex
}

func newSocket(conn engineio.Conn, base *baseHandler) *socket {
	ret := &socket{
		conn: conn,
	}
	ret.socketHandler = newSocketHandler(ret, base)
	return ret
}

func (s *socket) Id() string {
	return s.conn.Id()
}

func (s *socket) Request() *http.Request {
	return s.conn.Request()
}

func (s *socket) Emit(event string, args ...interface{}) error {
	if err := s.socketHandler.Emit(event, args...); err != nil {
		return err
	}
	if event == "disconnect" {
		s.conn.Close()
	}
	return nil
}

func (s *socket) Disconnect() {
	s.conn.Close()
}

func (s *socket) send(args []interface{}) error {
	packet := packet{
		Type: _EVENT,
		Id:   -1,
		NSP:  s.namespace,
		Data: args,
	}
	encoder := newEncoder(s.conn)
	return encoder.Encode(packet)
}

func (s *socket) sendConnect() error {
	packet := packet{
		Type: _CONNECT,
		Id:   -1,
		NSP:  s.namespace,
	}
	encoder := newEncoder(s.conn)
	return encoder.Encode(packet)
}

func (s *socket) sendId(args []interface{}) (int, error) {
	s.mu.Lock()
	packet := packet{
		Type: _EVENT,
		Id:   s.id,
		NSP:  s.namespace,
		Data: args,
	}
	s.id++
	if s.id < 0 {
		s.id = 0
	}
	s.mu.Unlock()

	encoder := newEncoder(s.conn)
	err := encoder.Encode(packet)
	if err != nil {
		return -1, nil
	}
	return packet.Id, nil
}

func (s *socket) loop() error {
	defer func() {
		if err := recover(); err != nil {
			log.Println("socket-io error:", err)
		}
		s.LeaveAll()
		p := packet{
			Type: _DISCONNECT,
			Id:   -1,
		}
		s.socketHandler.onPacket(nil, &p)
	}()

	p := packet{
		Type: _CONNECT,
		Id:   -1,
	}
	encoder := newEncoder(s.conn)
	if err := encoder.Encode(p); err != nil {
		log.Println("加密p失败", err)
		return err
	}
	s.socketHandler.onPacket(nil, &p)
	for {
		decoder := newDecoder(s.conn)
		var p packet
		if err := decoder.Decode(&p); err != nil {
			str := err.Error()
			if strings.Contains(str, "EOF") {
				log.Println("解密p失败，分析, return = ", err, str, p , "id:" , p.Id, ",data:",p.Data, ",type:",p.Type.String(),",attach:",p.attachNumber)
				return err
			}
			log.Println("解密p失败，大部分情况为空, continue", err, str, p)
			continue
		}
		ret, err := s.socketHandler.onPacket(decoder, &p)
		if err != nil {
			log.Println("读取packet失败,可能是参数错误,没有实现相关方法:", err, decoder)
			continue
		}
		switch p.Type {
		case _CONNECT:
			s.namespace = p.NSP
			s.sendConnect()
		case _BINARY_EVENT:
			fallthrough
		case _EVENT:
			if p.Id >= 0 {
				p := packet{
					Type: _ACK,
					Id:   p.Id,
					NSP:  s.namespace,
					Data: ret,
				}
				encoder := newEncoder(s.conn)
				if err := encoder.Encode(p); err != nil {
					return err
				}
			}
		case _DISCONNECT:
			return nil
		}
	}
}
