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
	id uint64

	conns map[uint64]*WSConn
	fds   map[int]*WSConn

	timer *sys.TimeWheel

	mu sync.RWMutex
}

func NewControl() *Control {
	epoll, err := sys.MakeEpoll()
	if err != nil {
		panic(err)
	}

	//// 每个 tick 为 100 ms ， 100 个为 10s，也即控制超时为 10s。
	//timer := sys.NewTimeWheel(time.Millisecond*100, 100, func(data interface{}) {
	//	c := data.(*WSConn)
	//	log.Printf("[sys.NewTimeWheel] connection timeout, conn=%v", c)
	//	c.ctrl.Deregist(c)
	//	c.Close()
	//})

	// 每个 tick 为 100 s ， 36 个为 1h，也即控制超时为 1h。
	timer := sys.NewTimeWheel(time.Second*100, 36, func(data interface{}) {
		c := data.(*WSConn)
		log.Printf("[sys.NewTimeWheel] connection timeout, conn=%v", c)
		c.ctrl.Deregist(c)
		c.Close()
	})

	ctrl := &Control{
		Epoll: epoll,
		id:    0,
		conns: make(map[uint64]*WSConn),
		fds:   make(map[int]*WSConn),
		timer: timer,
	}

	ctrl.Start()
	return ctrl
}

func (c *Control) Start() {
	c.timer.Start()
	go c.PushMsgCron(time.Second * 3)
}

func (c *Control) Regist(conn *websocket.Conn) error {
	uid := atomic.AddUint64(&c.id, 1)
	wsConn, err := NewWSConn(c, uid, conn)
	if err != nil {
		log.Printf("[Regist] NewWSConn() fail.")
		return err
	}
	if err := c.regEpoll(wsConn); err != nil {
		log.Printf("[Regist] c.regEpoll(wsConn) fail.")
		return err
	}
	c.timer.Add(wsConn)
	return nil
}

func (c *Control) PushMsgCron(duration time.Duration) {

	log.Printf("[PushMsgCron] start.")
	for range time.Tick(duration) {
		start := time.Now()
		for uid, conn := range c.conns {
			msg := fmt.Sprintf("hello user[%d], ts=%v", uid, time.Now().Format("2006/01/02 15:04:05"))
			err := conn.PushMsgs([][]byte{[]byte(msg)})
			if err != nil {
				log.Printf("[PushMsgCron] call conn.PushMsg(), msg=%s, err=%s", msg, err)
			}
		}
		elasp := time.Since(start)
		log.Printf("[PushMsgCron] PushMsgCron() finish, elasp=%v", elasp)
	}
	log.Printf("[PushMsgCron] end.")
}

func (c *Control) Deregist(conn *WSConn) error {
	if err := c.unRegEpoll(conn); err != nil {
		log.Printf("Faild to remove connection")
		return err
	}
	return nil
}

func (c *Control) Recover(conn *WSConn) (err error) {
	wsFd := conn.GetWebSocketFd()
	evFd := conn.GetEventFd()
	err = c.Resume(wsFd)
	if err != nil {
		log.Printf("Faild to remove connection, err=%v", err)
	}
	err = c.Resume(evFd)
	if err != nil {
		log.Printf("Faild to remove connection, err=%v", err)
	}
	return nil
}

func (c *Control) regEpoll(conn *WSConn) (err error) {
	wsFd := conn.GetWebSocketFd()
	evFd := conn.GetEventFd()
	err = c.Add(wsFd)
	if err != nil {
		log.Printf("Faild to add connection, err=%v", err)
		return err
	}
	defer func() {
		if err != nil {
			c.Remove(wsFd)
		}
	}()
	err = c.Add(evFd)
	if err != nil {
		log.Printf("Faild to add connection, err=%v", err)
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fds[wsFd] = conn
	c.fds[evFd] = conn
	c.conns[conn.id] = conn
	return nil
}

func (c *Control) unRegEpoll(conn *WSConn) (err error) {
	wsFd := conn.GetWebSocketFd()
	evFd := conn.GetEventFd()
	err = c.Remove(wsFd)
	if err != nil {
		log.Printf("Faild to remove connection, err=%v", err)
	}
	err = c.Remove(evFd)
	if err != nil {
		log.Printf("Faild to remove connection, err=%v", err)
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

func (c *Control) GetWSConnByUid(id uint64) (*WSConn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if conn, ok := c.conns[id]; ok {
		return conn, nil
	}
	log.Printf("can not get session.")
	return nil, errors.New("WSConn not exist")
}
