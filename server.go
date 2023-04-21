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

const (
	MagicNumber = 0x3bef5c
)

// 消息的格式为:  |Option | Header1 | Body1 | Header2 | Body2 | ...

// Option Json编码Option, CodeType决定Header+Body的编码方式
type Option struct {
	MagicNumber int // 魔数用于标记是什么rpc请求
	CodeType    codec.Type
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodeType:    codec.GobType,
}

type request struct {
	// 请求头
	h *codec.Header
	// 请求参数和响应参数
	argv, replyv reflect.Value
}

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// Accept 调用Listener接口Accept函数从全连接队列中取出TCP连接
func (server *Server) Accept(listener net.Listener) {

	// for无线循环, 阻塞等待socket连接建立
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Panicln("rpc server: accept error:", err)
			return
		}

		// 开启子协程处理, 处理过程交给ServerConn方法
		go server.ServerConn(conn)
	}
}

// ServerConn
// 1. 首先使用json反序列化, 得到Option实例, 检查MagicNumber和CodeType, 确定rpc请求的编码方式
// 2. 根据CodeType获取对应的消息编解码器的构造函数, 并实例化编解码器
// 3. 将rpc请求的反序列化的工作交给serverCodec完成
///
func (server *Server) ServerConn(conn io.ReadWriteCloser) {
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Panicln("rpc server: options error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x\n", MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodeType]
	if f == nil {
		log.Panicf("rpc server: invalid codec type %s\n", opt.CodeType)
		return
	}

	// f为编解码器的构造函数, 入参为连接对象conn, 返回编解码器
	server.serverCodec(f(conn))
}

var invalidRequest = struct{}{}

// serverCodec
// 死循环中重复如下逻辑:
// 1. 利用编解码器, 反序列化得到rpc请求req
// 2. 对于每一个rpc请求, 启动一个go协程进行处理
// 注: WaitGroup的作用是 当读取请求出现错误时, 当前协程等待所有handler子协程完成工作后才退出
///
func (server *Server) serverCodec(codec codec.Codec) {
	// new(Type)函数创建一个Type类型的实例, 返回该实例的指针
	// make仅用于创建map、slice、chan类型的实例, 返回值(引用)而非指针
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)

	for {
		// 依次读取输入
		req, err := server.readRequest(codec)
		if err != nil {
			if req == nil {
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
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	header, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: header}

	// day1: 假定请求参数是字符串类型
	// New函数 New(typ Type) Value, Value表示指向新的类型为typ零值的指针
	/*
			初学疑惑: 字符串为不可变类型, 为什么可以通过指向字符串空值的指针, 能将字节流写入缓冲区并转化为字符串？
			解答: 字符串可以看作结构体, 包含指针+长度字段, unsafe.Sizeof返回16字节, 即两个机器字
			示例:
		    strPtrVal := reflect.New(reflect.TypeOf(""))
		    if strPtrVal.Kind() == reflect.Ptr {
		       if strPtrVal.Elem().Kind() == reflect.String {
		          fmt.Println("strPtr is a pointer to the string. ")
		          // Elem()方法得到的字符串Value是可取地址的, 因此可以使用Set方法设置字符串值
		          strPtrVal.Elem().SetString("Hello World")
		       }
		    }
		    // 将指针Value转为(*string)类型, 从而可以解引用得到str
		    str := *(strPtrVal.Interface().(*string))
	*/
	req.argv = reflect.New(reflect.TypeOf(""))

	// Value.Interface函数返回空接口, 底层值设置为Value对应类型的值(这里为string指针)
	// ReadBody方法调用decoder.Decode方法读取字节流, 并解码为消息体; Decode方法只接受底层值为指针类型的入参;
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
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
	// 若argv为指针类型的Value, Elem返回指向的值(若不为Interface或Pointer类型, 程序会恐慌)
	log.Println("[DIY_RPC server] header:", req.h, "\targ:{", req.argv.Elem(), "}")
	req.replyv = reflect.ValueOf(fmt.Sprintf("DIY_RPC resp, seq:%d", req.h.Seq))

	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

// 注意: 因为调用Codec#Write函数时, header和body是分别发送的。为了保证不同请求的应答信息的header, body顺序发送, 发送应答数据时需要加上互斥锁。
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
