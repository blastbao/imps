package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/blastbao/imps/model"
	"github.com/blastbao/imps/sys"
	"github.com/gorilla/websocket"
)

type WSConn struct {
	id        uint64
	conn      *websocket.Conn
	wsFd      int
	eventFd   *sys.EventFD
	timestamp time.Time
	lock      uint32
	lastPing  time.Time

	sendCh chan [][]byte
}

func NewWSConn(id uint64, c *websocket.Conn) *WSConn {
	wsFd := websocketFd(c)
	evfd, err := sys.NewEventFD()

	log.Printf("[NewWSConn] call sys.NewEventFD(), evfd=%v, err=%s", evfd, err)

	wsConn := &WSConn{
		id:        id,
		conn:      c,
		timestamp: time.Now(),
		wsFd:      wsFd,
		eventFd:   evfd,
		lock:      0,
		sendCh:    make(chan [][]byte, 1000),
	}

	return wsConn
}

func websocketFd(conn *websocket.Conn) int {
	connVal := reflect.Indirect(reflect.ValueOf(conn)).FieldByName("conn").Elem()
	tcpConn := reflect.Indirect(connVal).FieldByName("conn")
	fdVal := tcpConn.FieldByName("fd")
	pfdVal := reflect.Indirect(fdVal).FieldByName("pfd")
	return int(pfdVal.FieldByName("Sysfd").Int())
}

func (s *WSConn) GetWebSocketFd() int {
	return s.wsFd
}

func (s *WSConn) GetEventFd() int {
	return s.eventFd.Fd()
}

func (s *WSConn) Close() error {
	var err error
	err = s.eventFd.Close()
	if err != nil {
		log.Fatalf("[Close] call s.eventFd.Close() failed, err=%s", err)
		return err
	}
	err = s.conn.Close()
	if err != nil {
		log.Fatalf("[Close] call s.conn.Close() failed,  err=%s", err)
		return err
	}
	close(s.sendCh)
	return nil
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


func (s *WSConn) HandleEvent(fd int) error {

	var err error

	wsFd := s.GetWebSocketFd()
	evFd := s.GetEventFd()

	switch fd {
	case wsFd:
		err = s.HandleRequest()
		if err != nil {
			log.Printf("[handleConn] Faild to wsConn.HandleRequest(), err: %v", err)
			return err
		}
	case evFd:
		err = s.HandleMsgPush()
		if err != nil {
			log.Printf("[handleConn] Faild to wsConn.HandleRequest(), err: %v", err)
			return err
		}
	default:
		log.Printf("[handleConn] unknown fd")
		return err
	}
	return nil
}

const (
	readTimeout = 5 * time.Second
	readMaxSize = 512 //byte
	writeTimeout = 5 * time.Second
)


func (s *WSConn) HandleRequest() error {

	log.Printf("[HandleRequest] Start, conn.RemoteAddr=%v.", s.conn.RemoteAddr())

	s.conn.SetReadLimit(readMaxSize)
	_ = s.conn.SetReadDeadline(time.Now().Add(readTimeout))
	msgType, msg, err := s.conn.ReadMessage()
	if err != nil {
		log.Printf("[HandleRequest] Faild to ReadMessage(), %v", err)
		return err
	}

	log.Printf("[HandleRequest] wsMsgType=%d, wsMsg=%s, err=%s.", msgType, msg, err)

	switch msgType {
	case websocket.TextMessage:
		log.Printf("recv text: %s", msg)
		err = s.HandleHeartBeat(msg)
		if err != nil {
			log.Printf("[HandleRequest] Faild to s.HandleHeartBeat(msg), %v", err)
			return err
		}
	default:
		return errors.New("unknown msg type")
	}

	log.Printf("[HandleRequest] end, conn.RemoteAddr=%v.", s.conn.RemoteAddr())
	return nil
}

func (s *WSConn) HandleHeartBeat(msg []byte) error {

	log.Printf("[HandleHeartBeat] Start.")

	req := new(model.Req)
	err := json.Unmarshal(msg, req)
	if err != nil {
		log.Printf("[HandleHeartBeat] Faild to json.Unmarshal(msg, req), %v", err)
		return err
	}

	log.Printf("[HandleHeartBeat] receive msg `%s` from client `%s`", req.Msg, s.conn.RemoteAddr())

	rspMsg := model.Rsp{
		Code: uint32(12306),
		Msg:  fmt.Sprintf("Pong."),
	}

	content, err := json.Marshal(rspMsg)
	if err != nil {
		log.Printf("[HandleMsgPush] Faild to json.Marshal(msg), %v", err)
		return err
	}

	s.lastPing = time.Now()
	err = s.conn.WriteMessage(websocket.TextMessage, content)
	if err != nil {
		log.Printf("[HandleHeartBeat] Faild to json.Unmarshal(msg, req), %v", err)
		return err
	}

	log.Printf("[HandleHeartBeat] end.")
	return nil
}

func (s *WSConn) HandleMsgPush() error {

	log.Printf("[HandleMsgPush] Start.")

	events, err := s.eventFd.ReadEvents()
	if err != nil {
		log.Printf("[HandleMsgPush] Faild to s.eventFd.ReadEvents(), %v", err)
		return err
	}

	log.Printf("[HandleHeartBeat] s.eventFd.ReadEvents() succ, events=%d", events)

	msgs := <-s.sendCh

	for _, msg := range msgs {
		req := model.Req{
			Op:  uint32(999),
			Msg: string(msg),
		}

		content, err := json.Marshal(req)
		if err != nil {
			log.Printf("[HandleMsgPush] Faild to json.Marshal(msg), %v", err)
			continue
		}

		_ = s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		err = s.conn.WriteMessage(websocket.TextMessage, content)
		if err != nil {
			log.Printf("[HandleMsgPush] Faild to json.Unmarshal(msg, req), %v", err)
			continue
		}
		log.Printf("[HandleHeartBeat] send msg `%s` to client `%s`", req.Msg, s.conn.RemoteAddr())
	}

	log.Printf("[HandleMsgPush] end.")
	return nil
}

func (s *WSConn) PushMsgs(msgs [][]byte) error {
	log.Printf("[PushMsg] Start.")
	s.sendCh <- msgs

	err := s.eventFd.WriteEvents(uint64(len(msgs)))
	if err != nil {
		log.Printf("[PushMsg] Faild to s.eventFd.WriteEvents(1), %v", err)
		return err
	}

	log.Printf("[PushMsg] end.")
	return nil
}
