package main

import (
	"DIY_RPC"
	"context"
	"log"
	"net"
	"net/http"
	"sync"
)

// Calculator 服务名称
type Calculator int

type Args struct {
	Num1, Num2 int
}

// Sum Calculator服务提供的方法
func (f Calculator) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	// 1. 注册Calculator.Sum服务
	var cal Calculator

	err := DIY_RPC.DefaultServer.Register(cal) // 方法接收者为类型T(非指针), *T也可以作为方法接收者; 反之不行
	if err != nil {
		log.Printf("[DIY_RPC Server] Register service error: %v\n", err)
		return
	}

	// 2. 注册http请求的处理函数
	DIY_RPC.HandleHTTP()

	// 3. 启动rpc服务器, 监听8000端口, 并按照注册的ServeHTTP方法处理请求
	listener, _ := net.Listen("tcp", "localhost:8000")
	addr <- "localhost:8000"
	log.Fatal(http.Serve(listener, nil))
}

func call(addCh chan string) {
	// DialHTTP方法:
	// 1. 建立tcp连接, 启动子协程执行NewHTTPClient方法
	// 2. 构造客户端实例时发送CONNECT HTTP请求, 随后校验响应行;
	// 3. 校验通过后, 执行NewClient, 发送option结构, 构建编解码器, 创建receive子协程
	client, err := DIY_RPC.DialHTTP("tcp", <-addCh)

	if err != nil {
		log.Printf("[DIY_RPC client]The initialization of Client failure, err[%v]\n", err)
		return
	}

	wg := sync.WaitGroup{}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			args := Args{
				Num1: 2*i + 1,
				Num2: i * i,
			}
			var reply int
			// 发送请求, 接收响应; 传入指针类型用于接收响应结果(字符串类型)
			err = client.Call(context.Background(), "Calculator.Sum", args, &reply)
			if err != nil {
				log.Panicf("The call of req [%d] fail, error[%v]\n", i, err)
				return
			}
			log.Printf("[DIY_RPC client]The call of req [%d] success, Args: %v  resp: {%v}\n", i, args, reply)
		}(i)
	}
	// 等待子协程结束
	wg.Wait()
}

// day5
func main() {
	// 步骤1: 创建一个chan用于传递server监听端口
	addr := make(chan string, 1)

	// 步骤2: 启动客户端, 发送5个rpc调用
	go call(addr)

	// 步骤3: 启动服务端
	startServer(addr)
}
