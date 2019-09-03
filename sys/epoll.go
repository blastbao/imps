package sys

import (
	"log"
	"reflect"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

type Epoll struct {
	fd          int
	connections map[int]*websocket.Conn
	lock        *sync.RWMutex
}

func MakeEpoll() (*Epoll, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &Epoll{fd: fd, lock: &sync.RWMutex{}, connections: make(map[int]*websocket.Conn)}, nil
}

// 新增文件描述符监测
func (e *Epoll) Add(conn *websocket.Conn) error {
	// 生成文件描述符id
	fd := websocketFd(conn)
	log.Printf("描述符id: %d", fd)
	// BSD 系统不支持 EPOLL
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP | unix.EPOLLONESHOT, Fd: int32(fd)})
	if err != nil {
		return err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	e.connections[fd] = conn
	if len(e.connections)%100 == 0 {
		log.Printf("Total number of connections: %v", len(e.connections))
	}
	return nil
}

// 下线删除描述符
func (e *Epoll) Remove(conn *websocket.Conn) error {
	fd := websocketFd(conn)
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_DEL, fd, nil)
	if err != nil {
		return err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	delete(e.connections, fd)
	return nil
}

func (e *Epoll) Wait() ([]*websocket.Conn, error) {
	events := make([]unix.EpollEvent, 100)
	n, err := unix.EpollWait(e.fd, events, -1) // -1 为不设超时
	if err != nil {
		return nil, err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	var connections []*websocket.Conn
	for i := 0; i < n; i++ {
		conn := e.connections[int(events[i].Fd)]
		connections = append(connections, conn)
	}
	return connections, nil
}

// Mod sets to listen events on fd.
func (e *Epoll) Mod(conn *websocket.Conn) error {
	// 生成文件描述符id
	fd := websocketFd(conn)
	log.Printf("描述符id: %d", fd)
	// BSD 系统不支持 EPOLL
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_MOD, fd, &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP | unix.EPOLLONESHOT, Fd: int32(fd)})
	if err != nil {
		return err
	}
	return nil
}

// Resume implements Poller.Resume() method.
func (e *Epoll) Resume(conn *websocket.Conn) error {
	return e.Mod(conn)
}

func websocketFd(conn *websocket.Conn) int {
	connVal := reflect.Indirect(reflect.ValueOf(conn)).FieldByName("conn").Elem()
	tcpConn := reflect.Indirect(connVal).FieldByName("conn")
	fdVal := tcpConn.FieldByName("fd")
	pfdVal := reflect.Indirect(fdVal).FieldByName("pfd")
	return int(pfdVal.FieldByName("Sysfd").Int())
}
