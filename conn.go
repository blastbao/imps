package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blastbao/imps/model"
	"github.com/blastbao/imps/sys"
	"github.com/gorilla/websocket"
)

const (
	CONN_IDLE   uint32 = 0
	CONN_BUSY   uint32 = 1
	CONN_CLOSED uint32 = 2
)

type WSConn struct {
	id        uint64
	conn      *websocket.Conn
	wsFd      int
	eventFd   *sys.EventFD
	timestamp time.Time

	lastPing time.Time

	mu    sync.Mutex
	state uint32

	ctrl   *Control
	sendCh chan [][]byte
}

func NewWSConn(ctrl *Control, id uint64, c *websocket.Conn) (*WSConn, error) {
	wsFd := websocketFd(c)
	evfd, err := sys.NewEventFD()
	if err != nil {
		log.Printf("[NewWSConn] call sys.NewEventFD(), evfd=%v, err=%s", evfd, err)
		return nil, err
	}
	wsConn := &WSConn{
		id:        id,
		conn:      c,
		timestamp: time.Now(),
		wsFd:      wsFd,
		eventFd:   evfd,
		state:     0,
		sendCh:    make(chan [][]byte, 10),
		ctrl:      ctrl,
	}
	return wsConn, nil
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
	if err := s.TryLock(1); err != nil {
		return err
	}
	defer func() {
		atomic.CompareAndSwapUint32(&s.state, CONN_BUSY, CONN_CLOSED)
	}()

	var err error
	err = s.eventFd.Close()
	if err != nil {
		log.Printf("[Close] call s.eventFd.Close() failed, err=%s", err)
		return err
	}
	err = s.conn.Close()
	if err != nil {
		log.Printf("[Close] call s.conn.Close() failed,  err=%s", err)
		return err
	}
	close(s.sendCh)
	return nil
}

func (s *WSConn) String() string {
	format := "id: %d, create on: %s"
	return fmt.Sprintf(format, s.id, s.timestamp)
}

func (s *WSConn) HandleEvent(fd int) error {
	var err error
	wsFd := s.GetWebSocketFd()
	evFd := s.GetEventFd()

	switch fd {
	case wsFd:

		start := time.Now()
		err = s.HandleHeartBeats()
		if err != nil {
			log.Printf("[HandleEvent] s.HandleHeartBeats() failed, err: %v", err)
			return err
		}
		elasp := time.Since(start)

		if elasp > time.Second * 1 {
			log.Printf("[HandleEvent] s.HandleHeartBeats() cost %v", elasp)
		}

	case evFd:
		start := time.Now()
		err = s.HandleMsgPush()
		if err != nil {
			log.Printf("[HandleEvent] s.HandleMsgPush() failed, err: %v", err)
			return err
		}
		elasp := time.Since(start)
		if elasp > time.Second * 1 {
			log.Printf("[HandleEvent] s.HandleMsgPush() cost %v", elasp)
		}
	default:
		log.Printf("[HandleEvent] unknown fd")
		return err
	}
	return nil
}

const (
	readTimeout  = 5 * time.Second
	readMaxSize  = 512 //byte
	writeTimeout = 5 * time.Second
)

func (s *WSConn) State() uint32 {
	return atomic.LoadUint32(&s.state)
}

func (s *WSConn) IsClosed() bool {
	return atomic.LoadUint32(&s.state) == CONN_CLOSED
}

func (s *WSConn) Occupy() bool {
	if atomic.LoadUint32(&s.state) != CONN_IDLE {
		return false
	}
	return atomic.CompareAndSwapUint32(&s.state, CONN_IDLE, CONN_BUSY)
}

func (s *WSConn) Release() bool {
	if atomic.LoadUint32(&s.state) != CONN_BUSY {
		return true
	}
	return atomic.CompareAndSwapUint32(&s.state, CONN_BUSY, CONN_IDLE)
}


var (
	rspMsg = model.Rsp{
		Code: uint32(12306),
		Msg:  fmt.Sprintf("Pong."),
	}
	content, _ = json.Marshal(rspMsg)
)


func (s *WSConn) HandleHeartBeats() error {
	if err := s.TryLock(3); err != nil {
		return err
	}
	defer func() {
		s.Release()
	}()


	//log.Printf("[HandleHeartBeats] Start, conn.RemoteAddr=%v.", s.conn.RemoteAddr())

	s.conn.SetReadLimit(readMaxSize)
	_ = s.conn.SetReadDeadline(time.Now().Add(readTimeout))
	_, _, err := s.conn.ReadMessage()
	if err != nil {
		log.Printf("[HandleHeartBeats] Faild to ReadMessage(), %v", err)
		return err
	}

	//log.Printf("[HandleHeartBeats] wsMsgType=%d, wsMsg=%s, err=%s.", msgType, msg, err)

	//req := new(model.Req)
	//err = json.Unmarshal(msg, req)
	//if err != nil {
	//	log.Printf("[HandleHeartBeats] Faild to json.Unmarshal(msg, req), %v", err)
	//	return err
	//}

	//log.Printf("[HandleHeartBeats] receive msg `%s` from client `%s`", req.Msg, s.conn.RemoteAddr())

	//rspMsg := model.Rsp{
	//	Code: uint32(12306),
	//	Msg:  fmt.Sprintf("Pong."),
	//}

	//content, err := json.Marshal(rspMsg)
	//if err != nil {
	//	log.Printf("[HandleHeartBeats] Faild to json.Marshal(rspMsg), %v", err)
	//	return err
	//}

	s.ctrl.timer.Add(s)
	s.lastPing = time.Now()

	_ = s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	err = s.conn.WriteMessage(websocket.TextMessage, content)
	if err != nil {
		log.Printf("[HandleHeartBeats] s.conn.WriteMessage() failed, %v", err)
		return err
	}

	//log.Printf("[HandleHeartBeats] end.")
	return nil
}

func (s *WSConn) HandleMsgPush() error {
	if err := s.TryLock(3); err != nil {
		return err
	}
	defer s.Release()

	//log.Printf("[HandleMsgPush] Start.")

	//events, err := s.eventFd.ReadEvents()
	_, err := s.eventFd.ReadEvents()
	if err != nil {
		log.Printf("[HandleMsgPush] Faild to s.eventFd.ReadEvents(), %v", err)
		return err
	}

	//log.Printf("[HandleMsgPush] s.eventFd.ReadEvents() succ, events=%d", events)

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
		//log.Printf("[HandleMsgPush] send msg `%s` to client `%s`", req.Msg, s.conn.RemoteAddr())
	}

	//log.Printf("[HandleMsgPush] end.")
	return nil
}

func (s *WSConn) TryLock(retry int) error {
	for r := 0; r < retry; r++ {
		if s.IsClosed() {
			//log.Printf("[TryLock] s.IsClosed() return true, so return err.")
			return errors.New("connection is closed")
		}
		res := s.Occupy()
		if res {
			//log.Printf("[TryLock] call s.Occupy() success.")
			return nil
		}
		time.Sleep(time.Millisecond * 10)
	}
	return errors.New("TryLock() timeout")
}

func (s *WSConn) PushMsgs(msgs [][]byte) error {

	if err := s.TryLock(3); err != nil {
		return err
	}
	defer s.Release()

	//log.Printf("[PushMsg] Start.")
	s.sendCh <- msgs
	err := s.eventFd.WriteEvents(uint64(len(msgs)))
	if err != nil {
		log.Printf("[PushMsg] Faild to s.eventFd.WriteEvents(1), %v", err)
		return err
	}
	//log.Printf("[PushMsg] end.")
	return nil
}
