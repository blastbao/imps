package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blastbao/imps/model"
	"github.com/blastbao/imps/sys"
	"github.com/gorilla/websocket"
)

type WSConns struct {
	*sys.Epoll
	id    uint64
	conns map[*websocket.Conn]*WSConn
	mu    sync.RWMutex
}

func NewWSConns() *WSConns {
	epoll, err := sys.MakeEpoll()
	if err != nil {
		panic(err)
	}
	wsConns := &WSConns{
		Epoll: epoll,
		id:    0,
		conns: make(map[*websocket.Conn]*WSConn),
	}
	return wsConns
}

func (cs *WSConns) Regist(conn *websocket.Conn) error {
	if err := cs.Add(conn); err != nil {
		log.Printf("Faild to add connection")
		return err
	}
	wsConn := NewWSConn(atomic.AddUint64(&cs.id, 1), conn)
	cs.mu.Lock()
	cs.conns[conn] = wsConn
	cs.mu.Unlock()
	return nil
}

func (cs *WSConns) Deregist(conn *websocket.Conn) error {
	cs.mu.Lock()
	delete(cs.conns, conn)
	cs.mu.Unlock()
	if err := cs.Remove(conn); err != nil {
		log.Printf("Faild to remove connection")
		return err
	}
	return nil
}

func (cs *WSConns) GetWSConn(conn *websocket.Conn) (*WSConn, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if session, ok := cs.conns[conn]; ok {
		return session, nil
	}
	log.Printf("can not get session.")
	return nil, errors.New("session not exist")
}

type WSConn struct {
	id        uint64
	conn      *websocket.Conn
	timestamp time.Time
	lock      uint32
	lastPing time.Time
}

func NewWSConn(id uint64, c *websocket.Conn) *WSConn {
	return &WSConn{
		id:        id,
		conn:      c,
		timestamp: time.Now(),
		lock:      0,
	}
}

func (s *WSConn) String() string {
	format := "id: %d, create on: %s"
	return fmt.Sprintf(format, s.id, s.timestamp)
}

func (s *WSConn) handleMsg(req *model.Req) error {
	log.Printf("[HandleMsg] req: %v", req)
	return nil
}

func (s *WSConn) Lock() bool {
	return atomic.CompareAndSwapUint32(&s.lock, 0, 1)
}

func (s *WSConn) UnLock() bool {
	return atomic.CompareAndSwapUint32(&s.lock, 1, 0)
}

func (s *WSConn) HandleHeartBeat(msg []byte) error {

	log.Printf("[HandleHeartBeat] start, msg=%v.", string(msg))

	req := new(model.Req)
	err := json.Unmarshal(msg, req)
	if err != nil {
		log.Printf("[HandleHeartBeat] Faild to json.Unmarshal(msg, req), %v", err)
		return err
	}

	s.lastPing = time.Now()
	err = s.conn.WriteMessage(websocket.TextMessage, msg)
	if err != nil {
		log.Printf("[HandleHeartBeat] Faild to json.Unmarshal(msg, req), %v", err)
		return err
	}

	log.Printf("[HandleHeartBeat] end.")
	return nil
}

func (s *WSConn) HandleRequest() error {

	log.Printf("[HandleRequest] start, conn.RemoteAddr=%v.", s.conn.RemoteAddr())

	msgType, msg, err := s.conn.ReadMessage()
	if err != nil {
		log.Printf("[HandleRequest] Faild to ReadMessage(), %v", err)
		return err
	}

	log.Printf("[HandleRequest] wsMsgType=%d, wsMsg=%s, err=%s.", msgType, msg, err)

	switch msgType {

	case websocket.CloseMessage:
		log.Println("get close signal")

	case websocket.PingMessage:
		log.Println("get ping")
		err = s.HandleHeartBeat(msg)
		if err != nil {
			log.Printf("[HandleRequest] Faild to s.HandleHeartBeat(msg), %v", err)
			return err
		}
	case websocket.PongMessage:
		log.Println("get pong")
		err = s.HandleHeartBeat(msg)
		if err != nil {
			log.Printf("[HandleRequest] Faild to s.HandleHeartBeat(msg), %v", err)
			return err
		}
	case websocket.TextMessage:
		log.Printf("recv text: %s", msg)
		err = s.HandleHeartBeat(msg)
		if err != nil {
			log.Printf("[HandleRequest] Faild to s.HandleHeartBeat(msg), %v", err)
			return err
		}
	case websocket.BinaryMessage:
		log.Printf("recv binary: %s", msg)
		break
	default:
		return errors.New("unknown msg type")
	}

	log.Printf("[HandleRequest] end, conn.RemoteAddr=%v.", s.conn.RemoteAddr())
	return nil
}
