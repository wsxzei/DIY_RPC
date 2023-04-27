package main

import (
	"DIY_RPC"
	"log"
	"net"
	"sync"
	"time"
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

/** rpc服务端启动:
 * 1. 注册服务Calculator, 在readRequest阶段才能返回service实例和methodType实例
 * 2. 打开监听套接字, 绑定需要监听的端口, go中封装为net.Listener
 * 3. server.Accept方法为死循环, 不断从Accept队列中获取连接实例, 每个连接实例分配一个协程处理请求;
 * 4. 服务端监听的地址通过channel传递给客户端.
 */
func startServer(addr chan string) {

	// 注册服务
	var cal Calculator
	err := DIY_RPC.DefaultServer.Register(&cal)
	if err != nil {
		log.Fatal("[startServer] error occurs when start Server:", err)
	}

	// port为空或0, 系统自动选择可用的端口;
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	DIY_RPC.DefaultServer.Accept(listener)
}

// day3: 构造请求参数和接收响应的参数, 发送rpc请求
func main() {
	// 步骤1: 创建一个chan用于传递server监听端口
	addr := make(chan string)

	// 步骤2: 启动服务端
	go startServer(addr)

	// 步骤3: 启动Client, 当addr通道中有地址可读时, Dial被调用
	// Dial方法: 1. 建立tcp连接并发送Option结构体, 协商序列化算法
	//			2. 初始化Client对象, 启动receive协程接收服务端响应
	client, err := DIY_RPC.Dial("tcp", <-addr)
	defer func() { client.Close() }()
	wg := sync.WaitGroup{}
	if err != nil {
		log.Printf("[DIY_RPC client]The initialization of Client failure, err[%v]\n", err)
		return
	}

	// 等待服务器启动, 进入Listener状态
	time.Sleep(time.Second)

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
			err = client.Call("Calculator.Sum", args, &reply)
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

/* 运行结果
2023/04/28 00:42:01 rpc server: register Calculator.Sum
2023/04/28 00:42:01 start rpc server on [::]:10778
2023/04/28 00:42:02 [DIY_RPC client]The call of req [1] success, Args: {3 1}  resp: {4}
2023/04/28 00:42:02 [DIY_RPC client]The call of req [4] success, Args: {9 16}  resp: {25}
2023/04/28 00:42:02 [DIY_RPC client]The call of req [0] success, Args: {1 0}  resp: {1}
2023/04/28 00:42:02 [DIY_RPC client]The call of req [3] success, Args: {7 9}  resp: {16}
2023/04/28 00:42:02 [DIY_RPC client]The call of req [2] success, Args: {5 4}  resp: {9}
*/
