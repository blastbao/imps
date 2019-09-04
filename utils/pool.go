package utils

import (
	"log"
	"net"
	"net/url"
	"strconv"

	"github.com/gorilla/websocket"
)

var (
	ip   = "10.211.55.4"
	port = 6060

	u = url.URL{
		Scheme: "ws",
		Host:   ip + ":" + strconv.Itoa(port),
		Path:   "/",
	}

	factory = func() (net.Conn, error) {
		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			log.Printf("failed to connect, err=%v", err)
			return nil, err
		}
		return c, nil
	}
)
