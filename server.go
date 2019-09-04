package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/micro/grpc-go/logger"
	"github.com/panjf2000/ants"
)

type WSServer struct {
	addr string
	port int

	debug     bool
	debugPort int

	stopChan chan struct{}

	workerPool *ants.Pool
	ctrl       *Control
}

func NewWSServer(addr string, port int, debug bool, debugPort int) *WSServer {
	pool, _ := ants.NewPool(200000)
	svr := &WSServer{
		addr:       addr,
		port:       port,
		debug:      debug,
		debugPort:  debugPort,
		stopChan:   make(chan struct{}, 1),
		ctrl:       NewControl(),
		workerPool: pool,
	}
	return svr
}

func (ws *WSServer) Run() {

	if ws.debug {
		go func() {
			if err := http.ListenAndServe(fmt.Sprintf("%s:%d", ws.addr, ws.debugPort), nil); err != nil {
				log.Printf("pprof failed: %v", err)
			}
			log.Printf("pprof is running.")
		}()
	}

	go ws.Timer()
	go ws.Start()

	log.Printf("ws server is running.")
	http.HandleFunc("/", ws.HandleConnection)
	if err := http.ListenAndServe(fmt.Sprintf("%s:%d", ws.addr, ws.port), nil); err != nil {
		log.Fatal(err)
	}
}

func (ws *WSServer) Stop() {
	ws.workerPool.Release()
	close(ws.stopChan)
}

func (ws *WSServer) Start() {
	log.Printf("[Start] websocket server Start.")
STOP:
	for {
		// stop check
		select {
		case <-ws.stopChan:
			log.Printf("[Start] receive stop signal.")
			break STOP
		default:
		}

		// epoll wait
		fds, err := ws.ctrl.Wait()
		if err != nil {
			log.Printf("[Start] Faild to sys wait %v", err)
			continue
		}
		log.Printf("[Start] len(fds) := %d", len(fds))

		// handle events
		for _, fd := range fds {
			fd := fd // variable copy, avoid share the common conn.
			log.Printf("[Start] fd = %d", fd)
			err = ws.workerPool.Submit(func() {
				ws.HandleEvent(fd)
			})
			if err != nil {
				logger.Errorf("[Start] ws.workerPool.Submit() fail: %s", err)
			}
		}
	}

	log.Printf("[Start] websocket server end.")
}

func (ws *WSServer) Timer() {
	log.Printf("[Timer] Start.")

	taskDuration := time.Duration(time.Second * time.Duration(5))
	timer := time.NewTicker(taskDuration)
	defer timer.Stop()

STOP:

	for {
		select {
		case <-ws.stopChan:
			log.Printf("[Timer] receive stop signal.")
			break STOP
		case <-timer.C:
			log.Printf("[Timer] ticker arrive.")
		}
	}

	log.Printf("[Timer] End.")
}

func (ws *WSServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	log.Printf("Connect to server")
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	if err = ws.ctrl.Regist(conn); err != nil {
		log.Printf("Faild to add connection")
		conn.Close()
	}
}

func (ws *WSServer) HandleEvent(fd int) (err error) {

	wsConn, err := ws.ctrl.GetWSConnByFd(fd)
	if err != nil {
		log.Printf("[HandleEvent] ws.ctrl.GetWSConnByFd(fd) faild, err: %v", err)
		err = ws.ctrl.Remove(fd)
		if err != nil {
			log.Printf("[HandleEvent] ws.ctrl.Remove(fd) faild, err: %v", err)
		}
		return err
	}

	defer func() {
		if err != nil {
			log.Printf("[HandleEvent] err != nil, should Deregist and Close conn, conn=%v.", wsConn.conn.RemoteAddr())
			err = ws.ctrl.Deregist(wsConn)
			log.Printf("[HandleEvent] ws.ctrl.Deregist(wsConn) success, err=%v.", err)
			err = wsConn.Close()
			log.Printf("[HandleEvent] wsConn.Close() success, err=%v.", err)
		}
	}()

	err = wsConn.HandleEvent(fd)
	if err != nil {
		log.Printf("[HandleEvent] wsConn.HandleEvent(fd) faild, err: %v", err)
		return err
	}

	err = ws.ctrl.Resume(fd)
	if err != nil {
		log.Printf("[HandleEvent] ws.ctrl.Resume(fd) faild , err: %v", err)
		return err
	}

	log.Printf("[HandleEvent] WSConn Info: %v", wsConn)
	return nil
}

func main() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}
	rLimit.Cur = rLimit.Max
	log.Printf("RLIMIT: %v", rLimit)
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}

	svr := NewWSServer("10.211.55.4", 6060, true, 6061)
	svr.Run()
}
