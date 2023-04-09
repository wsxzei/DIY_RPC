package codec

import "io"

type Header struct {
	ServiceMethod string // 格式为"服务名.方法名"
	Seq uint64 // 请求序号, 用于区分不同的请求
	Error string // 错误信息, 服务端若出现错误, 错误信息置于Error
}

// Codec 消息编解码的接口
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type Type string

// NewCodecFunc Codec实例的构造函数
type NewCodecFunc func(io.ReadWriteCloser) Codec

const(
	GobType Type = "application/gob"
	JsonType Type = "application/json"
)

// NewCodecFuncMap 序列化名称到Codec函数的映射
var NewCodecFuncMap map[Type]NewCodecFunc

func init(){
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}



