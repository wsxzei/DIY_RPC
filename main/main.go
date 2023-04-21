package main

import (
	"DIY_RPC"
	"fmt"
	"log"
	"net"
	"sync"
)

func startServer(addr chan string) {
	// port为空或0, 系统自动选择可用的端口;
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	DIY_RPC.DefaultServer.Accept(listener)
}

// day2: client.Call并发五个RPC同步调用,参数和返回值均为string
func main() {
	// 步骤1: 创建一个chan用于传递server监听端口
	addr := make(chan string)

	// 步骤2: 启动服务端
	go startServer(addr)

	// 步骤3: 启动Client, 当addr通道中有地址可读时, Dial被调用
	// Dial方法: 1. 建立tcp连接并发送Option结构体, 协商序列化算法
	//			2. 初始化Client对象, 启动receive协程接收服务端响应
	client, err := DIY_RPC.Dial("tcp", <-addr)
	wg := sync.WaitGroup{}
	if err != nil {
		log.Printf("The initialization of Client failure, err[%v]\n", err)
		return
	}

	//time.Sleep(time.Second)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			args := fmt.Sprintf("DIY_RPC req [%d]", i)
			var reply string
			// 发送请求, 接收响应; 传入指针类型用于接收响应结果(字符串类型)
			err = client.Call("Foo.Sum", args, &reply)
			if err != nil {
				log.Panicf("The call of req [%d] fail, error[%v]\n", i, err)
				return
			}
			log.Printf("[DIY_RPC client]The call of req [%d] success, resp: {%v}\n", i, reply)
		}(i)
	}
	// 等待子协程结束
	wg.Wait()
}

/* 运行结果
2023/04/21 22:52:04 [DIY_RPC server] header: &{Foo.Sum 3 }      arg:{ DIY_RPC req [1] }
2023/04/21 22:52:04 [DIY_RPC server] header: &{Foo.Sum 2 }      arg:{ DIY_RPC req [0] }
2023/04/21 22:52:04 [DIY_RPC server] header: &{Foo.Sum 1 }      arg:{ DIY_RPC req [4] }
2023/04/21 22:52:04 [DIY_RPC server] header: &{Foo.Sum 4 }      arg:{ DIY_RPC req [3] }
2023/04/21 22:52:04 [DIY_RPC server] header: &{Foo.Sum 5 }      arg:{ DIY_RPC req [2] }
2023/04/21 22:52:04 [DIY_RPC client]The call of req [4] success, resp: {DIY_RPC resp, seq:1}
2023/04/21 22:52:04 [DIY_RPC client]The call of req [1] success, resp: {DIY_RPC resp, seq:3}
2023/04/21 22:52:04 [DIY_RPC client]The call of req [0] success, resp: {DIY_RPC resp, seq:2}
2023/04/21 22:52:04 [DIY_RPC client]The call of req [3] success, resp: {DIY_RPC resp, seq:4}
2023/04/21 22:52:04 [DIY_RPC client]The call of req [2] success, resp: {DIY_RPC resp, seq:5}
*/
