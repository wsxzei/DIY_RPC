package DIY_RPC

import (
	"DIY_RPC/codec"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
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

type request struct{
	// 请求头
	h *codec.Header
	// 请求参数和响应参数
	argv, replyv reflect.Value
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
		go server.ServerConn(conn)
	}
}

// ServerConn
// 1. 首先使用json反序列化, 得到Option实例, 检查MagicNumber和CodeType是否正确
// 2. 随后根据CodeType得到对应的消息编解码器
// 3. 将反序列化的工作交给serverCodec完成
///
func (server *Server) ServerConn(conn io.ReadWriteCloser){
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil{
		log.Panicln("rpc server: options error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber{
		log.Printf("rpc server: invalid magic number %x\n", MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodeType]
	if f == nil{
		log.Panicf("rpc server: invalid codec type %s\n", opt.CodeType)
		return
	}

	// f为编解码器的构造函数, 入参为连接对象conn, 返回编解码器
	server.serverCodec(f(conn))
}


var invalidRequest = struct{}{}

// serverCodec
// 1. 读取请求
// 2. 处理请求
// 3. 回复请求 /
func (server *Server) serverCodec(codec codec.Codec){
	// new(Type)函数创建一个Type类型的实例, 返回该实例的指针
	// make仅用于创建map、slice、chan类型的实例, 返回值(引用)而非指针
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)

	for{
		// 依次读取输入
		req, err := server.readRequest(codec)
		if err != nil{
			if req == nil{
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(codec, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(codec, req, sending, wg)
	}
	// 若出现错误跳出循环, 等待处理请求的协程结束后, 主协程再退出
	wg.Wait()
	codec.Close()
}

// 利用加解码器读取请求头
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error){
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil{
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error){
	header, err := server.readRequestHeader(cc)
	if err != nil{
		return nil, err
	}
	req := &request{h: header}

	// day1: 假定请求参数是字符串类型
	// New函数 New(typ Type) Value, Value表示指向新的类型为typ零值的指针
	req.argv = reflect.New(reflect.TypeOf(""))
	// Value.Interface函数返回空接口, 底层值设置为Value对应类型的值
	if err = cc.ReadBody(req.argv.Interface()); err != nil{
		log.Println("rpc server: read argv err:", err)
	}

	return req, nil
}

// 处理请求:
// 将请求头和req.argv指针执行的元素打印出来, 设置replyv为Value类型, 具体值为字符串类型
// 使用sendResponse, 将响应信息发送出去
// TODO, 需要调用注册的rpc方法, 获取正确的返回参数
// day1: 打印argv, 发送hello world
///
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// 请求完成
	defer wg.Done()
	// 若argv为指针, Elem返回指向的值(若不为Interface或Pointer类型, 程序会恐慌)
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("greerpc resp %d", req.h.Seq))

	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

// 注意: 因为调用Codec#Write函数时, header和body是分别发送的。为了保证不同请求的应答信息的header, body顺序发送, 发送应答数据时需要加上互斥锁。
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil{
		log.Println("rpc server: write response error:", err)
	}
}