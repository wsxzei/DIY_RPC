package DIY_RPC

import (
	"DIY_RPC/codec"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

type Call struct {
	Seq           uint64
	ServiceMethod string      // 格式: "<service>.<method>"
	Args          interface{} // rpc的参数
	Reply         interface{} // rpc调用的返回值
	Error         error       // 错误出现时设置
	Done          chan *Call  // 异步调用: 调用结束时, 调用call.done()通知调用发起方
}

// 调用结束时, 调用call.done()通知调用发起方
func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	// 编解码器, 序列化要发送出去的请求, 反序列化收到的响应
	cc codec.Codec
	// 客户端发送的一系列请求, 最开始为Option结构体(魔数+序列化算法)
	opt *Option
	// 互斥锁, 保证请求的有序发送, 防止出现多个请求报文混淆
	sending sync.Mutex
	// 每个请求的消息头, 只有在请求发送时才需要
	// 因为请求发送互斥, 所以客户端只需要一个, 声明在Client结构体中可以复用
	header codec.Header
	mu     sync.Mutex // 更新Client状态的操作互斥
	// 下一个发送的请求的编号, 每个请求有唯一编号
	seq uint64
	// 存储未处理完的请求, 键是编号, 值是Call实例
	pending map[uint64]*Call
	// closing和shutdown任意一个值设置为true, 表示Client不可用
	closing  bool // 用户主动关闭
	shutdown bool // 错误导致的关闭
}

// ErrShutdown 实现error接口的实例
var ErrShutdown = errors.New("connection is shut down")

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Lock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	// 关闭编解码器内嵌的连接
	return client.cc.Close()
}

func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

// 将未收到响应的RPC调用存储到pending中
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	// 设置调用的编号
	call.Seq = client.seq
	client.seq++
	client.pending[call.Seq] = call
	return call.Seq, nil
}

// 根据调用序号, 从pending中删除Call指针
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	// 内建函数: 删除map中特定key的元素, 如果map为nil或没有key对应的元素, 则为空操作
	delete(client.pending, seq)
	return call
}

// 服务端或客户端发生错误时调用, 将shutdown设置为true, 将错误信息通知所有pending状态的Call
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err // 设置错误信息
		call.done()      // 通知异步调用发起方, 调用已完成
	}
}

// 接收响应
func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		// 根据请求头的序列号, 移除pending中的Call指针
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			// 情况1: 请求没有完整发送(未注册到pending中), 但是服务端仍旧处理了
			// 我的看法: 客户端收到不是自己发送请求的响应(seq在pending中不存在)、重复收到相同的请求(响应已经被接收)
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// 情况2: call存在, 但服务端处理出错, 在Header.Error中存在错误消息
			call.Error = fmt.Errorf(h.Error)
			// decoder#Decode方法的入参Value不是有效的(IsValid返回false), 解码后的值将被丢弃
			// IsValid返回false: reflect.ValueOf入参为nil, 或未初始化的interface
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			// 情况3: call存在, 服务端处理正常, 需要从响应body中读取Reply的值
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	// err不为nil, 退出循环, 终止客户端
	client.terminateCalls(err)
}

// NewClient 创建Client实例
// 1. 完成协议交换: 发送Option信息给服务端, 协商消息编解码方式
// 2. 初始化Client结构体
// 3. 创建子协程receive()接收响应
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	codecFunc := codec.NewCodecFuncMap[opt.CodeType]
	if codecFunc == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodeType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	// 发送Option结构体
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		conn.Close()
		return nil, err
	}
	return newClientCodec(codecFunc(conn), opt), nil
}

// 初始化Client结构体, 创建子协程接收服务端的响应,
func newClientCodec(cc codec.Codec, opt *Option) *Client {
	// 初始化Client结构体
	client := &Client{
		cc:      cc,
		opt:     opt,
		seq:     1,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

func parseOptions(opts ...*Option) (*Option, error) {
	// 如果opts为空, 或传递了nil, 使用默认Option结构体
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	// 大于1的情况下返回错误
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}

	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodeType == "" {
		opt.CodeType = codec.GobType
	}
	return opt, nil
}

// Dial 用户传入服务端地址, 创建Client实例; opts为可选参数
func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	// ...是语法糖, 可以打散切片作为入参
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	// 如果client为nil, 返回时关闭连接
	defer func() {
		if client == nil {
			conn.Close()
		}
	}()
	// 发送Option结构体, 启动子协程接收响应, 创建并返回Client结构体
	return NewClient(conn, opt)
}

// 发送rpc请求: 入参call包括本次请求的方法, 序列号, 参数
func (client *Client) send(call *Call) {
	// 确保客户端发送完整的请求(header+body)
	client.sending.Lock()
	defer client.sending.Unlock()

	// 注册到pending, Call取Client.sql作为序列号
	seq, err := client.registerCall(call)
	if err != nil {
		// 客户端已经关闭
		call.Error = err
		call.done()
		return
	}

	// 构造请求头
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	// 编码并发送请求
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		// 请求发送失败, 需要移除pending中的Call指针, 后续如果接收到服务端响应, 直接丢弃
		removedCall := client.removeCall(seq)
		// removedCall可能为nil:
		// Write函数部分失败(比如请求参数不完整), 导致服务端接返回包含非空Error的响应
		// 接收响应的协程可能先收到了该请求的响应, 并且进行了处理(从pending中移除请求, call.Error, call.done())
		if removedCall != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go Go和Call时客户端暴露给用户的两个RPC服务调用接口, Go是一个异步接口, 返回Call实例
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		// 容量为0的channel, receive协程在调用Call.done方法时会阻塞
		panic("rpc client: done channel is unbuffered")
	}
	// Seq在注册Call到pending时设置, Error在收到的响应头包含错误信息时设置
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply, // reply为指针类型, 用于接收响应
		Done:          done,  // 用于异步转同步
	}

	client.send(call)
	return call
}

// Call Call是对Go的封装, 阻塞call.Done, 等待响应返回, 是异步转同步的接口
// 仅返回error类型, 响应结果写入reply
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	// 阻塞等待Call.Done有元素可以读, 即收到服务端对rpc请求的响应后, 调用者才能返回
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}
