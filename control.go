package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blastbao/imps/sys"
	"github.com/gorilla/websocket"
)

type Control struct {
	*sys.Epoll
	id    uint64

	conns map[uint64]*WSConn
	fds map[int]*WSConn

	mu sync.RWMutex
}

func NewControl() *Control {
	epoll, err := sys.MakeEpoll()
	if err != nil {
		panic(err)
	}
	ctrl := &Control{
		Epoll: epoll,
		id:    0,
		conns: make(map[uint64]*WSConn),
		fds:   make(map[int]*WSConn),
	}
	return ctrl
}

func (c *Control) Regist(conn *websocket.Conn) error {

	uid := atomic.AddUint64(&c.id, 1)

	wsConn := NewWSConn(uid, conn)
	if err := c.regEpoll(wsConn); err != nil {
		log.Printf("Faild to add connection")
		return err
	}

	go func(wsConn *WSConn) {
		for range time.Tick(time.Second * 10) {
			msg := fmt.Sprintf("push msg from bacnkend svr, ts=%v", time.Now())
			err := wsConn.PushMsgs([][]byte{[]byte(msg)})
			log.Printf("[NewWSConn] call wsConn.PushMsg(), msg=%s, err=%s", msg, err)
		}
	}(wsConn)

	c.mu.Lock()
	c.conns[uid] = wsConn
	c.mu.Unlock()
	return nil
}

func (c *Control) Deregist(conn *WSConn) error {
	if err := c.unRegEpoll(conn); err != nil {
		log.Printf("Faild to remove connection")
		return err
	}
	c.mu.Lock()
	delete(c.conns, conn.id)
	c.mu.Unlock()
	return nil
}

func (c *Control) Recover(conn *WSConn) (err error) {
	wsFd := conn.GetWebSocketFd()
	evFd := conn.GetEventFd()
	err = c.Resume(wsFd)
	if err != nil {
		log.Fatalf("Faild to remove connection, err=%v", err)
	}
	err = c.Resume(evFd)
	if err != nil {
		log.Fatalf("Faild to remove connection, err=%v", err)
	}
	return nil
}

func (c *Control) regEpoll(conn *WSConn) (err error) {
	wsFd := conn.GetWebSocketFd()
	evFd := conn.GetEventFd()
	err = c.Add(wsFd)
	if err != nil {
		log.Fatalf("Faild to add connection, err=%v", err)
		return err
	}
	defer func() {
		if err != nil {
			c.Remove(wsFd)
		}
	}()
	err = c.Add(evFd)
	if err != nil {
		log.Fatalf("Faild to add connection, err=%v", err)
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fds[wsFd] = conn
	c.fds[evFd] = conn
	return nil
}

func (c *Control) unRegEpoll(conn *WSConn) (err error) {
	wsFd := conn.GetWebSocketFd()
	evFd := conn.GetEventFd()
	err = c.Remove(wsFd)
	if err != nil {
		log.Fatalf("Faild to remove connection, err=%v", err)
	}
	err = c.Remove(evFd)
	if err != nil {
		log.Fatalf("Faild to remove connection, err=%v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.fds, wsFd)
	delete(c.fds, evFd)
	delete(c.conns, conn.id)
	return nil
}

func (c *Control) GetWSConnByFd(fd int) (*WSConn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if conn, ok := c.fds[fd]; ok {
		return conn, nil
	}
	log.Printf("can not get session.")
	return nil, errors.New("WSConn not exist")
}
