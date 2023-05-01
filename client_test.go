package DIY_RPC

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

/* 测试一: rpc client侧, 创建客户端实例超时 */
// TestClient_dialTimeout 测试连接服务端、创建客户端实例超时
func TestClient_dialTimeout(t *testing.T) {
	// 并发运行定义的多个测试
	t.Parallel()

	// 模拟服务端, 监听某个空闲端口
	l, _ := net.Listen("tcp", ":0")

	// 模拟创建客户端的过程, 需要2秒
	f := func(conn net.Conn, opt *Option) (*Client, error) {
		conn.Close()
		time.Sleep(2 * time.Second)
		return nil, nil
	}

	t.Run("timeout", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", l.Addr().String(), &Option{
			ConnectTimeout: 1, // 连接超时时间: 1秒
		})
		// 测试结果期望出现一个错误, 错误信息包含connection timeout
		_assert(err != nil && strings.Contains(err.Error(), "connect timeout"), "expect a timeout error")
	})

	t.Run("0 second timeout", func(t *testing.T) {
		client, err := dialTimeout(f, "tcp", l.Addr().String())
		_assert(err == nil && client == nil, "0 means no timeout condition")
	})
}

/* 测试二: rpc server侧, 处理请求超时 */
type Bar int // 服务类型

// 服务提供的方法, 模拟执行超时, 休眠2秒
func (b Bar) Timeout(argv int, reply *int) error {
	*reply = 2 * argv
	time.Sleep(time.Second * 2)
	return nil
}

func startServer(ch chan<- string) {
	// 服务Bar注册
	var bar Bar = 0
	DefaultServer.Register(bar)

	// 监听空闲端口
	listener, _ := net.Listen("tcp", ":0")
	// 传递服务端地址给客户端
	ch <- listener.Addr().String()

	// 死循环处理客户端连接
	DefaultServer.Accept(listener)
}

func TestClient_Call(t *testing.T) {
	t.Parallel()

	addr := make(chan string)
	go startServer(addr)

	// 测试rpc客户端调用服务超时
	t.Run("client timeout", func(t *testing.T) {
		client, err := Dial("tcp", <-addr, &Option{
			HandleTimeout: 1 * time.Second, // 请求处理的超时时间 1 秒
		})
		if err != nil {
			fmt.Printf("rpc client Dial failed, error[%v]\n", err)
			return
		}

		// 创建具有超时检测能力的context对象, 超时时间 1 秒
		ctx, _ := context.WithTimeout(context.Background(), time.Second)

		arg := 3
		err = client.Call(ctx, "Bar.Timeout", arg, &arg)
		if err != nil {
			fmt.Printf("rpc client  client.Call failed, error[%v]\n", err)
		}
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expect a timeout error")
	})

	t.Run("server handle timeout", func(t *testing.T) {

	})

}
