package geerpc

import (
	"DIY_RPC/codec"
	"io"
	"log"
	"net"
)

const(
	MagicNumber = 0x3bef5c
)

// Option Json编码Option, CodeType决定Header+Body的编码方式
type Option struct {
	MagicNumber int // 魔数用于标记是什么rpc请求
	CodeType codec.Type
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodeType: codec.GobType,
}


type Server struct {}

func NewServer() *Server{
	return &Server{}
}

var DefaultServer = NewServer()

// Accept 调用Listener接口Accept函数从全连接队列中取出TCP连接
func (server *Server) Accept(listener net.Listener){

	// for无线循环, 阻塞等待socket连接建立
	for{
		conn, err := listener.Accept()
		if err != nil{
			log.Panicln("rpc server: accept error:", err)
			return
		}
		// 开启子协程处理, 处理过程交给ServerConn方法

	}
}

// ServerConn
// 1. 首先通过
///
func (server *Server) ServerConn(conn io.ReadWriteCloser){

}


