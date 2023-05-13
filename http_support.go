package DIY_RPC

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
)

// 兼容HTTP协议, 实现HTTP协议到RPC消息格式的转化
// 支持HTTP协议是为了提供
// 服务端对HTTP协议的支持:
// 1. 客户端向RPC服务器发送CONNECT请求
// 2. RPC服务器返回 HTTP 200状态码表示连接建立
// 3. 客户端使用创建好的连接发送 RPC 报文，先发送 Option，再发送 N 个请求报文
//    服务端处理 RPC 请求并响应
///

const (
	connected        = "200 Connected to DIY_RPC"
	defaultRPCPath   = "/DIY_RPC"
	defaultDebugPath = "/debug/DIY_RPC"
)

// ServeHTTP Server指针实现了http.Handler接口, 用于响应RPC请求
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusMethodNotAllowed) // 405
		io.WriteString(w, "405 must CONNECT\n")
		return
	}

	// 劫持连接后, 原有的io流 ResponseWriter 不能使用
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Println("rpc hijacking", req.RemoteAddr, ":", err.Error())
		return
	}
	// 注意是两个CRLF(\r\n), 之前没注意只加了一个CRLF, 导致客户端调用http.ReadResponse无法成功读取响应(阻塞)
	_, err = io.WriteString(conn, "HTTP/1.0 "+connected+"\r\n\r\n")
	if err != nil {
		log.Println("[ServeHTTP] Write HTTP response error: ", err)
		return
	}

	// 解析完HTTP协议后, 将连接交给ServeConn方法, 读取Option结构体即随后的N个请求
	go server.ServerConn(conn)
}

// HandleHTTP 实现了Handler接口的Server实例 注册到全局ServeMux
// (*Server)和debugHTTP都实现了http.Handler接口, 分别作为defaultRPCPath、defaultDebugPath的handler
func (server *Server) HandleHTTP() {
	// URL前缀包含'/DIY_RPC'的请求 路由给server.ServeHTTP方法处理
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Println("rpc server debug path:", defaultDebugPath)
}

// HandleHTTP 默认服务端实例注册http handler
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}

// 客户端对HTTP协议的支持:
// NewHTTPClient 发起CONNECT请求, 检查返回状态码, 若正确则建立连接
// 后续的通信过程交给NewClient
///

// NewHTTPClient 构造基于HTTP协议的Client实例
func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	// 发送HTTP协议CONNECT方法请求
	httpReq := fmt.Sprintf("CONNECT %s HTTP/1.0\r\n\r\n", defaultRPCPath)
	_, err := io.WriteString(conn, httpReq)
	if err != nil {
		log.Println("[DIY_RPC Client] NewHTTPClient send HTTP Request error:", err.Error())
		return nil, err
	}

	// ReadResponse 入参一: *bufio.Reader, 入参二: *Request
	// 从缓冲区中读取HTTP响应, 并返回一个Response结构体; 参数二用于选择性指定与Response相关的请求, 若为nil则假定为GET请求
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})

	// 检查返回的状态行
	if err == nil && resp.Status == connected {
		// resp.Status表示诸如"200 ok"的响应状态, 这里自定义为connected, 即"200 Connected to DIY_RPC"
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

// DialHTTP 连接指定网络和地址的 HTTP RPC服务器
func DialHTTP(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}
