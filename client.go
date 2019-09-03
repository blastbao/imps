package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/blastbao/imps/model"
	"github.com/gorilla/websocket"
	"gopkg.in/gin-gonic/gin.v1/json"
)

var (
	ip          = flag.String("ip", "10.211.55.4", "server IP")
	port        = flag.Int("port", 6060, "server port")
	connections = flag.Int("conn", 2, "number of websocket connections")
)

func main() {

	flag.Usage = func() {
		io.WriteString(os.Stderr, `Websockets client generator
Example usage: ./client -ip=172.17.0.1 -conn=10
`)
		flag.PrintDefaults()
	}
	flag.Parse()

	u := url.URL{
		Scheme: "ws",
		Host:   *ip + ":" + strconv.Itoa(*port),
		Path:   "/",
	}

	log.Printf("Connecting to %s", u.String())

	var conns []*websocket.Conn
	for i := 0; i < *connections; i++ {

		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			fmt.Println("Failed to connect", i, err)
			break
		}
		conns = append(conns, c)

		defer func() {
			c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			time.Sleep(time.Second)
			c.Close()
		}()
	}

	log.Printf("Finished initializing %d connections", len(conns))

	// 1 s
	tts := 1 * time.Second // time.Millisecond * 5 //
	// 5 ms
	if *connections > 100 {
		tts = time.Millisecond * 5
	}

	for {

		for i := 0; i < len(conns); i++ {
			time.Sleep(tts)
			conn := conns[i]
			//log.Printf("WSConn %d sending message", i)
			//if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second*5)); err != nil {
			//	fmt.Printf("Failed to receive pong: %v", err)
			//}

			msg := model.Req{
				Op:  uint32(i),
				Msg: fmt.Sprintf("Hello from conn %v", i),
			}

			cotent, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.BinaryMessage, cotent); err != nil {
				fmt.Printf("WSConn %d failed to WriteMessage(), err:%v", i, err)
			}
		}
	}
}
