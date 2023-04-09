package main

import (
	"DIY_RPC"
	"encoding/json"
	"fmt"
	"DIY_RPC/codec"
	"log"
	"net"
	"time"
)

func startServer(addr chan string) {
	// 随机选择一个空闲的端口
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	DIY_RPC.DefaultServer.Accept(listener)
}

func main() {
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple geerpc client
	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	time.Sleep(time.Second)
	// send options
	_ = json.NewEncoder(conn).Encode(DIY_RPC.DefaultOption)
	cc := codec.NewGobCodec(conn)
	// send request & receive response
	for i := 0; i < 5; i++ {
		// RPC请求: Header部分
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		// 发送RPC请求: header + body
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))

		// 读取应答信息的header, 存放到h指向的Header结构体中
		_ = cc.ReadHeader(h)
		var reply string
		// 读取应答信息的Body
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}