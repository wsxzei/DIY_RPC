package DIY_RPC

import (
	"DIY_RPC/codec"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MagicNumber = 0x3bef5c
)

// 消息的格式为:  |Option | Header1 | Body1 | Header2 | Body2 | ...

// Option Json编码Option, CodeType决定Header+Body的编码方式
type Option struct {
	MagicNumber int // 魔数用于标记是什么rpc请求
	CodeType    codec.Type

	/* day4: 新增超时设定 */
	ConnectTimeout time.Duration // 连接超时
	HandleTimeout  time.Duration // 处理报文超时
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodeType:       codec.GobType,
	ConnectTimeout: time.Second * 100, // 默认10秒连接超时
}

type request struct {
	// 请求头
	h *codec.Header
	// 请求参数和响应参数
	argv, replyv reflect.Value
	mType        *methodType // 请求的方法
	svc          *service    // 方法所处的服务
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
	NumCalls  uint64         // 方法调用次数
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
	s.registerMethods()

	return s
}

// registerMethods 过滤符合条件的方法:
// 1. 两个导出或内置类型的入参(反射时为3个, 第0个为实例自身)
// 2. 返回值只有一个error类型
///
func (s *service) registerMethods() {
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
		// 获取两个入参, 验证是否为导出或内建类型(第0个入参为接收者自身)
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		// replyType必须为指针类型
		if replyType.Kind() != reflect.Pointer {
			continue
		}

		s.methodMap[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
			NumCalls:  0,
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
	atomic.AddUint64(&m.NumCalls, 1)
	// 获取函数类型reflect.Value, Call方法执行反射调用
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.receiver, argVal, replyVal})
	// 接口为nil, 说明没有底层类型, 即不存在错误
	if err := returnValues[0].Interface(); err != nil {
		return err.(error)
	}
	return nil
}

// Server RPC服务端
type Server struct {
	serviceMap    sync.Map // 保存服务端提供的服务
	handleTimeout time.Duration
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// Register 注册服务
func (server *Server) Register(receiver interface{}) error {
	// 1. 调用newService, 生成Service结构体
	s := newService(receiver)

	// 2. 将服务存储到serviceMap中
	// service.name为key, service指针为value; 如果服务存在, 则返回服务实例, 若不存在则设置为给定值;
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc server: service already defined: " + s.name)
	}
	return nil
}

// findService 解析service.method, 返回service结构体和methodType结构体指针
func (server *Server) findService(serviceMethod string) (s *service, mType *methodType, err error) {
	// 1. 解析出服务名和方法名
	dotIdx := strings.LastIndex(serviceMethod, ".")
	if dotIdx == -1 {
		err = errors.New("rpc server: service.method request ill-formed[" +
			serviceMethod + "]")
		return
	}
	serviceName, methodName := serviceMethod[:dotIdx], serviceMethod[dotIdx+1:]
	svc, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service[" + serviceName + "]")
		return
	}
	s = svc.(*service)
	mType = s.methodMap[methodName]
	if mType == nil {
		err = errors.New("rpc server: can't find method[" + methodName + "]")
	}
	return
}

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
		log.Println("rpc server: options error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x\n", MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodeType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s\n", opt.CodeType)
		return
	}
	// 设置请求处理的超时时间, 若为0, 则表示不会超时
	server.handleTimeout = opt.HandleTimeout

	// f为编解码器的构造函数, 入参为连接对象conn, 返回编解码器
	server.serverCodec(f(conn))
}

// 空结构体不占据内存空间, 在rpc通信场景下作为占位符, 可以节省带宽
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
		go server.handleRequest(codec, req, sending, wg, server.handleTimeout)
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

	/* 1. 从请求头中获取serviceMethod, 经过服务发现返回service和methodType指针 */
	req.svc, req.mType, err = server.findService(req.h.ServiceMethod)

	if err != nil {
		return req, err
	}

	/* 2. 通过newArgv和newReplyv方法, 创建出rpc调用方法的两个入参实例 */
	// 初始化请求参数, argv是指针类型的Value 或 可取地址的Value
	req.argv = req.mType.newArgv()
	// 初始化响应值, replyv为指针类型(ReplyType必须为指针类型)
	req.replyv = req.mType.newReplyv()

	// 编解码器反序列化得到请求body, 传入指针类型的interface{}接收结果
	// 对于非指针但是可取地址的req.argv, 先得到它的指针Value, 再转化为空接口
	argValInf := req.argv.Interface()
	if req.argv.Kind() != reflect.Pointer {
		argValInf = req.argv.Addr().Interface()
	}

	/* 3. 将请求报文的body反序列化, 得到rpc调用的第一个入参 */
	// ReadBody方法调用decoder.Decode方法读取字节流, 并解码为消息体; Decode方法只接受底层值为指针类型的入参;
	if err = cc.ReadBody(argValInf); err != nil {
		log.Println("rpc server: read body err[", err, "]")
		return req, err
	}

	return req, nil
}

// handleRequest 处理请求:
// day3版本, request中包含请求的service和methodType信息, 反射调用服务方法, 将请求头和返回值发送给客户端
// day4版本, 添加超时功能
///
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	// 请求完成
	defer wg.Done()
	// 请求处理分为: 请求方法调用结束、方法返回结果发送完成 两个阶段;
	called := make(chan struct{})
	sent := make(chan struct{})
	// finish信道: 防止处理请求的子协程阻塞
	finished := make(chan struct{})
	defer close(finished)

	// 子协程完成服务方法的反射调用, 通过信道传递 调用完成、响应结果发送完成 事件
	go func() {
		err := req.svc.call(req.mType, req.argv, req.replyv)
		select {
		case called <- struct{}{}:
			// 服务调用完成
			if err != nil {
				// 设置请求头的Error字段, 并将空结构体作为body(无效的请求)
				req.h.Error = err.Error()
				server.sendResponse(cc, req.h, invalidRequest, sending)
				sent <- struct{}{} // 完成rpc响应的发送
				return
			}
			server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
			sent <- struct{}{}
		case <-finished:
			// 处理请求超时, handleMessage协程不读取called信道, 导致子协程阻塞泄漏的情况
			// handleMessage协程退出前, 会关闭finished信道, 子协程退出
			close(called)
			close(sent)
		}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		log.Printf("[rpc server] request handle timeout: expect within %s", timeout)
		// time.After先于called接收到消息, 说明消息处理超时, called和sent将被阻塞
		req.h.Error = fmt.Sprintf("[rpc server] request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		// called信道接收到消息, 说明处理没有超时, 继续执行server.sendResponse
		<-sent
	}
}

// 注意: 因为调用Codec#Write函数时, header和body是分别发送的。为了保证不同请求的应答信息的header, body顺序发送, 发送应答数据时需要加上互斥锁。
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
