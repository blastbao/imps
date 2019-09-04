package sys

import (
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type Epoll struct {
	fd   int
	lock *sync.RWMutex
}

func MakeEpoll() (*Epoll, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &Epoll{fd: fd, lock: &sync.RWMutex{}}, nil
}

func (e *Epoll) Add(fd int) error {
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP | unix.EPOLLONESHOT, Fd: int32(fd)})
	return err
}

// 下线删除描述符
func (e *Epoll) Remove(fd int) error {
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_DEL, fd, nil)
	return err
}

// Mod sets to listen events on fd.
func (e *Epoll) Resume(fd int) error {
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_MOD, fd, &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP | unix.EPOLLONESHOT, Fd: int32(fd)})
	return err
}

func (e *Epoll) Wait() ([]int, error) {
	events := make([]unix.EpollEvent, 1000)
	n, err := unix.EpollWait(e.fd, events, -1) // -1 为不设超时
	if err != nil {
		return nil, err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	var fds []int
	for i := 0; i < n; i++ {
		fds = append(fds, int(events[i].Fd))
	}
	return fds, nil
}
