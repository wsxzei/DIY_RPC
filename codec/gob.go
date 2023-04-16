package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

var _Codec *GobCodec

func NewGobCodec(connection io.ReadWriteCloser) Codec {
	// 带有4kb缓冲区的io.Writer接口实现类bufio.Writer
	bufWriter := bufio.NewWriter(connection)
	return &GobCodec{
		conn: connection,
		buf:  bufWriter,
		// io.Reader作为入参, 读取字节流并进行解码
		dec: gob.NewDecoder(connection),
		// 将空接口值表示的数据编码后, 通过io.Writer对象发送出去(缓冲区的输出流)
		enc: gob.NewEncoder(bufWriter),
	}
}

/*
	GobCodec实现Codec接口中定义的方法
	gob.Decoder#Decode从输入流中解析数据, 并存储在空接口值中
	gob.Encoder#Encode: 将空接口值表示的数据实体进行编码
*/

func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// ReadBody 调用decoder.Decode方法读取字节流, 并解码为消息体; Decode方法只接受底层值为指针类型的入参;
func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

// 带有缓冲区的写操作, 需要最后执行flush操作
// 调用Encoder#Encode方法, 需要确保接口的动态值 非(指针类型且为nil)
func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		// 将缓冲区的数据发送
		c.buf.Flush()
		if err != nil {
			// 编码发送的过程中出现错误, 关闭连接
			c.Close()
		}
	}()
	if err := c.enc.Encode(h); err != nil {
		log.Panicln("rpc codec: gob error encoding header", err)
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		log.Panicln("rpc codec: gob error encoding body", err)
		return err
	}
	return nil
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
