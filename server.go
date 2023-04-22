package DIY_RPC

import (
	"DIY_RPC/codec"
	"encoding/json"
	"fmt"
	"go/ast"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
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

// 一个函数能被远程调用, 需要满足五个条件:
// 1. 方法所属的type是导出的;
// 2. 方法是导出的(大写字母开头);
// 3. 方法有两个入参, 均为导出或内建类型;
// 4. 第二个入参(返回值)必须为指针;
// 5. 返回值为error类型
///
type methodType struct {
	method    reflect.Method // 方法本身
	ArgType   reflect.Type   // 第一个参数的类型
	ReplyType reflect.Type   // 第二个参数的类型
	numCalls  uint64         // 方法调用次数
}

// 创建argv实例
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	if m.ArgType.Kind() == reflect.Pointer {
		// ArgType为指针类型, argv应该是指向相同类型的指针
		argv = reflect.New(m.ArgType.Elem())
	} else {
		// ArgType不为指针, Value.Elem()得到可取地址的reflect.Value
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	// ReplyType必须为指针类型
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	// replyv为slice、map引用类型的指针, 指向缺省值为nil的实例, 需要初始化
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

type service struct {
	name      string                 // 映射的结构体的名称
	typ       reflect.Type           // 结构体的reflect.Type
	receiver  reflect.Value          // 结构体实例的reflect.Value, 方法接收者
	methodMap map[string]*methodType // 映射的结构体中, 符合条件的方法
}

func newService(receiver interface{}) *service {
	s := new(service)
	s.receiver = reflect.ValueOf(receiver)
	// Indirect方法获取入参v指向的值, 如果v不为指针, 则返回v;
	// Name方法返回被定义类型的包内名称, 若是*T、struct{}等未定义的类型, 则返回空字符串
	s.name = reflect.Indirect(s.receiver).Type().Name()
	s.typ = reflect.TypeOf(receiver)
	if !ast.IsExported(s.name) {
		// 打印错误信息, 随后os.Exit(1)
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	// 注册service结构体的方法
	s.registerMethod()

	return s
}

// registerMethod 过滤符合条件的方法:
// 1. 两个导出或内置类型的入参(反射时为3个, 第0个为实例自身)
// 2. 返回值只有一个error类型
///
func (s *service) registerMethod() {
	s.methodMap = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		// 验证入参和返回值的个数
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		// 验证唯一的返回值是否为error接口
		// 注意: 不能使用reflect.TypeOf(error(nil)), 获取的是error接口的底层类型(未定义), 打印为<nil>
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		// 获取两个入参, 验证是否为导出或内建类型
		argType, replyType := mType.In(0), mType.In(1)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.methodMap[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
			numCalls:  0,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

// isExportedOrBuiltinType 判断入参t是否为内建类型或导出类型
// 导出类型: 名称的首字母大写;
// 内建类型: 包路径为空, 非内建类型至少有一层包路径
///
func isExportedOrBuiltinType(t reflect.Type) bool {
	// IsExported验证是否name首字母大写; PkgPath返回完整的包路径,
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// call 通过反射调用指定方法
func (s *service) call(m *methodType, argVal, replyVal reflect.Value) error {
	// 累加调用次数
	atomic.AddUint64(&m.numCalls, 1)
	// 获取函数类型reflect.Value, Call方法执行反射调用
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.receiver, argVal, replyVal})
	// 接口为nil, 说明没有底层类型, 即不存在错误
	if err := returnValues[0].Interface(); err != nil {
		return err.(error)
	}
	return nil
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
