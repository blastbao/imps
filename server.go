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
	wsConns    *WSConns
}

func NewWSServer(addr string, port int, debug bool, debugPort int) *WSServer {
	pool, _ := ants.NewPool(200000)
	svr := &WSServer{
		addr:       addr,
		port:       port,
		debug:      debug,
		debugPort:  debugPort,
		stopChan:   make(chan struct{}, 1),
		wsConns:    NewWSConns(),
		workerPool: pool,
	}
	return svr
}

func (ws *WSServer) Run() {

	if ws.debug {
		go func() {
			if err := http.ListenAndServe(fmt.Sprintf("%s:%d", ws.addr, ws.debugPort), nil); err != nil {
				log.Fatalf("pprof failed: %v", err)
			}
			log.Printf("pprof is running.")
		}()
	}

	go ws.timer()
	go ws.start()

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

func (ws *WSServer) start() {
	log.Printf("[start] websocket server start.")
STOP:
	for {
		// stop check
		select {
		case <-ws.stopChan:
			log.Printf("[start] receive stop signal.")
			break STOP
		default:
		}

		// epoll wait
		conns, err := ws.wsConns.Wait()
		if err != nil {
			log.Printf("[start] Faild to sys wait %v", err)
			continue
		}
		log.Printf("[start] len(conns) := %d", len(conns))

		// handle events
		for _, conn := range conns {
			conn := conn // variable copy, avoid share the common conn.
			log.Printf("[start] conn.RemoteAddr = %s", conn.RemoteAddr())
			err = ws.workerPool.Submit(func() {
				ws.HandleRequest(conn)
			})
			if err != nil {
				logger.Errorf("[start] ws.workerPool.Submit() fail: %s", err)
			}
		}
	}

	log.Printf("[start] websocket server end.")
}

func (ws *WSServer) timer() {
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
	if err = ws.wsConns.Regist(conn); err != nil {
		log.Printf("Faild to add connection")
		conn.Close()
	}
}

func (ws *WSServer) HandleRequest(conn *websocket.Conn) (err error) {

	defer func() {
		if err != nil {
			log.Printf("[handleConn] err != nil, should Deregist and Close conn, conn=%v.", conn.RemoteAddr())
			err = ws.wsConns.Deregist(conn)
			log.Printf("[handleConn] Deregist conn success, err=%v.", err)
			err = conn.Close()
			log.Printf("[handleConn] close conn success, err=%v.", err)
		}
	}()

	wsConn, err := ws.wsConns.GetWSConn(conn)
	if err != nil {
		log.Printf("[handleConn] Faild to wsConns.GetWSConn(), err: %v", err)
		return err
	}
	//log.Printf("[handleConn] WSConn Info: %v", wsConn)

	err = wsConn.HandleRequest()
	if err != nil {
		log.Printf("[handleConn] Faild to wsConn.HandleRequest(), err: %v", err)
		return err
	}

	err = ws.wsConns.Resume(conn)
	if err != nil {
		log.Printf("[handleConn] Faild to wsConns.Resume(), err: %v", err)
		return err
	}

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
